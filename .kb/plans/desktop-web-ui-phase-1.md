# Desktop Web UI Implementation for Pando (Phase 1)

## Pando Engine HTTP Server and API (The Engine Server)

**Goal:** Develop or enable an embedded HTTP server in Pando (`pando serve`) to provide the backend API that will interact with the Web UI.

**Status: COMPLETED** ✅

### Implemented Components:

1. **`pando serve` Command (cmd/serve.go):**
   - Flags: --host, --port, --debug
   - Default port: 8765
   - Integration with Pando's app.App
   - Graceful shutdown with SIGINT/SIGTERM signals

2. **HTTP Server (internal/api/):**
   - Local token authentication
   - CORS middleware for WebUI
   - SSE support for streaming

3. **Implemented REST Endpoints:**
   - `GET /health` - Health check
   - `GET /api/v1/token` - Get authentication token
   - `GET /api/v1/project` - Current project info
   - `GET /api/v1/project/context` - Project context
   - `GET /api/v1/sessions` - Session list
   - `GET /api/v1/sessions/:id` - Session detail with messages
   - `GET /api/v1/tools` - Available MCP tools list
   - `GET /api/v1/files` - Project file navigation
   - `GET /api/v1/files/:path` - Read specific file
   - `POST /api/v1/chat` - Send prompt (synchronous response)
   - `GET/POST /api/v1/chat/stream` - SSE streaming of LLM responses

4. **SSE Streaming:**
   - Connects with `CoderAgent.Run()` for streaming
   - Events: session, content, done, error
   - Compatible with browser EventSource API

5. **Local Authentication:**
   - Token generated when server starts
   - Header `X-Pando-Token` or query param `?token=`
   - Public endpoints: /health, /api/v1/token

6. **Configuration:**
   - `[server]` section in .pando.toml
   - Fields: enabled, host, port, requireAuth
   - Defaults: localhost:8765, requireAuth=true

### Completion Criteria Met:
- ✅ The `pando serve` server starts a local API on port 8765
- ✅ Endpoints exist to return project context
- ✅ Endpoints exist to list sessions
- ✅ SSE endpoint exists to send prompts to the agent

### Files Created:
- `cmd/serve.go` - CLI command
- `internal/api/server.go` - HTTP server
- `internal/api/routes.go` - Route registration
- `internal/api/handlers_base.go` - Base handlers
- `internal/api/handlers_sessions.go` - Session handlers
- `internal/api/handlers_tools.go` - Tools handler
- `internal/api/handlers_files.go` - File handlers
- `internal/api/handlers_chat.go` - Chat/SSE handlers

### Next Step:
Continue with **Phase 2: Frontend SolidJS UI** - Create the web interface that consumes this API.
