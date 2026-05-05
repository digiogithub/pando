# Inter-Instance Communication Plan (ZeroMQ IPC)

## Overview

Enable Pando instances to communicate with each other using ZeroMQ (`github.com/go-zeromq/zmq4`). Instances sharing the same working directory follow a **primary/secondary** model: the first instance to start in a path becomes the **primary** (sole SQLite writer), while subsequent instances connect to it as **secondaries** (read-only, relay DB writes via ZMQ). Any Pando instance in desktop, web-ui, or TUI mode can browse all running instances and projects, select one, and observe or control it in real time.

---

## Architecture Summary

```
┌─────────────────────────────────────────────────────────┐
│  Host machine                                           │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │  Instance A  (path /proj/foo  –  PRIMARY)        │   │
│  │  ┌──────────────┐  ┌─────────────────────────┐  │   │
│  │  │  SQLite (RW) │  │  ZMQ Bus                │  │   │
│  │  │              │  │  PUB  tcp://127.0.0.1:N │  │   │
│  │  │              │  │  ROUTER tcp://…:N+1     │  │   │
│  │  └──────────────┘  └──────────────┬──────────┘  │   │
│  └───────────────────────────────────│─────────────┘   │
│                                      │ ZMQ              │
│  ┌───────────────────────────────────│─────────────┐   │
│  │  Instance B  (path /proj/foo  –  SECONDARY)     │   │
│  │  (no SQLite writes; subscribes to A's PUB;     │   │
│  │   routes state reads through ZMQ RPC to A)     │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │  Instance C  (path /proj/bar  –  PRIMARY)       │   │
│  │  Own SQLite + Own ZMQ Bus                       │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │  Observer (TUI / Web-UI / Desktop)              │   │
│  │  Subscribes to Instance A PUB + Instance C PUB │   │
│  │  Sends commands via DEALER → ROUTER             │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### ZMQ Socket Patterns

| Direction                        | Pattern         | Notes                            |
|----------------------------------|-----------------|----------------------------------|
| Instance → Observers (events)    | PUB / SUB       | broadcast session/tool/LLM events |
| Observer → Instance (commands)   | DEALER / ROUTER | JSON-RPC 2.0 RPCs                 |
| Secondary → Primary (relay)      | DEALER / ROUTER | DB write relay, sync requests     |

### Port Allocation

Ports are derived deterministically from the working-directory path to avoid config files:

```
base_port = 40000 + (fnv32a(abs_path) % 20000)
PUB port  = base_port
RPC port  = base_port + 1
```

This gives each path a fixed 2-port slot in `[40000, 60000)`. The range is large enough to avoid collisions in typical development use.

### Primary Election

A file lock (`<workdir>/.pando/ipc.lock`) is used:
- **Primary**: acquires exclusive flock, binds ZMQ sockets, writes own PID + ports.
- **Secondary**: cannot acquire the lock, reads `ipc.lock` to discover primary PUB/RPC ports, connects as a client.
- On clean shutdown, the primary releases the lock; on crash, the lock is automatically released by the OS.

---

## Phase 1 — ZMQ Bus Infrastructure

**Goal**: Add `go-zeromq/zmq4` dependency and create the `internal/ipc` package.

### Tasks

1. `go get github.com/go-zeromq/zmq4@latest` – add dependency.
2. Create `internal/ipc/` package:
   - `bus.go` — `Bus` struct with PUB+ROUTER sockets, `Start(ctx)`, `Stop()`, `Publish(topic, payload)`.
   - `client.go` — `Client` struct with SUB+DEALER sockets, `Subscribe(topics...)`, `Call(method, params)`.
   - `ports.go` — `PortsForPath(absPath string) (pub, rpc int)` using FNV-32a hash.
   - `lock.go` — `AcquireLock(workdir string) (isPrimary bool, err error)` using `syscall.Flock`.
3. Unit tests for port determinism and lock acquisition.

### Key Types

```go
// internal/ipc/bus.go
type Bus struct {
    PubAddr string  // "tcp://127.0.0.1:<pub_port>"
    RPCAddr string  // "tcp://127.0.0.1:<rpc_port>"
    // ...
}

