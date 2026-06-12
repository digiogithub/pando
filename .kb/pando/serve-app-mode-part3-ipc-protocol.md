# Pando Server Implementation Analysis — Part 3: IPC Protocol and Inter-Instance Communication

## 5. IPC Protocol (Inter-Process Communication)

### Overview

Pando's IPC protocol allows multiple instances in the same working directory to communicate with each other. It is implemented over **ZMQ** (library `go-zeromq/zmq4`) using a **PUB/SUB + ROUTER/DEALER** pattern.

**Main protocol files:**
- `/www/MCP/Pando/pando/internal/ipc/bus.go` — Bus (server) PUB + ROUTER
- `/www/MCP/Pando/pando/internal/ipc/client.go` — Client (client) SUB + DEALER
- `/www/MCP/Pando/pando/internal/ipc/envelope.go` — Envelope (standard message)
- `/www/MCP/Pando/pando/internal/ipc/ports.go` — Port assignment
- `/www/MCP/Pando/pando/internal/ipc/lock_common.go` — LockInfo and lock file
- `/www/MCP/Pando/pando/internal/ipc/lock_unix.go` — Lock acquisition (flock)
- `/www/MCP/Pando/pando/internal/ipc/options.go` — Configuration (timeouts)
- `/www/MCP/Pando/pando/internal/ipc/errors.go` — Error definitions
- `/www/MCP/Pando/pando/internal/ipc/protocol/rpc.go` — RPC constants and types
- `/www/MCP/Pando/pando/internal/ipc/protocol/topics.go` — PUB topic constants
- `/www/MCP/Pando/pando/internal/ipc/protocol/payloads.go` — Payload types
- `/www/MCP/Pando/pando/internal/ipc/bridge/bridge.go` — Bridge: connects in-process events to the bus
- `/www/MCP/Pando/pando/internal/ipc/bridge/handlers.go` — JSON-RPC handlers

### Socket Architecture

**Bus (server — primary instance):**
- **PUB socket** (`tcp://127.0.0.1:{pubPort}`): Broadcasts events to all subscribed instances
- **ROUTER socket** (`tcp://127.0.0.1:{rpcPort}`): Handles JSON-RPC request/response calls

**Client (client — secondary/observer instances):**
- **SUB socket**: Connects to the Bus's PUB to receive events filtered by topic
- **DEALER socket** (cached): Connects to the Bus's ROUTER to make RPC calls

### Message Format

**Envelope (PUB):** (`envelope.go`)
```go
type Envelope struct {
    InstanceID string          `json:"instanceId"`
    ProjectID  string          `json:"projectId"`
    SessionID  string          `json:"sessionId,omitempty"`
    Topic      string          `json:"topic"`
    Timestamp  time.Time       `json:"timestamp"`
    Payload    json.RawMessage `json:"payload"`
}
```
The ZMQ frame is formed as: `[topic_bytes + 0x00 + json_envelope_bytes]`, allowing subscribers to filter by topic prefix.

**JSON-RPC (ROUTER):** (`bus.go`)
```go
// Request
type rpcRequest struct {
    JSONRPC string          `json:"jsonrpc"`  // "2.0"
    ID      string          `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

// Response
type rpcResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      string          `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *rpcError       `json:"error,omitempty"`
}
```

### Port Assignment (`ports.go`)

- **Deterministic ports (FNV-32a hash):** `PortsForPath(absPath)` → base port = `40000 + (fnv32a(path) % 20000)`, pub = base, rpc = base+1
- **Free ports (fallback):** `FindFreePorts()` → two TCP ports assigned by the OS (for secondary instances that cannot use the deterministic ports already occupied by the primary)
- **Base port range:** 40000-60000

### Primary Instance Lock (`lock_unix.go`)

The **flock** mechanism (exclusive per process) in `.pando/ipc.lock` determines which instance is the "primary":
- **First instance:** acquires the lock (`LOCK_EX|LOCK_NB`), writes its `LockInfo` (instanceID, PID, ports), and becomes primary
- **Subsequent instances:** fail to acquire the lock, read the existing primary's `LockInfo`, and become secondary
- Secondary instances open the database in read-only mode and use DBProxy to write via RPC

### LockInfo (`lock_common.go`)
```go
type LockInfo struct {
    InstanceID string    `json:"instance_id"`
    PID        int       `json:"pid"`
    PubPort    int       `json:"pub_port"`
    RPCPort    int       `json:"rpc_port"`
    StartedAt  time.Time `json:"started_at"`
}
```

### Instance Registry (`instanceregistry/`)

- **Files:**
  - `entry.go` — `Entry` and `Mode` definition (TUI, WebUI, Desktop, ACP, NonInteractive, Proxy)
  - `announce.go` — `Announce()` writes JSON to `/tmp/pando-instances/<instanceID>.json`; `Revoke()` removes it
  - `registry.go` — `Registry.List()` scans `/tmp/pando-instances/`, verifies PIDs are alive (`signal(0)`), and cleans up stale entries
- **Purpose:** Allow any instance to discover all other Pando instances running on the system, regardless of working directory

### PUB Socket Topics (`protocol/topics.go`)

**Sessions:**
- `session.list` — Complete session list
- `session.update` — Session created or updated
- `session.activated` — Active session changed
- `session.deleted` — Session deleted

