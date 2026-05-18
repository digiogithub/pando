# Phase 4 — Write-Change PUB Propagation

**Date:** 2026-05-18
**Parent plan:** `pando/plans/unified_single_writer_master_plan.md`
**Status:** not started
**Risk:** low
**Effort:** medium

## 1. Goal

After each write in the primary, publish a standardised change event on the PUB socket so that secondary instances can:

- Invalidate their in-memory caches
- Refresh active session/message views
- Subscribe to real-time state changes without polling

This closes the consistency gap between "write happened on primary" and "secondary still sees stale data from its RO snapshot".

## 2. Why

### 2.1 Consistency visibility

A secondary reads from its own RO SQLite connection. After a write is proxied to the primary and committed, the secondary's RO connection won't see the change until SQLite's WAL checkpoint flushes it or the connection is reopened. With explicit PUB propagation, the secondary **knows** a write happened and can take action.

### 2.2 Real-time UI sync

If a secondary is running a TUI or Web-UI, session updates and new messages should appear immediately after another instance writes them, without requiring a page refresh or polling loop.

### 2.3 Foundation for cross-instance observation (Phase 7 of original IPC plan)

This is a prerequisite for the remote observation features in the original `inter_instance_ipc_plan.md`.

## 3. Design

### 3.1 Event Topics

Define standard PUB topics for write-change events:

| Topic | Trigger | Payload |
|---|---|---|
| `db.session.created` | CreateSession | `{session_id, title, created_at}` |
| `db.session.updated` | UpdateSession | `{session_id, title, updated_at}` |
| `db.session.deleted` | DeleteSession | `{session_id}` |
| `db.message.created` | CreateMessage | `{session_id, message_id, role}` |
| `db.message.updated` | UpdateMessage | `{session_id, message_id}` |
| `db.message.deleted` | DeleteMessage | `{session_id, message_id}` |
| `db.file.created` | CreateFile | `{session_id, file_id, path}` |
| `db.file.deleted` | DeleteFile, DeleteSessionFiles | `{session_id, file_id}` |
| `db.project.created` | CreateProject | `{project_id, name}` |
| `db.project.updated` | UpdateProjectStatus, UpdateProjectLastOpened, MarkProjectInitialized | `{project_id, field}` |
| `db.project.deleted` | DeleteProject | `{project_id}` |
| `db.skill.created` | InsertSkill | `{skill_id}` |
| `db.skill.updated` | IncrementSkillUsage, DeactivateLowestSkill | `{skill_id}` |

### 3.2 Event Publisher Interface

```go
// internal/ipc/changepub/publisher.go

package changepub

import (
    "context"
    "encoding/json"
    "time"
)

// Publisher sends write-change events on the IPC bus after every DB write.
type Publisher interface {
    Publish(ctx context.Context, topic string, payload any) error
}

// WriteChange is the standard envelope for write-change events.
type WriteChange struct {
    InstanceID string          `json:"instance_id"`
    Path       string          `json:"path"`
    Topic      string          `json:"topic"`
    Payload    json.RawMessage `json:"payload"`
    Timestamp  string          `json:"timestamp"` // RFC3339
}

// BusPublisher publishes write-change events through an ipc.Bus.
type BusPublisher struct {
    bus        PublishFunc
    instanceID string
    path       string
}

type PublishFunc func(topic string, payload any) error

func (p *BusPublisher) Publish(ctx context.Context, topic string, payload any) error {
    raw, err := json.Marshal(payload)
    if err != nil {
        return err
    }
    change := WriteChange{
        InstanceID: p.instanceID,
        Path:       p.path,
        Topic:      topic,
        Payload:    raw,
        Timestamp:  time.Now().UTC().Format(time.RFC3339),
    }
    return p.bus(topic, change)
}

// NoopPublisher discards all events — used when no bus is available.
type NoopPublisher struct{}

func (NoopPublisher) Publish(ctx context.Context, topic string, payload any) error { return nil }
```

### 3.3 Hook into Write Processing

#### Option A: Hook at the coordinator level (Phase 3)

