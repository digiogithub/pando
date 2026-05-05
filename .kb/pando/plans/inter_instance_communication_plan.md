# Inter-Instance Communication Plan: ZeroMQ + Single-Writer SQLite

## Overview

This plan implements inter-instance communication for Pando using ZeroMQ (pure Go) so that multiple Pando instances can discover each other, coordinate SQLite writes (single-writer pattern), broadcast session events in real-time, and allow remote observation/control from any TUI, Web-UI, or Desktop client.

**Key Goals:**
1. Instances on the same path share a single SQLite writer (leader)
2. Any UI can list all running instances and projects across the system
3. Remote control: select an instance and observe/interact with its active session in real-time
4. Session list and live session streaming per instance

**Dependencies:**
- `github.com/go-zeromq/zmq4` — pure Go ZeroMQ 4 implementation (no CGO)
- JSON-RPC 2.0 over ZeroMQ frames

**Existing Infrastructure to Leverage:**
- `internal/pubsub/` — Generic Broker[T] (in-process pub/sub with Go channels)
- `internal/config/global_projects.go` — Global project registry (`~/.config/pando/projects.json`)
- `internal/project/manager.go` — Project Manager with child ACP process spawning
- `internal/notify/` — User-facing notification bus
- `internal/api/` — REST API with SSE streaming
- `internal/session/` — Session service with pubsub events
- `internal/mesnada/orchestrator/` — Subagent orchestrator

---

## Phase 1: Instance Identity & Discovery Service

**Goal:** Every Pando instance gets a unique identity and registers itself in a discoverable way so other instances on the same host can find it.

### 1.1 Instance Identity (`internal/instance/identity.go`)

Create a new package `internal/instance/` with:

```go
type InstanceInfo struct {
    InstanceID   string    `json:"instance_id"`    // UUID generated at startup
    PID          int       `json:"pid"`
    WorkDir      string    `json:"work_dir"`       // Canonical absolute path
    StartedAt    time.Time `json:"started_at"`
    Mode         string    `json:"mode"`           // "tui", "webui", "desktop", "acp", "noninteractive"
    ZMQPubPort   int       `json:"zmq_pub_port"`   // PUB socket port for event broadcasting
    ZMQRouterPort int      `json:"zmq_router_port"` // ROUTER socket port for RPC commands
    APIPort      int       `json:"api_port"`        // HTTP API port (if applicable)
    IsLeader     bool      `json:"is_leader"`       // True if this instance owns SQLite writes
    ProjectName  string    `json:"project_name"`
}
```

### 1.2 Instance Registry (`internal/instance/registry.go`)

File-based registry at `~/.config/pando/instances.json` (or `$XDG_RUNTIME_DIR/pando/instances.json` for ephemeral data):

- **Register**: On startup, each instance writes its `InstanceInfo` to the registry
- **Deregister**: On shutdown (or via `defer`), remove the entry
- **Heartbeat file**: Each instance touches a heartbeat file periodically (`~/.config/pando/instances/{instance_id}.heartbeat`). Stale entries (no heartbeat > 30s) are pruned by any instance that reads the registry
- **File locking**: Use `flock` on the registry file for atomic read-modify-write operations
- **ListByWorkDir(path)**: Returns all live instances sharing the same working directory
- **ListAll()**: Returns all live instances across all projects

### 1.3 Leader Election for SQLite Writes

For instances sharing the same `WorkDir`:
- The **first** instance to start acquires an exclusive `flock` on `.pando/instance.lock` in the project directory
- That instance becomes the **leader** (`IsLeader = true`) and is the only one that writes to SQLite
- Non-leader instances route write operations through the leader via ZeroMQ RPC
- If the leader dies, the lock is released and the next instance that detects the vacancy acquires leadership
- Leader changes are broadcast via PUB/SUB so all instances update their routing

### 1.4 Integration Points

- Modify `cmd/root.go` startup to call `instance.Register()` and `defer instance.Deregister()`
- Modify `internal/app/app.go` `New()` to initialize instance identity before other services
- Add `InstanceID` to the `App` struct

