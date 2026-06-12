# Pando Server Implementation Analysis — Part 2: Detailed REST API Endpoints

## 4. REST API Endpoints

Endpoints are registered in `internal/api/routes.go`. All handlers are in separate files within `internal/api/`.

### System and Project (`handlers_base.go`)
- **GET /health** → `handleHealth()` — Returns `{"status":"healthy","version":"..."}`
- **GET /api/v1/project** → `handleProject()` — CWD and version
- **GET /api/v1/project/context** → `handleProjectContext()` — Project context

### Sessions (`handlers_sessions.go`)
- **GET /api/v1/sessions** — Lists all sessions with `is_running` state
- **GET /api/v1/sessions/{id}** — Gets session with all its messages
- **DELETE /api/v1/sessions/{id}** — Deletes session and its messages
- **PATCH /api/v1/sessions/{id}** — Updates session title
- **GET /api/v1/sessions/{id}/stream** — SSE stream of session events with replay

### Chat / LLM (`handlers_chat.go`)
- **POST /api/v1/chat** — Synchronous chat: sends prompt, waits for complete response
- **GET|POST /api/v1/chat/stream** — Asynchronous chat with SSE streaming:
  - Agent execution is decoupled from HTTP (runs in background)
  - Client can reconnect via `GET /sessions/{id}/stream` and receive event replay
  - Uses `BackgroundSessionManager` to manage lifecycle

### BackgroundSessionManager (`background_runner.go`)
- **File:** `internal/api/background_runner.go`
- Manages background agent executions independent of HTTP connections
- Circular buffer of 500 events per session (replay for reconnections)
- 10-minute TTL for completed sessions (then garbage collection)
- Supports multiple SSE subscribers per session (slow subscribers are skipped)
- Cancellation of active sessions

### MCP Tools (`handlers_tools.go`)
- **GET /api/v1/tools** — Lists available MCP tools with name, description, parameters, and required fields

### Files (`handlers_files.go`)
- **GET /api/v1/files** — Lists files for a session/project
- **GET /api/v1/files/rename** — Renames file
- **GET /api/v1/files/search** — Text search for files
- **GET /api/v1/files/raw/{path}** — Raw file content
- **GET /api/v1/files/{path}** — File metadata by path
- **GET /api/v1/fs/browse** — Filesystem browsing (browse directories)

### Configuration (`handlers_config.go`)
- **GET/PUT /api/v1/settings** — General settings (config read/write)
- **GET /api/v1/settings/providers** — Available LLM providers
- **GET/PUT /api/v1/config/providers** — Provider CRUD (API keys masked on GET)
- **GET/PUT /api/v1/config/agents** — LLM agent configuration
- **GET/PUT /api/v1/config/mcp-servers** — MCP servers
- **DELETE /api/v1/config/mcp-servers/{name}** — Delete MCP server
- **POST /api/v1/config/mcp-servers/{name}/reload** — Reload MCP server
- **GET/PUT /api/v1/config/mcp-gateway** — MCP Gateway configuration
- **GET/PUT /api/v1/config/lsp** — LSP server configuration
- **DELETE /api/v1/config/lsp/{language}** — Delete LSP for language
- **GET/PUT /api/v1/config/tools** — Internal tools configuration
- **GET /api/v1/config/browsers** — Browser configuration
- **GET/PUT /api/v1/config/openlit** — OpenLit configuration (observability)
- **GET/PUT /api/v1/config/bash** — Shell configuration
- **GET/PUT /api/v1/config/extensions** — Tool extensions
- **GET/PUT /api/v1/config/services** — Services
- **GET/PUT /api/v1/config/evaluator** — Evaluator configuration (self-improvement)
- **GET /api/v1/config/provider-accounts** — Lists provider accounts
- **POST /api/v1/config/provider-accounts** — Creates provider account
- **GET /api/v1/config/provider-types** — Lists available provider types
- **GET/PUT/DELETE /api/v1/config/provider-accounts/{id}** — Specific account CRUD
- **POST /api/v1/config/provider-accounts/{id}/test** — Test connection
- **POST /api/v1/config/api-server/regenerate-token** — Regenerate API token
- **GET /api/v1/config/init-status** — Configuration initialization status
- **POST /api/v1/config/generate** — Generates initial configuration

### Container Runtime (`handlers_container.go`)
- **GET /api/v1/container/capabilities** — Runtime capabilities
- **GET /api/container/config** — Container configuration (legacy route without /v1)
- **GET /api/v1/container/sessions** — Active sessions in containers
- **POST /api/v1/container/sessions/{sessionId}/stop** — Stops session in container
- **GET /api/v1/container/events** — Container runtime events
- **GET /api/v1/container/images** — Lists available images
- **DELETE /api/v1/container/images/{ref...}** — Deletes image
- **POST /api/v1/container/images/gc** — Image garbage collection