func (b *Bus) Publish(topic string, payload any) error
func (b *Bus) RegisterMethod(method string, handler HandlerFunc)

// internal/ipc/client.go
type Client struct{}
func (c *Client) Subscribe(ctx context.Context, topics ...string) (<-chan Envelope, error)
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error)
```

### Envelope Format (JSON-RPC 2.0 compatible)

```json
{
  "instanceId": "uuid",
  "projectId":  "uuid",
  "sessionId":  "uuid or empty",
  "topic":      "session.update",
  "timestamp":  "2026-05-05T22:00:00Z",
  "payload":    { ... }
}
```

---

## Phase 2 — Instance Registry & Leader Election

**Goal**: Determine primary/secondary role at startup and maintain a registry of running instances.

### Tasks

1. Create `internal/instanceregistry/` package:
   - `registry.go` — `Registry` struct: discovers all running pando instances by scanning `/tmp/pando-instances/` (one JSON file per instance, keyed by PID).
   - `entry.go` — `Entry { InstanceID, Path, PID, PubPort, RPCPort, StartedAt, Mode }`.
   - `announce.go` — `Announce(entry)` writes the instance's JSON file; `Revoke(instanceID)` removes it on shutdown.
2. Extend `internal/ipc/lock.go` with `LockInfo` struct (stores instanceID, ports in the lock file).
3. Wire into `internal/app/app.go`:
   - On startup: call `ipc.AcquireLock(workdir)`.
   - If primary: start `ipc.Bus`, call `instanceregistry.Announce`.
   - If secondary: create `ipc.Client` connecting to primary.
4. Add `InstanceRole` field to the app struct (`Primary` / `Secondary`).
5. New DB migration: no schema change needed (instance state is ephemeral / file-based).

### Instance File Location

```
/tmp/pando-instances/<instanceID>.json
```

Each file is cleaned up on exit or after a stale timeout (PID check).

---

## Phase 3 — Session State Protocol & Broadcasting

**Goal**: Define all ZMQ message types and hook them into the session lifecycle.

### Message Topics

| Topic                 | Direction        | Description                                      |
|-----------------------|------------------|--------------------------------------------------|
| `session.list`        | PUB on change    | Full list of sessions (id, title, updated_at)    |
| `session.update`      | PUB              | Single session metadata update                   |
| `session.activated`   | PUB              | Active session changed                           |
| `message.append`      | PUB              | New message added to session (role, content)     |
| `llm.token`           | PUB              | Streaming LLM token (sessionId, token)           |
| `llm.start`           | PUB              | LLM call started                                 |
| `llm.end`             | PUB              | LLM call ended (tokens_in, tokens_out)           |
| `tool.start`          | PUB              | Tool execution started (name, params)            |
| `tool.end`            | PUB              | Tool execution ended (name, result, duration)    |
| `instance.heartbeat`  | PUB every 5s     | Instance alive signal + active session info      |
| `instance.shutdown`   | PUB              | Instance is shutting down                        |

### RPC Methods (DEALER → ROUTER)

| Method                | Params                          | Result                                  |
|-----------------------|---------------------------------|-----------------------------------------|
| `state.sync`          | `{ projectId? }`                | `{ sessions, activeSessionId, instance }` |
| `session.list`        | `{}`                            | `[ { id, title, updatedAt } ]`          |
| `session.get`         | `{ sessionId }`                 | Full session with messages              |
| `session.activate`    | `{ sessionId }`                 | `{ ok }`                                |
| `message.send`        | `{ sessionId, content }`        | `{ messageId }`                         |
| `session.interrupt`   | `{ sessionId }`                 | `{ ok }`                                |

### Tasks

1. Create `internal/ipc/protocol/` sub-package defining all payload structs as Go types.
2. Hook `internal/session/session.go`:
   - After message append → `bus.Publish("message.append", ...)`
   - On session create/update → `bus.Publish("session.update", ...)`
   - On active session switch → `bus.Publish("session.activated", ...)`
3. Hook LLM streaming in `internal/llm/` → publish `llm.token`, `llm.start`, `llm.end`.
4. Hook tool execution → publish `tool.start`, `tool.end`.
5. Register RPC method handlers in `internal/ipc/bus.go` calling back into `session.Service`.
6. Start heartbeat goroutine in bus (5s interval).

---

## Phase 4 — Secondary Instance Mode

**Goal**: Secondary instances (same path as a running primary) operate without SQLite writes; all state reads/writes route through the primary via ZMQ RPC.

### Tasks

1. Wrap `internal/db/` writes behind a `StateStore` interface:
   ```go
   type StateStore interface {
       AppendMessage(ctx, msg) error
       UpdateSession(ctx, session) error
       // ...
   }
   ```
2. `PrimaryStateStore` — direct SQLite writes (current behavior).
3. `SecondaryStateStore` — delegates all writes to `ipc.Client.Call("db.write", ...)`.
4. Register `db.write` RPC handler in the primary bus that routes to `PrimaryStateStore`.
5. In `app.go`, wire `StateStore` based on role (Primary → direct, Secondary → relay).
6. Secondary instances still run their own TUI/Web-UI; they pull data via ZMQ RPCs.

---

## Phase 5 — Remote Observation & Control

**Goal**: Any Pando instance can observe and control any other instance in real time.

### Tasks

1. Create `internal/remoteview/` package:
   - `RemoteSession` — subscribes to a remote instance's PUB, maintains a local mirror of its session state.
   - `RemoteControl` — wraps `ipc.Client` with typed methods: `SendMessage`, `SwitchSession`, `Interrupt`.
2. `RemoteSession.Messages()` returns a channel of live `message.append` / `llm.token` events.
3. `RemoteSession.Sync()` performs `state.sync` RPC to bootstrap initial state.
4. Authentication: shared token derived from the instance lock file (only processes on the same host can read it).
5. Add `GET /api/instances` REST endpoint listing all running instances (from `instanceregistry`).
6. Add `GET /api/instances/:id/stream` SSE endpoint that proxies the remote instance's PUB stream.
7. Add `POST /api/instances/:id/sessions/:sid/message` REST endpoint for remote `message.send`.

---

## Phase 6 — Instance Discovery UI (API layer)

**Goal**: Expose a clean REST+SSE API for the TUI and Web-UI to consume.

### Tasks

1. New file `internal/api/handlers_instances.go`:
   - `GET  /api/instances`                             — list all running instances
   - `GET  /api/instances/:id`                         — get instance details + session list
   - `GET  /api/instances/:id/stream`                  — SSE proxy of remote PUB stream
   - `POST /api/instances/:id/sessions/:sid/activate`  — remote session switch
   - `POST /api/instances/:id/sessions/:sid/message`   — send message to remote session
   - `POST /api/instances/:id/sessions/:sid/interrupt` — interrupt current operation
2. Wire `RemoteView` into the API handler (use `remoteview.RemoteSession` per connection).
3. SSE format identical to existing chat SSE: `data: { type, payload }\n\n`.

---

## Phase 7 — TUI Instance Browser

**Goal**: New TUI page to browse and control remote instances.

### Keybinding

`Ctrl+Alt+I` → open Instances Browser page.

### Layout

```
┌──────────────────────────────────────────────────────────────────┐
│  Instances  [2 running]                                          │
├───────────────────────┬──────────────────────────────────────────┤
│ Instances             │  Sessions — /proj/foo (PRIMARY)          │
│ ▸ /proj/foo  PRIMARY  │  ▸ session-abc  "Fix login bug"  2m ago  │
│   /proj/bar  PRIMARY  │    session-def  "Refactor auth"  1h ago  │
│   /proj/foo  2nd       │                                         │
├───────────────────────┼──────────────────────────────────────────┤
│                       │  Live View — session-abc                 │
│                       │  [Tool] read_file internal/auth/auth.go  │
│                       │  [LLM]  Analyzing the auth middleware...  │
│                       │  ▌ (streaming cursor)                    │
└───────────────────────┴──────────────────────────────────────────┘
```

### Tasks

1. Create `internal/tui/instances/` page:
   - `model.go` — Bubble Tea model with three panes: instance list, session list, live view.
   - `instances_list.go` — polls `instanceregistry` every 2s.
   - `sessions_list.go` — subscribes to selected instance's `session.list` events.
   - `live_view.go` — subscribes to `message.append`, `llm.token`, `tool.start/end`; renders in a scrollable viewport.
2. Register page in `internal/tui/app.go` with `Ctrl+Alt+I` binding.
3. Add keyboard shortcuts within the page:
   - `Enter` on session → watch live view
   - `m` → open message send dialog (remote control)
   - `i` → interrupt current operation
   - `s` → switch active session (remote)

---

## Phase 8 — Web-UI Instance Browser

**Goal**: New Web-UI panel for remote instance observation and control.

### Tasks

1. New React component `web-ui/src/components/InstancesBrowser/`:
   - `InstanceList.tsx` — fetches `GET /api/instances`, auto-refreshes every 5s.
   - `SessionList.tsx` — fetches sessions from selected instance.
   - `LiveView.tsx` — SSE subscription to `/api/instances/:id/stream`, renders messages in real time like the main chat view.
   - `RemoteControls.tsx` — buttons: Send Message, Switch Session, Interrupt.
2. New route `/instances` in the React router.
3. Add "Instances" entry to the sidebar navigation.
4. Reuse existing chat message renderer components for `LiveView`.

---

## Implementation Order & Dependencies

```
Phase 1 (ZMQ Bus)
    └── Phase 2 (Instance Registry + Lock)
            ├── Phase 3 (Protocol + Session Hooks)
            │       └── Phase 4 (Secondary Mode)
            │               └── Phase 5 (Remote Observation)
            │                       └── Phase 6 (API Layer)
            │                               ├── Phase 7 (TUI)
            │                               └── Phase 8 (Web-UI)
            └── (Phase 4 depends on 3 too)