### Files to Create
- `internal/instance/identity.go` — InstanceInfo struct, ID generation
- `internal/instance/registry.go` — File-based registry with flock
- `internal/instance/leader.go` — Leader election via flock on project dir
- `internal/instance/cleanup.go` — Stale instance pruning

### Files to Modify
- `cmd/root.go` — Register/deregister on startup/shutdown
- `internal/app/app.go` — Initialize instance, pass InstanceInfo
- `internal/config/global_projects.go` — Extend with instance count info

---

## Phase 2: ZeroMQ Transport Layer

**Goal:** Set up the ZeroMQ sockets (PUB/SUB + ROUTER/DEALER) that form the communication backbone between instances.

### 2.1 ZMQ Bus (`internal/zmqbus/bus.go`)

Create `internal/zmqbus/` package:

```go
type Bus struct {
    instanceID  string
    pubSocket   zmq4.Socket  // PUB — broadcasts events
    routerSocket zmq4.Socket // ROUTER — receives RPC requests
    ctx         context.Context
    cancel      context.CancelFunc
}

func NewBus(ctx context.Context, instanceID string) (*Bus, error)
func (b *Bus) Start(pubPort, routerPort int) error
func (b *Bus) Shutdown() error
func (b *Bus) Publish(topic string, payload []byte) error
func (b *Bus) OnRequest(handler RequestHandler) // Register RPC handler
```

### 2.2 ZMQ Client (`internal/zmqbus/client.go`)

Client for connecting to another instance's bus:

```go
type Client struct {
    subSocket    zmq4.Socket  // SUB — subscribes to events
    dealerSocket zmq4.Socket  // DEALER — sends RPC requests
}

func NewClient(ctx context.Context) (*Client, error)
func (c *Client) SubscribeTo(pubEndpoint string, topics ...string) error
func (c *Client) SendRPC(routerEndpoint string, req *JSONRPCRequest) (*JSONRPCResponse, error)
func (c *Client) Close() error
```

### 2.3 Port Management

- Each instance dynamically selects two free TCP ports on `127.0.0.1` for PUB and ROUTER sockets
- Reuse `findFreePort()` from `internal/app/app.go` (extract to a shared utility)
- Ports are recorded in the instance registry (Phase 1)

### 2.4 Connection Pool (`internal/zmqbus/pool.go`)

- Maintains a pool of `Client` connections to known instances
- Lazy connection: only connects when needed
- Auto-reconnect on failure (ZeroMQ handles this natively)
- Prune connections to dead instances

### Files to Create
- `internal/zmqbus/bus.go` — PUB + ROUTER sockets
- `internal/zmqbus/client.go` — SUB + DEALER client
- `internal/zmqbus/pool.go` — Connection pool for multi-instance
- `internal/zmqbus/options.go` — Configuration options
- `internal/zmqbus/errors.go` — Error types

### Files to Modify
- `go.mod` — Add `github.com/go-zeromq/zmq4` dependency
- `internal/app/app.go` — Initialize ZMQ bus in `New()`

---

## Phase 3: JSON-RPC 2.0 Protocol Layer

**Goal:** Define the JSON-RPC message layer on top of ZeroMQ for structured communication between instances.

### 3.1 JSON-RPC Types (`internal/zmqbus/jsonrpc/types.go`)

```go
type Request struct {
    JSONRPC string      `json:"jsonrpc"` // always "2.0"
    ID      string      `json:"id,omitempty"` // omit for notifications
    Method  string      `json:"method"`
    Params  interface{} `json:"params,omitempty"`
}

type Response struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      string      `json:"id"`
    Result  interface{} `json:"result,omitempty"`
    Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
}
```

### 3.2 Method Registry (`internal/zmqbus/jsonrpc/registry.go`)

Define all protocol methods:

**Instance Discovery:**
- `instance.ping` → `{ status, instanceId, mode, uptime }`
- `instance.info` → `{ InstanceInfo }`
- `instance.list_sessions` → `{ sessions: [] }`

**Session Observation:**
- `session.subscribe` → Start streaming a session's events
- `session.unsubscribe` → Stop streaming
- `session.get_state` → Full current state of a session (messages, status, etc.)
- `session.list_messages` → Paginated message history

**Remote Control:**
- `session.send_message` → Send a user message to a remote session
- `session.cancel` → Cancel the current generation
- `session.create` → Create a new session on the remote instance

**Database Proxy (leader only):**
- `db.execute` → Execute a write operation on the leader's SQLite
- `db.query` → Execute a read query (any instance with local DB can serve reads)

**Orchestrator/Mesnada:**
- `agent.list_tasks` → List running mesnada tasks
- `agent.get_task_output` → Stream task output
- `agent.cancel_task` → Cancel a running task

### 3.3 Message Router (`internal/zmqbus/jsonrpc/router.go`)

Routes incoming JSON-RPC requests to registered handlers:

```go
type Router struct {
    handlers map[string]HandlerFunc
}

type HandlerFunc func(ctx context.Context, params json.RawMessage) (interface{}, error)

func (r *Router) Register(method string, handler HandlerFunc)
func (r *Router) Handle(ctx context.Context, req *Request) *Response
```

### Files to Create
- `internal/zmqbus/jsonrpc/types.go` — JSON-RPC 2.0 types
- `internal/zmqbus/jsonrpc/registry.go` — Method constants and param/result types
- `internal/zmqbus/jsonrpc/router.go` — Request routing
- `internal/zmqbus/jsonrpc/errors.go` — Standard error codes

---

## Phase 4: Single-Writer SQLite Proxy

**Goal:** Ensure only the leader instance writes to SQLite. Non-leader instances proxy writes through ZeroMQ RPC.

### 4.1 Write Proxy (`internal/instance/dbproxy.go`)

```go
type DBProxy struct {
    isLeader   bool
    localDB    *sql.DB
    localQ     db.Querier
    leaderConn *zmqbus.Client // Connection to leader instance
}

// WriteProxy implements db.Querier but routes writes to leader
func (p *DBProxy) CreateSession(ctx context.Context, params db.CreateSessionParams) (db.Session, error) {
    if p.isLeader {
        return p.localQ.CreateSession(ctx, params)
    }
    // Forward to leader via JSON-RPC
    return p.forwardWrite(ctx, "db.CreateSession", params)
}
```

### 4.2 Leader Promotion/Demotion

- When a leader dies, the next instance acquires the flock
- The new leader:
  1. Opens SQLite in read-write mode
  2. Broadcasts `instance.leader_changed` event
  3. Begins accepting `db.*` RPC requests
- Former followers update their `leaderConn` to point to the new leader

### 4.3 Read-Only Mode for Followers

- Follower instances open SQLite in `?mode=ro` (read-only)
- All reads are served locally (fast, no network hop)
- Writes are serialized through the leader, maintaining consistency
- The leader publishes `db.change` events so followers can invalidate caches

### 4.4 Graceful Handoff

When the leader is shutting down gracefully:
1. Stop accepting new write requests
2. Drain pending writes
3. Release flock
4. Broadcast `instance.leader_stepping_down`
5. A follower acquires the lock and takes over

### Files to Create
- `internal/instance/dbproxy.go` — Write proxy (leader/follower aware)
- `internal/instance/leader_watcher.go` — Monitors leader liveness, handles promotion

### Files to Modify
- `internal/db/connect.go` — Support read-only mode for followers
- `internal/app/app.go` — Use DBProxy instead of direct db.Querier
- `internal/session/session.go` — Works transparently via Querier interface

---

## Phase 5: Real-Time Event Broadcasting (PUB/SUB)

**Goal:** Broadcast session events, agent activity, and system state changes so any connected UI can observe in real-time.

### 5.1 Event Topics & Types (`internal/zmqbus/events.go`)