If Phase 3 is implemented, the coordinator's `processJob` is the natural hook:

```go
func (c *Coordinator) processJob(ctx context.Context, job WriteJob) {
    result, err := dbproxy.DispatchWrite(ctx, c.q, job.Request)
    if err == nil {
        // Publish change event
        topic := dbMethodToTopic(job.Request.Method)
        if topic != "" {
            payload := buildChangePayload(job.Request)
            _ = c.publisher.Publish(ctx, topic, payload) // best-effort
        }
    }
    job.ResultC <- WriteResult{Result: result, Error: err}
}
```

#### Option B: Hook at the dispatcher level (no coordinator)

In `dispatchWrite`, after a successful write, publish:

```go
func dispatchWriteWithPublish(ctx context.Context, q db.Querier, req WriteRequest, pub Publisher) (json.RawMessage, error) {
    result, err := dispatchWrite(ctx, q, req)
    if err == nil {
        topic := dbMethodToTopic(req.Method)
        if topic != "" {
            payload := buildChangePayload(req)
            _ = pub.Publish(ctx, topic, payload)
        }
    }
    return result, err
}
```

**Recommendation:** Go with Option A if Phase 3 is done; Option B otherwise. Both are fine.

### 3.4 Method-to-Topic Mapping

```go
func dbMethodToTopic(method string) string {
    switch method {
    case "CreateSession":    return "db.session.created"
    case "UpdateSession":    return "db.session.updated"
    case "DeleteSession":    return "db.session.deleted"
    case "CreateMessage":    return "db.message.created"
    case "UpdateMessage":    return "db.message.updated"
    case "DeleteMessage":    return "db.message.deleted"
    case "CreateFile":       return "db.file.created"
    case "DeleteFile",
         "DeleteSessionFiles": return "db.file.deleted"
    case "CreateProject":         return "db.project.created"
    case "UpdateProjectStatus",
         "UpdateProjectLastOpened",
         "MarkProjectInitialized": return "db.project.updated"
    case "DeleteProject":         return "db.project.deleted"
    case "InsertSkill":           return "db.skill.created"
    case "IncrementSkillUsage",
         "DeactivateLowestSkill": return "db.skill.updated"
    default:
        return "" // unknown methods: no event
    }
}
```

### 3.5 Secondary Subscribes to Changes

Secondary instances should subscribe to `db.*` topics on the primary's PUB socket to receive write-change events:

```go
// In Bootstrap() when secondary:
ch, err := ipcClient.SubscribeTo(pubEndpoint, "db.session.", "db.message.", "db.file.", "db.project.", "db.skill.")
if err != nil {
    logging.Warn("IPC: secondary failed to subscribe to change events", "error", err)
} else {
    go handleWriteChanges(ch)
}
```

A lightweight handler that invalidates views or updates local state:

```go
func handleWriteChanges(ch <-chan ipc.Envelope) {
    for env := range ch {
        var change changepub.WriteChange
        if err := json.Unmarshal(env.Payload, &change); err != nil {
            continue
        }
        logging.Debug("IPC: write change received",
            "topic", change.Topic,
            "source", change.InstanceID)
        // TODO: invalidate session cache, refresh active view, etc.
    }
}
```

## 4. Acceptance Criteria

- [ ] `internal/ipc/changepub/` package with `Publisher` interface and `BusPublisher` implementation
- [ ] `methodToTopic` mapping for all 20 write methods
- [ ] Primary publishes `db.*` event after every successful write
- [ ] NoopPublisher available when no bus (graceful degradation)
- [ ] Secondary subscribes to `db.*` topics and logs received events
- [ ] Write-change publishing is best-effort (does not fail the write if publish fails)
- [ ] Existing tests pass

## 5. Risks

- **Low risk.** Additive feature — nothing breaks if publishing fails. Best-effort semantics by design.

## 6. Dependencies

- Phase 1 (unified bootstrap) — so secondaries know how to connect to primary PUB
- Phase 3 (serialisation loop) — recommended but not required

## 7. Estimated effort

2-3 days.