**Messages:**
- `message.append` — New message added

**LLM (streaming):**
- `llm.token` — Each streaming token from the LLM
- `llm.start` — LLM call start
- `llm.end` — LLM call end (with tokens in/out)

**Tools:**
- `tool.start` — Tool execution start
- `tool.end` — Tool execution end

**Instance:**
- `instance.heartbeat` — Every 5 seconds (liveness)
- `instance.shutdown` — Graceful shutdown

### JSON-RPC Methods (`protocol/rpc.go`)

| Method | Parameters | Description |
|--------|-----------|-------------|
| `instance.ping` | — | Checks if instance is alive. Response: `PingResult{Status, InstanceID, Uptime}` |
| `instance.info` | — | Detailed information |
| `session.list` | — | Lists all sessions |
| `session.get` | `{session_id}` | Gets session by ID |
| `session.activate` | `{session_id}` | Changes active session (publishes `session.activated` event) |
| `message.send` | `{session_id, content}` | Sends message to local agent (starts LLM processing) |
| `message.list` | `{session_id}` | Message history for a session |
| `session.interrupt` | `{session_id}` | Cancels ongoing LLM generation |
| `state.sync` | `{project_id}` | Requests complete state snapshot |

### Bridge — Connecting In-Process Events to the Bus (`bridge/bridge.go`)

The **Bridge** subscribes to internal session and agent events, and re-publishes them on the ZMQ bus:

- `bridgeSessions()` — Subscribes to `session.Service.Subscribe()` events and maps them to PUB topics:
  - `pubsub.CreatedEvent` → `session.update`
  - `pubsub.UpdatedEvent` → `session.update`
  - `pubsub.DeletedEvent` → `session.deleted`
- `bridgeAgent()` — Subscribes to `agent.Service.Subscribe()` events and maps them:
  - `AgentEventTypeContentDelta` → `llm.token` (each token)
  - `AgentEventTypeToolCall` → `tool.start`
  - `AgentEventTypeToolResult` → `tool.end` (result truncated to 512 chars)
  - `AgentEventTypeResponse` → `llm.end`
- `runHeartbeat()` — Publishes `instance.heartbeat` every 5 seconds with uptime and session count

### Bridge RPC Handlers (`bridge/handlers.go`)

`RegisterHandlersWithAgent()` registers all RPC methods on the Bus:
- `instance.ping` → returns status, instanceID, uptime
- `session.list` → lists sessions from `session.Service`
- `session.get` → gets session by ID
- `session.activate` → verifies session exists, publishes `session.activated`, returns OK
- `message.send` → runs `runner.RunMessage()` (local agent) to process message
- `session.interrupt` → runs `interrupter.Cancel()` to cancel LLM
- `message.list` → lists messages from `message.Service`

`RegisterHandlers()` is a simplified version without agent (runner/interrupter set to nil).

### DBProxy — Database Write Proxy (`ipc/dbproxy/`)

- **Files:**
  - `/www/MCP/Pando/pando/internal/ipc/dbproxy/proxy.go` — `DBProxy` implements `db.Querier`
  - `/www/MCP/Pando/pando/internal/ipc/dbproxy/handlers.go` — `RegisterHandlers()` for the Bus
- **Purpose:** Secondary instances open the SQLite database in read-only mode and redirect all writes to the primary instance via ZMQ JSON-RPC
- **RPC Method:** `db.write` with `WriteRequest{Method, Params}`
- **Dispatcher:** `dispatchWrite()` routes to the corresponding `db.Querier` function (CreateSession, UpdateSession, DeleteSession, CreateMessage, UpdateMessage, DeleteMessage, CreateFile, UpdateFile, DeleteFile, InsertPromptTemplate, InsertSessionScore, InsertSkill, DeactivateLowestSkill, IncrementSkillUsage, CreateProject, UpdateProjectStatus, UpdateProjectLastOpened, MarkProjectInitialized, DeleteProject)

### Bus Configuration in Serve/App/TUI Modes

**In TUI mode (primary):** (`cmd/root.go`)
```go
bus := ipc.NewBus(instanceID)
bus.Start(ctx, pubPort, rpcPort)
dbproxy.RegisterHandlers(bus, db.New(conn))     // DB write handlers for secondary instances
bridge.RegisterHandlers(bus, instanceID, ...)    // session/message RPC handlers
pandoApp.SetupIPC(bus)                           // configures bus as session publisher
br := bridge.New(bus, ...)                       // bridge in-process events → ZMQ PUB
br.Start(ctx)
```

**In Serve/App mode:** (`cmd/serve.go` and `cmd/app.go`)
- Announce instance in registry (NOT primary, no lock)
- Create Bus and start with free ports
- Only register `bridge.RegisterHandlers()` (don't register `dbproxy.RegisterHandlers`)
- Create and start `bridge.New()` with CoderAgent as MessageRunner
- The bridge allows serve/app mode to receive incoming messages via RPC and process them with its local agent

**In ACP mode:** (`cmd/root.go` function `runACPServerWithOptions()`)
- Similar to serve/app but with deterministic ports (same-path collision allowed)
- Announce instance as `ModeACP`
- Only register `bridge.RegisterHandlers()` (no dbproxy)