Define canonical PUB/SUB topics:

```
instance.joined          — New instance registered
instance.left            — Instance deregistered
instance.leader_changed  — Leader election result

session.created          — New session started
session.updated          — Session metadata changed
session.ended            — Session completed
session.message          — New message in session (user or assistant)
session.token_stream     — Token-by-token streaming (for live observation)

agent.task_started       — Mesnada task began
agent.task_progress      — Task progress update
agent.task_completed     — Task finished
agent.task_failed        — Task error

db.change                — Database write occurred (for cache invalidation)

notification.*           — User-facing notifications (forwarded from notify package)
```

### 5.2 Bridge: Internal PubSub → ZMQ PUB (`internal/zmqbus/bridge.go`)

Bridge the existing in-process `pubsub.Broker[T]` to ZeroMQ PUB:

```go
type PubSubBridge struct {
    bus      *Bus
    sessions session.Service  // subscribes to session events
    notify   // subscribes to notifications
}

func (b *PubSubBridge) Start(ctx context.Context) {
    // Subscribe to internal session events and forward to ZMQ PUB
    sessionCh := b.sessions.Subscribe(ctx)
    go func() {
        for event := range sessionCh {
            payload, _ := json.Marshal(event)
            b.bus.Publish("session."+string(event.Type), payload)
        }
    }()
    // ... similar for notifications, agent events, etc.
}
```

### 5.3 Token Streaming Bridge

For live session observation, bridge the LLM token stream:
- The agent's `StreamResponse` currently writes tokens to the TUI via channels
- Add a parallel writer that publishes each token chunk to `session.token_stream` topic
- Include `sessionId` and `messageId` in the payload so observers can render correctly

### 5.4 Event Envelope

Every PUB event uses a standard envelope:

```json
{
    "instance_id": "uuid",
    "project_path": "/path/to/project",
    "session_id": "uuid",
    "event_type": "session.message",
    "timestamp": "2026-05-05T22:00:00Z",
    "data": { /* event-specific payload */ }
}
```

### Files to Create
- `internal/zmqbus/events.go` — Event types and envelope
- `internal/zmqbus/bridge.go` — Internal pubsub → ZMQ PUB bridge
- `internal/zmqbus/token_bridge.go` — Token stream forwarding

### Files to Modify
- `internal/llm/agent/agent.go` — Add hook for token stream broadcasting
- `internal/session/session.go` — Ensure all lifecycle events are published
- `internal/mesnada/orchestrator/orchestrator.go` — Forward task events to ZMQ

---

## Phase 6: Remote Control Protocol

**Goal:** Enable any connected UI to send commands to any instance — observe sessions, send messages, cancel operations.

### 6.1 RPC Handlers (`internal/zmqbus/handlers/`)

Implement JSON-RPC handlers for each method defined in Phase 3:

**Instance handlers:**
```go
func HandleInstancePing(ctx context.Context, params json.RawMessage) (interface{}, error)
func HandleInstanceInfo(ctx context.Context, params json.RawMessage) (interface{}, error)
func HandleInstanceListSessions(ctx context.Context, params json.RawMessage) (interface{}, error)
```

**Session handlers:**
```go
func HandleSessionGetState(ctx context.Context, params json.RawMessage) (interface{}, error)
func HandleSessionListMessages(ctx context.Context, params json.RawMessage) (interface{}, error)
func HandleSessionSendMessage(ctx context.Context, params json.RawMessage) (interface{}, error)
func HandleSessionCancel(ctx context.Context, params json.RawMessage) (interface{}, error)
```

**Agent handlers:**
```go
func HandleAgentListTasks(ctx context.Context, params json.RawMessage) (interface{}, error)
func HandleAgentGetTaskOutput(ctx context.Context, params json.RawMessage) (interface{}, error)
func HandleAgentCancelTask(ctx context.Context, params json.RawMessage) (interface{}, error)
```

### 6.2 Remote Session Proxy (`internal/instance/remote_session.go`)

