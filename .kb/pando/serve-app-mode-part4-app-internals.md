# Analysis of the Pando Server Implementation — Part 4: App Internals and Services

## 6. App — Application Core

### `internal/app/app.go`
- **File:** `/www/MCP/Pando/pando/internal/app/app.go` (~1500 lines)
- **`App` Structure:**
```go
type App struct {
    Sessions    session.Service      // Session service
    Messages    message.Service      // Message service
    History     history.Service      // File history service
    Permissions permission.Service   // Permissions service (auto-approval)
    CoderAgent  agent.Service        // Main LLM agent

    Projects            *project.Service       // Project management
    ProjectManager      *project.Manager       // Project manager (lifecycle)
    Snapshots           *snapshot.Service      // Session snapshots
    LSPClients          map[string]*lsp.Client // LSP clients per language
    SkillManager        *skills.SkillManager   // Skill manager
    MesnadaOrchestrator *mesnadaOrch.Orchestrator // Mesnada orchestrator
    CronService         *cronjob.Service       // Cron jobs service
    MesnadaServer       *mesnadaServer.Server  // Orchestrator HTTP server
    Remembrances        *rag.RemembrancesService // RAG/KB/code indexing service
    LuaManager          *luaengine.FilterManager // Lua filter manager
    MCPGateway          *mcpgateway.Gateway    // MCP server gateway with favorites
    Evaluator           *evaluator.EvaluatorService // Self-improvement

    IPCBus       *ipc.Bus    // ZMQ bus (primary only)
    IPCIsPrimary bool        // Is primary instance?
}
```

### Initialization in `New()` (sequential process):

1. **Base services:** Sessions, Messages, History, Projects, Permissions
2. **ProjectManager:** Project manager with lifecycle capabilities
3. **Global auto-registration:** Registers the CWD as a global project for cross-instance discovery
4. **Theme:** Initializes theme according to configuration
5. **Skills:** If enabled, discovers and loads skills from configured paths
6. **LSP Clients:** Initializes LSP clients in background (skipped in ACP mode)
7. **Dynamic models:** Refreshes models from configured providers
8. **OpenLit:** Initializes OpenLit for observability (distributed tracing)
9. **Remembrances (RAG):**
   - Semantic search service (KB, code indexing, events)
   - Syncs KB documents from disk
   - Automatically indexes sessions
   - **Context Enricher:** Searches for relevant context before each user prompt
10. **Lua Filter Manager:** Lua filters for customizing agent behavior (if enabled)
11. **Snapshots:** Snapshot service (configurable auto-cleanup)
12. **Evaluator:** Self-improvement system with UCB selection of templates and skills
13. **Browser Registry:** Initializes browser registry for web browsing
14. **MCP Gateway:** Gateway managing MCP servers with usage-based favorites system
15. **Mesnada Orchestrator:** Sub-agent orchestrator (if enabled) with:
    - Cron jobs service
    - Optional ACP (Agent Communication Protocol) HTTP server
    - Embedded orchestrator HTTP server
16. **CoderAgent:** Main LLM agent with all configured tools
17. **Persona Manager:** Persona manager (built-in + user-defined)
18. **Persona Selector:** Automatic persona selection

### `Shutdown()` — Ordered shutdown:
1. MesnadaServer → CronService → MesnadaOrchestrator
2. Snapshot cleanup → Browser sessions → Watchers → LSP clients
3. OpenLit shutdown → Project manager → IPC bus

### `SetupIPC(bus)`:
Configures the ZMQ bus as an IPC publisher for the session service (`session.SetIPCPublisher(bus)`), allowing session events to be broadcast to other instances.

---

## 7. SSE Streaming Mechanism in Chat

The streaming chat flow works as follows:

1. **POST /api/v1/chat/stream** receives `{sessionId, prompt}`
2. Creates/gets session with `getOrCreateSession()`
3. Sets SSE headers (`text/event-stream`)
4. Sends `session` event with `{sessionId, running:true}`
5. **Submits** to the `BackgroundSessionManager` which runs the agent in a goroutine with `context.Background()` (independent of HTTP)
6. **Subscribes** to the session event channel
7. `streamSessionEvents()` iterates events from the channel:
   - `contentDelta` → SSE `content` event with text delta
   - `toolCall` → SSE `tool_call` event with name and parameters
   - `toolResult` → SSE `tool_result` event with result/error
   - `response` → SSE `done` event (completion)
   - `error` → SSE `error` event
   - Special handling of tool calls with pending inputs (tool-use with confirmation)

8. If the client disconnects, the agent continues running in the background
9. **Reconnection:** `GET /api/v1/sessions/{id}/stream` allows reconnecting and receiving buffered event replay

---

## 8. Relationship between Modes and Features

