# Pando Technical Details

## Configuration Flow
- **[internal/config/config.go]**: Uses Viper for config loading. Config struct includes `Data`, `Provider` map, `Agent` map (AgentCoder/Task/Summarizer/Title), `MCPServer` map, `LSPConfig` map, `ShellConfig`, `TUIConfig`, `Debug`/`DebugLSP` bools, `AutoCompact` bool.
- Config resolution order: `$HOME/.opencode.json` → `$XDG_CONFIG_HOME/opencode/.opencode.json` → `./.opencode.json` (local overrides global).
- `config.Load(cwd, debug)` called from `cmd/root.go` before any service initialization.
- `config.Get()` returns a global singleton after Load.
- Agent names defined as constants: `AgentCoder`, `AgentSummarizer`, `AgentTask`, `AgentTitle`.
- `MCPType` supports `stdio` and `sse` transports.
- `models.ModelID` type used for model references across the codebase.

## Database Layer
- **[internal/db/connect.go]**: SQLite connection with auto-migration support.
- **[internal/db/db.go]**: sqlc-generated `Queries` struct with prepared statements for all CRUD operations on sessions, messages, and files.
- **[internal/db/migrations/]**: SQL migration files run automatically on connect.
  - `20250424200609_initial.sql`: Creates sessions, messages, files tables.
  - `20250515105448_add_summary_message_id.sql`: Adds summary_message_id to sessions.
- **[internal/db/models.go]**: sqlc-generated Go model types.
- **[internal/db/sql/]**: Raw SQL query definitions consumed by sqlc: `files.sql`, `messages.sql`, `sessions.sql`.
- **[internal/db/embed.go]**: Embeds migration SQL files for distribution.

## Agent System Internals
- **[internal/llm/agent/agent.go]**: `Service` interface with `Run(ctx, sessionID, prompt)` returning a done channel. Manages tool execution loop: send message → receive response → if tool calls, execute tools → send results → repeat until done.
- **[internal/llm/agent/tools.go]**: `CoderAgentTools()` builds the tool set: file tools (glob, grep, ls, view, write, edit, patch), system tools (bash, fetch, sourcegraph), diagnostics, and sub-agent tool.
- **[internal/llm/agent/agent-tool.go]**: The `agent` tool itself, enabling sub-task delegation to a Task agent.
- **[internal/llm/agent/mcp-tools.go]**: `GetMcpTools()` discovers and registers MCP server tools. Called with 30s timeout at startup.

## Provider Layer
- **[internal/llm/provider/provider.go]**: Common provider interface/types. Each provider implements chat completion with streaming support.
- Provider implementations follow a consistent pattern: construct client from config, implement `Send()` for messages, handle tool calls in responses.
- **[internal/llm/models/]**: Model definitions with IDs, context sizes, capabilities per provider.

## Tool Implementation Pattern
- **[internal/llm/tools/tools.go]**: Tool registration and base types. Each tool implements a standard interface with name, description, parameters schema, and execute function.
- Tools receive context including permissions service, session info, and LSP clients.
- **[internal/llm/tools/bash.go]**: Shell execution with configurable shell path/args from config. Supports timeout parameter.
- **[internal/llm/tools/edit.go]**: File editing with old_string/new_string replacement pattern.
- **[internal/llm/tools/patch.go]**: Unified diff application to files.
- **[internal/llm/tools/fetch.go]**: URL fetching with format control (text/markdown/html).
- **[internal/llm/tools/sourcegraph.go]**: Public code search via Sourcegraph API.

## PubSub System
- **[internal/pubsub/]**: Generic typed pub/sub. `Event[T]` carries typed payloads. Services (sessions, messages, permissions, agent) publish events. TUI subscribes via `setupSubscriptions()` in `cmd/root.go`.
- Channel-based with buffered output channel (size 100) and 2s timeout on slow consumers.

## Logging
- **[internal/logging/]**: Structured logging with `RecoverPanic()` wrapper used extensively. Supports `Info`, `Warn`, `Error`, `Debug`, `ErrorPersist` levels. Panic recovery includes optional callback for graceful degradation.
- Log events are also published via pubsub for TUI log viewer.

## LSP Client
- **[internal/lsp/client.go]**: Full LSP client implementation supporting initialize, textDocument/didOpen, didChange, didClose, diagnostics.
- **[internal/lsp/transport.go]**: stdio transport for communicating with language servers.
- **[internal/lsp/handlers.go]**: Notification and response handlers.
- **[internal/lsp/watcher/]**: File system watcher for notifying LSP of file changes.
- **[internal/lsp/language.go]**: Language ID detection for LSP.

## Diff & File Utilities
- **[internal/diff/]**: Diff computation for file change visualization.
- **[internal/fileutil/]**: Path resolution, file existence checks, safe file operations.
- **[internal/format/]**: Output formatting (text/json), spinner animation, validation helpers.

## TUI Architecture
- **[internal/tui/tui.go]**: Root BubbleTea model.
- **[internal/tui/page/]**: Page-level components (chat page, logs page).
- **[internal/tui/components/]**: Reusable UI components.
- **[internal/tui/layout/]**: Layout management.
- **[internal/tui/styles/]**: Lipgloss style definitions.
- **[internal/tui/theme/]**: Theme system with configurable themes.
- **[internal/tui/image/]**: Image rendering support in terminal.