### Events and SSE Notifications
- **GET /api/v1/config/events** — SSE for configuration hot-reload
- **GET /api/v1/notifications/stream** — SSE for user notifications (LLM errors, tool errors, LSP diagnostics)

### Remembrances (RAG + Code Indexing)
- **GET /api/v1/remembrances/projects** — Indexed projects
- **POST /api/v1/remembrances/projects/index** — Indexes a code project

### Skills
- **GET /api/v1/skills/installed** — Installed skills
- **GET /api/v1/skills/catalog** — Available skills catalog
- **POST /api/v1/skills/install** — Installs a skill
- **DELETE /api/v1/skills/{name}** — Uninstalls skill

### Logs
- **GET /api/v1/logs** — Gets historical logs
- **GET /api/v1/logs/stream** — SSE streaming of real-time logs

### Orchestrator (Mesnada)
- **GET /api/v1/orchestrator/tasks** — Lists orchestrator tasks
- **POST /api/v1/orchestrator/tasks** — Creates new task
- **GET /api/v1/orchestrator/tasks/{id}** — Gets task by ID
- **DELETE /api/v1/orchestrator/tasks/{id}** — Deletes task
- **POST /api/v1/orchestrator/tasks/{id}/cancel** — Cancels task

### Terminal
- **POST /api/v1/terminal/exec** — Executes command in terminal and returns result

### Snapshots (`handlers_snapshots.go`)
- **GET /api/v1/snapshots/count** — Snapshot count
- **GET /api/v1/snapshots** — Lists snapshots
- **POST /api/v1/snapshots** — Creates snapshot
- **GET /api/v1/snapshots/{id}** — Gets snapshot by ID
- **POST /api/v1/snapshots/{id}/apply** — Applies snapshot
- **POST /api/v1/snapshots/{id}/revert** — Reverts snapshot
- **DELETE /api/v1/snapshots/{id}** — Deletes snapshot

### Evaluator
- **GET /api/v1/evaluator/metrics** — Evaluation metrics
- **GET /api/v1/evaluator/templates** — Prompt templates
- **GET /api/v1/evaluator/skills** — Evaluator skills
- **GET /api/v1/evaluator/sessions** — Evaluated sessions

### Models
- **GET /api/v1/models** — Lists available models
- **PUT /api/v1/models/active** — Sets active model

### Personas
- **GET /api/v1/personas** — Lists personas
- **GET /api/v1/personas/active** — Gets active persona
- **PUT /api/v1/personas/active** — Sets active persona

### CronJobs
- **GET /api/v1/cronjobs** — Lists cron jobs
- **POST /api/v1/cronjobs** — Creates cron job
- **PUT/DELETE /api/v1/cronjobs/{name}** — Updates/deletes cron job
- **POST /api/v1/cronjobs/{name}/run** — Runs cron job immediately

### Projects
- **GET /api/v1/projects** — Lists projects
- **POST /api/v1/projects** — Creates project
- **GET /api/v1/projects/active** — Gets active project
- **GET /api/v1/projects/events** — Project events
- **GET /api/v1/projects/{id}** — Gets project by ID
- **DELETE /api/v1/projects/{id}** — Deletes project
- **PATCH /api/v1/projects/{id}** — Renames project
- **POST /api/v1/projects/{id}/activate** — Activates project
- **POST /api/v1/projects/{id}/deactivate** — Deactivates project
- **POST /api/v1/projects/{id}/init** — Initializes project

### Instances (observation and remote control via IPC) (`handlers_instances.go`)
- **GET /api/v1/instances** — Lists all live Pando instances
- **GET /api/v1/instances/{id}** — Gets instance by ID
- **GET /api/v1/instances/{id}/stream** — Proxies ZMQ PUB stream as SSE
- **GET /api/v1/instances/{id}/sessions** — Lists remote sessions via `session.list` RPC
- **GET /api/v1/instances/{id}/sessions/{sid}** — Gets remote session via `session.get` RPC
- **GET /api/v1/instances/{id}/sessions/{sid}/stream** — Proxies PUB stream filtered by session_id
- **DELETE /api/v1/instances/{id}/sessions/{sid}/cancel** — Interrupts remote LLM generation via `session.interrupt` RPC
- **POST /api/v1/instances/{id}/sessions/{sid}/message** — Sends message to remote session via `message.send` RPC