A `RemoteSession` wrapper that implements the same interface as a local session but routes everything through ZeroMQ:

```go
type RemoteSession struct {
    client     *zmqbus.Client
    instanceID string
    sessionID  string
}

func (rs *RemoteSession) SendMessage(ctx context.Context, content string) error
func (rs *RemoteSession) GetMessages(ctx context.Context) ([]message.Message, error)
func (rs *RemoteSession) StreamTokens(ctx context.Context) (<-chan string, error) // via SUB
func (rs *RemoteSession) Cancel(ctx context.Context) error
```

### 6.3 Permission Model

- Remote control requires explicit opt-in per instance (config flag `allow_remote_control: true`)
- Read-only observation is allowed by default for instances on the same path
- Shared secret token (auto-generated, stored in `~/.config/pando/zmq_token`) for authentication
- All communication restricted to `tcp://127.0.0.1` (localhost only)

### Files to Create
- `internal/zmqbus/handlers/instance.go` — Instance info/ping handlers
- `internal/zmqbus/handlers/session.go` — Session CRUD and streaming handlers
- `internal/zmqbus/handlers/agent.go` — Mesnada task handlers
- `internal/instance/remote_session.go` — Remote session proxy

### Files to Modify
- `internal/config/config.go` — Add `RemoteControl` config section

---

## Phase 7: UI Integration (TUI + Web-UI + Desktop)

**Goal:** All frontends can list instances/projects, select one, and observe or control it in real-time.

### 7.1 TUI: Instances Page (`internal/tui/components/instances/`)

New TUI page accessible via `Ctrl+I` (or from the sidebar):

```
┌─ Pando Instances ──────────────────────────────────┐
│                                                      │
│  Project: /www/MCP/Pando/pando                       │
│  ┌──────────┬──────────┬────────┬───────┬──────────┐ │
│  │ Instance │ Mode     │ Leader │ PID   │ Sessions │ │
│  ├──────────┼──────────┼────────┼───────┼──────────┤ │
│  │ ★ abc123 │ tui      │ ✓      │ 12345 │ 3        │ │
│  │   def456 │ webui    │        │ 12346 │ 1        │ │
│  │   ghi789 │ desktop  │        │ 12347 │ 2        │ │
│  └──────────┴──────────┴────────┴───────┴──────────┘ │
│                                                      │
│  Project: /www/other-project                         │
│  ┌──────────┬──────────┬────────┬───────┬──────────┐ │
│  │   jkl012 │ tui      │ ✓      │ 12400 │ 1        │ │
│  └──────────┴──────────┴────────┴───────┴──────────┘ │
│                                                      │
│  [Enter] View Sessions  [o] Observe  [r] Remote Ctrl │
│  [Refresh] F5           [q] Back                     │
└──────────────────────────────────────────────────────┘
```

Features:
- Grouped by project path
- Shows leader status, mode, session count
- Enter → shows session list for selected instance
- `o` → observe mode (read-only live view of the session)
- `r` → remote control (full interaction capability)

### 7.2 TUI: Remote Session View

When observing or controlling a remote session:
- Token-by-token streaming via ZMQ SUB (same rendering as local sessions)
- Message history loaded via RPC
- In observe mode: read-only, shows "OBSERVING instance abc123" in status bar
- In remote control mode: can type messages, shows "REMOTE CONTROL → abc123" in status bar
- `Esc` returns to local instance

### 7.3 Web-UI: Instances Panel (`web-ui/src/components/instances/`)

React components:
- `InstancesPanel.tsx` — Lists all instances grouped by project
- `InstanceCard.tsx` — Card showing instance info with actions
- `RemoteSessionView.tsx` — Embeds remote session observation/control
- API endpoints proxied through the local Pando HTTP API

### 7.4 Web-UI API Endpoints

New REST endpoints in `internal/api/`:

```
GET  /api/v1/instances                    — List all running instances
GET  /api/v1/instances/:id                — Get instance info
GET  /api/v1/instances/:id/sessions       — List sessions on remote instance
GET  /api/v1/instances/:id/sessions/:sid  — Get remote session state
POST /api/v1/instances/:id/sessions/:sid/messages — Send message to remote session
GET  /api/v1/instances/:id/sessions/:sid/stream   — SSE stream of remote session
DELETE /api/v1/instances/:id/sessions/:sid/cancel  — Cancel remote generation
```

These endpoints internally use the ZMQ client pool to communicate with the target instance.

### 7.5 Desktop (Wails)

The desktop app uses the same Web-UI components, so it gets instance management for free through the API layer.

### 7.6 Zustand/Pinia Store

```typescript
interface InstancesStore {
    instances: InstanceInfo[]
    selectedInstance: string | null
    remoteSession: RemoteSessionState | null
    fetchInstances: () => Promise<void>
    selectInstance: (id: string) => void
    observeSession: (instanceId: string, sessionId: string) => void
    sendRemoteMessage: (content: string) => Promise<void>
}
```

### Files to Create
- `internal/tui/components/instances/instances.go` — TUI instances page
- `internal/tui/components/instances/model.go` — Bubble Tea model
- `internal/tui/components/instances/view.go` — Render logic
- `internal/tui/components/instances/remote_view.go` — Remote session viewer
- `internal/api/handlers_instances.go` — REST API for instances
- `web-ui/src/components/instances/InstancesPanel.tsx`
- `web-ui/src/components/instances/InstanceCard.tsx`
- `web-ui/src/components/instances/RemoteSessionView.tsx`
- `web-ui/src/stores/instances.ts` — Zustand store

### Files to Modify
- `internal/tui/tui.go` — Add instances page, keybinding
- `internal/api/routes.go` — Register instance endpoints
- `web-ui/src/App.tsx` — Add instances route
- `web-ui/src/components/Sidebar.tsx` — Add instances navigation

---

## Implementation Order & Dependencies

```
Phase 1 ──→ Phase 2 ──→ Phase 3 ──→ Phase 4
  │                         │           │
  │                         ▼           │
  │                      Phase 5 ◄─────┘
  │                         │
  │                         ▼
  │                      Phase 6
  │                         │
  └─────────────────────→ Phase 7
```

- Phases 1-3 are sequential (each builds on the previous)
- Phase 4 (single-writer) depends on Phases 1-3
- Phase 5 (PUB/SUB events) depends on Phase 3 and uses Phase 4 for db.change events
- Phase 6 (remote control) depends on Phase 5
- Phase 7 (UI) can start partially in parallel with Phase 6 (instances list from Phase 1)

## Testing Strategy

- **Unit tests**: Each phase has focused tests in `tests/` (Python) and `*_test.go`
- **Integration tests**: Spawn 2-3 instances in the same directory, verify leader election, event forwarding, and remote control
- **Benchmark**: Measure latency of ZMQ PUB/SUB event propagation and RPC round-trip time
- **Commands**: `go test ./internal/instance ./internal/zmqbus ./internal/zmqbus/jsonrpc`

## Configuration

New config section in `.pando.toml`:

```toml
[instances]
enabled = true                    # Enable inter-instance communication
allow_remote_control = false      # Allow other instances to send messages
zmq_pub_port = 0                  # 0 = auto-select
zmq_router_port = 0              # 0 = auto-select
heartbeat_interval = "10s"
leader_timeout = "30s"
max_token_stream_buffer = 1000   # Max buffered tokens for observers
```

## Risk Mitigation

| Risk | Mitigation |
|------|-----------|
| ZMQ port conflicts | Dynamic port selection + registry |
| Stale instance entries | Heartbeat + PID liveness check |
| Leader crash during write | WAL mode + write-ahead journaling |
| Token stream backpressure | Bounded buffers, drop oldest on overflow |
| Security (local only) | Restrict to 127.0.0.1, shared token auth |
| go-zeromq/zmq4 maturity | Fallback plan: Unix domain sockets + custom protocol |
