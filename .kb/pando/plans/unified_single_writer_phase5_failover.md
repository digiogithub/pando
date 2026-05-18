# Phase 5 — Primary Failover & Secondary Promotion

**Date:** 2026-05-18
**Parent plan:** `pando/plans/unified_single_writer_master_plan.md`
**Status:** not started
**Risk:** high
**Effort:** large

## 1. Goal

When the primary instance dies or shuts down gracefully, the remaining secondary instances should detect this and, where possible, one of them should promote itself to primary, re-acquire the write lock, and continue serving writes.

## 2. Why

### 2.1 Availability

In a multi-instance scenario, if the primary dies, all secondaries lose their write path. They can still read locally, but no session creation, message saving, or project updates can happen until the primary is restarted.

### 2.2 Developer experience

A typical workflow: TUI session crashes → user opens new TUI → old TUI was primary, new instance becomes primary automatically.

### 2.3 Long-running sessions

If serve/app/desktop are long-lived and the TUI primary dies, the server should not become permanently read-only.

## 3. Design

### 3.1 Detection: Heartbeat via PUB

The primary already publishes `instance.heartbeat` every 5 seconds (from `bridge.Bridge`). Secondaries subscribe to these heartbeats.

If no heartbeat is received for `3 × heartbeat_interval` (15s default), the secondary considers the primary dead.

### 3.2 Failover Protocol

```
SECONDARY                            PRIMARY (dead)
    |
    | -- heartbeat expected every 5s -->
    | <-- heartbeat                   |
    |                                  |
    | [heartbeat missed: 15s]          |
    |                                  |
    | -- start failover sequence:
    |
    | 1. Mark self as "promoting"
    | 2. Try to acquire lock (flock)
    |    a. Success → promote
    |    b. Failure → another instance promoted first → stay secondary
    |
    | 3. If promoted:
    |    a. Close RO DB connection
    |    b. Open RW DB (Connect)
    |    c. Create new Bus (bind to path-derived ports)
    |    d. Register dbproxy + bridge handlers
    |    e. Announce as PRIMARY in instanceregistry
    |    f. Notify local services of role change
    |
    | 4. If not promoted (another secondary won):
    |    a. Connect to new primary's RPC endpoint (same ports, different instanceID)
    |    b. Re-create IPC Client
    |    c. Update local WriteProxy
```

### 3.3 `internal/ipc/failover/` package

```go
package failover

import (
    "context"
    "encoding/json"
    "sync"
    "time"

    "github.com/digiogithub/pando/internal/ipc"
)

// RoleChanged is emitted when the instance's role changes.
type RoleChanged struct {
    OldRole string // "primary" | "secondary"
    NewRole string
}

// Watcher monitors the primary's liveness and triggers failover.
type Watcher struct {
    mu       sync.RWMutex
    role     string
    instanceID string
    workdir    string

    // Connection management
    bus          *ipc.Bus        // non-nil if primary
    client       *ipc.Client     // non-nil if secondary
    pubEndpoint  string
    rpcEndpoint  string

    // Heartbeat tracking
    lastHeartbeat time.Time
    heartbeatCh   chan struct{}

    // Failover callbacks
    onPromote   func() error  // called when this instance should become primary
    onDemote    func() error  // called when this instance should become secondary
    onRoleChange chan<- RoleChanged

    cancel context.CancelFunc
}

// NewWatcher creates a failover watcher for the given role.
func NewWatcher(
    ctx context.Context,
    instanceID, workdir string,
    bus *ipc.Bus,    // nil if secondary
    client *ipc.Client, // nil if primary
    pubEndpoint string,
    onPromote, onDemote func() error,
) *Watcher

// Start begins heartbeat monitoring (secondary) or heartbeat publishing (primary).
func (w *Watcher) Start(ctx context.Context)

// Promote attempts to acquire the lock and become primary.
// Returns true if successful, false if another instance won.
func (w *Watcher) promote(ctx context.Context) (bool, error)
```

### 3.4 Lock Re-Acquisition Semantics

On promotion attempt:

1. Try `AcquireLock(workdir, instanceID, pubPort, rpcPort)`
2. If lock is available (primary is truly dead), acquire it
3. Write new LockInfo with our instanceID, PID, ports
4. If lock is held by another process that's still alive (PID check), abort promotion
5. If lock is held by a dead PID, force-acquire

### 3.5 Graceful Primary Shutdown

When a primary shuts down:

1. Stop accepting new RPC requests (close ROUTER)
2. Publish `instance.shutdown` event
3. Drain pending write jobs (if coordinator)
4. Release `flock`
5. Close sockets

Secondaries that see `instance.shutdown` can start failover immediately (no need to wait for heartbeat timeout).

### 3.6 Edge Cases

#### Multiple secondaries try to promote simultaneously

The `flock` is the arbiter — only one process can acquire the exclusive lock. Others will get `EAGAIN`/`EWOULDBLOCK` and stay secondary, connecting to the new primary.

#### Promotion fails (lock still held by live process)

Log warning, reset heartbeat timer, stay secondary.

#### Write in-flight during promotion

- If a write RPC call is in-flight and the primary dies, the call will timeout
- The secondary's `writeWithRetry` mechanism (Phase 2) will retry
- If retries exhaust, return an explicit error to the caller
- The caller (app layer) can decide how to handle: show error to user, retry later, etc.

## 4. Implementation Steps

1. Implement `internal/ipc/failover/watcher.go` with heartbeat tracking and promotion logic
2. Add `instance.shutdown` publishing to `ipc.Bus.Shutdown()` and `bridge.Bridge`
3. Wire failover watcher into `ipcruntime.Bootstrap()` result
4. `BootstrapResult.Cleanup` includes watcher shutdown
5. Add `OnRoleChanged` callback to `App` so session/message services can switch DB connections
6. Test: start primary, kill it, verify secondary promotes

## 5. Acceptance Criteria

- [ ] Primary publishes periodic heartbeats (5s)
- [ ] Primary publishes `instance.shutdown` on graceful exit
- [ ] Secondary detects primary death after 15s heartbeat absence
- [ ] Secondary attempts promotion on primary death
- [ ] `flock` ensures only one secondary promotes
- [ ] On promotion: RO → RW DB, Bus starts, handlers registered
- [ ] On failed promotion: secondary reconnects to new primary
- [ ] `OnRoleChanged` callback notifies app layer
- [ ] Integration test: kill primary, observe secondary promoting
- [ ] No duplicate writes or lost data during failover (eventual consistency is acceptable)
- [ ] Existing tests pass

## 6. Risks

- **High risk.** Failover is inherently complex. Undetected edge cases can lead to split-brain, lost writes, or stuck state.
- **Mitigation:** extensive testing, conservative defaults (start with disabled failover, enable via config flag), phased rollout.

### Risk mitigation strategy

- Phase 5a: Manual failover (operator runs command to promote secondary) — lower risk
- Phase 5b: Automatic failover behind feature flag (`--auto-failover`)
- Phase 5c: Automatic failover enabled by default once battle-tested

## 7. Dependencies

- Phase 1 (unified bootstrap)
- Phase 3 (serialisation loop) — recommended
- Phase 4 (PUB propagation) — for heartbeat detection

## 8. Estimated effort

7-10 days (including testing and edge case hardening).