| Feature | TUI (primary) | TUI (secondary) | Serve | App | ACP |
|---|---|---|---|---|---|
| **REST HTTP Server** | ✗ | ✗ | ✓ | ✓ + WebUI | ✗ |
| **ZMQ Bus (PUB/ROUTER)** | ✓ (primary) | ✗ | ✓ | ✓ | ✓ |
| **Events → ZMQ Bridge** | ✓ | ✗ | ✓ | ✓ | ✓ |
| **DBProxy writes** | ✗ | ✓ | ✗ | ✗ | ✗ |
| **Session management** | ✓ | ✓ (local read, proxy write) | ✓ | ✓ | ✓ |
| **Primary lock** | ✓ | ✗ | ✗ | ✗ | ✗ |
| **Instance Registry** | ✓ | ✓ | ✓ | ✓ | ✓ |
| **Context Enricher** | ✓ | ✓ | ✓ | ✓ | ✓ |
| **Auto-open browser** | ✗ | ✗ | ✗ | ✓ | ✗ |

---

## 9. Key Files Summary

| File | Purpose |
|------|---------|
| `cmd/serve.go` | `pando serve` command (HTTP server without WebUI) |
| `cmd/app_command.go` | `pando app` command (HTTP server with WebUI) |
| `cmd/app.go` | Shared `runAppMode()` logic |
| `cmd/root.go` | TUI mode (interactive, with full IPC and lock) |
| `internal/api/server.go` | HTTP server: Server struct, NewServer, middleware, HTTP server config |
| `internal/api/routes.go` | All API routes registration (+ writeJSON/writeError utilities) |
| `internal/api/handlers_base.go` | Handlers: health, project, project context |
| `internal/api/handlers_chat.go` | Handlers: sync chat, chat stream, session stream |
| `internal/api/handlers_sessions.go` | Session CRUD handlers |
| `internal/api/handlers_config.go` | Configuration handlers (providers, agents, LSP, etc.) |
| `internal/api/handlers_container.go` | Container runtime handlers |
| `internal/api/handlers_cronjobs.go` | Cron jobs CRUD handlers |
| `internal/api/handlers_evaluator.go` | Evaluator handlers |
| `internal/api/handlers_extras.go` | Miscellaneous handlers |
| `internal/api/handlers_files.go` | File handlers |
| `internal/api/handlers_instances.go` | Instance handlers (IPC → REST proxy) |
| `internal/api/handlers_logs.go` | Log handlers |
| `internal/api/handlers_models.go` | LLM model handlers |
| `internal/api/handlers_notifications.go` | Notification SSE |
| `internal/api/handlers_orchestrator.go` | Mesnada orchestrator handlers |
| `internal/api/handlers_personas.go` | Persona handlers |
| `internal/api/handlers_projects.go` | Project CRUD handlers |
| `internal/api/handlers_provider_accounts.go` | Provider account handlers |
| `internal/api/handlers_remembrances.go` | Remembrances handlers (RAG/index) |
| `internal/api/handlers_settings.go` | Settings handlers |
| `internal/api/handlers_snapshots.go` | Snapshot handlers |
| `internal/api/handlers_terminal.go` | Terminal handler |
| `internal/api/handlers_tools.go` | MCP tools handler |
| `internal/api/handlers_browser_config.go` | Browser configuration handler |
| `internal/api/handlers_config_events.go` | Configuration event SSE |
| `internal/api/handlers_config_init.go` | Initialization/config generate handlers |
| `internal/api/background_runner.go` | BackgroundSessionManager for async sessions |
| `internal/api/ui_assets_app.go` | Embedded WebUI (embed.FS) |
| `internal/app/app.go` | App struct, New(), Shutdown(), SetupIPC() |
| `internal/ipc/bus.go` | ZMQ bus (PUB+ROUTER) with JSON-RPC |
| `internal/ipc/client.go` | ZMQ client (SUB+DEALER) with multi-endpoint support |
| `internal/ipc/envelope.go` | Standard envelope for PUB messages |
| `internal/ipc/ports.go` | Port assignment (FNV hash + fallback) |
| `internal/ipc/lock_common.go` | LockInfo and lock file paths |
| `internal/ipc/lock_unix.go` | flock for instance primacy |
| `internal/ipc/options.go` | Connection/call timeouts |
| `internal/ipc/errors.go` | IPC protocol errors |
| `internal/ipc/protocol/rpc.go` | JSON-RPC constants and types |
| `internal/ipc/protocol/topics.go` | PUB topic constants |
| `internal/ipc/protocol/payloads.go` | Event payload types |
| `internal/ipc/bridge/bridge.go` | In-process events → ZMQ PUB bridge |
| `internal/ipc/bridge/handlers.go` | JSON-RPC handlers registered on the Bus |
| `internal/ipc/dbproxy/proxy.go` | DBProxy: database write proxy via ZMQ |
| `internal/ipc/dbproxy/handlers.go` | db.write RPC handlers |
| `internal/instanceregistry/entry.go` | Entry and Mode types for instances |
| `internal/instanceregistry/announce.go` | Announce/revoke instance in /tmp |
| `internal/instanceregistry/registry.go` | List/get living instances |