```

---

## Key Design Decisions

1. **Pure-Go ZeroMQ** (`go-zeromq/zmq4`) — no CGO, works on all platforms in the build.
2. **File-lock primary election** (`flock`) — OS-level, crash-safe, no daemon process needed.
3. **FNV-32a port derivation** — deterministic, no config file for ZMQ ports.
4. **JSON-RPC 2.0 over ZMQ** — standard protocol, easy to extend, transport-agnostic.
5. **`/tmp/pando-instances/` registry** — ephemeral, cleaned on exit, stale entries pruned by PID check.
6. **Secondary instances** — do not block operation; they connect opportunistically and degrade gracefully if the primary is unreachable (fall back to read-only mode).
7. **SSE proxy** in the REST API — Web-UI doesn't need to speak ZMQ directly; the Go backend proxies the stream.
8. **No Temporal** — the optional Temporal layer from the design docs is deferred; it can be added later on top of Phase 1-3 primitives.

---

## Files to Create / Modify

### New packages
- `internal/ipc/` — bus, client, ports, lock, protocol/
- `internal/instanceregistry/` — registry, entry, announce
- `internal/remoteview/` — remote session mirror + control
- `internal/tui/instances/` — TUI browser page
- `web-ui/src/components/InstancesBrowser/` — Web-UI browser

### Modified files
- `internal/app/app.go` — wire IPC startup, role detection
- `internal/session/session.go` — publish ZMQ events
- `internal/llm/` — publish LLM stream events
- `internal/api/handlers_instances.go` — new REST+SSE handlers
- `internal/api/router.go` — register new routes
- `internal/tui/app.go` — register instances page + keybinding
- `web-ui/src/App.tsx` — add /instances route + sidebar link
- `go.mod` / `go.sum` — add go-zeromq/zmq4

---

## Notes on Single-Writer SQLite

The single-writer constraint (per the design docs) is enforced as follows:

- The **primary** instance holds the `flock` on `<workdir>/.pando/ipc.lock` for its entire lifetime.
- Any **secondary** that tries to open the same SQLite file for writes will fail or be redirected.
- In Phase 4, writes from secondaries are relayed over ZMQ to the primary, which executes them in its single write path.
- This preserves SQLite's performance characteristics and avoids WAL contention.

---

*Created: 2026-05-05. Author: José F. Rives / Claude Sonnet 4.6.*
