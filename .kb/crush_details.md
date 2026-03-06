# Crush Implementation Details

## Configuration Flow
- **Loading**: `config/load.go` loads config from `crush.json` (project-level) + global config. Merges with env vars. Supports `$ENV_VAR` resolution in API keys via `config/resolve.go`.
- **Provider resolution**: `config/provider.go:Providers()` fetches model catalogs from Catwalk API (with 45s timeout), caches to `~/.local/share/crush/providers.json`. Falls back to embedded providers if network fails.
- **Hyper sync**: Separate sync path for Charm's Hyper provider (`config/hyper.go`).
- **Context paths**: `defaultContextPaths` array scans for `.cursorrules`, `CLAUDE.md`, `AGENTS.md`, etc. at project root. Read and injected into system prompt via `agent/prompt/prompt.go`.
- **Agent definitions**: `config.go` defines `AgentCoder` and `AgentTask` agent types with `AllowedTools` and `AllowedMCP` restrictions.

## Agent Orchestration
- **Coordinator pattern** (`coordinator.go`): Single coordinator manages one `currentAgent` (coder). TODOs in code indicate planned multi-agent support ("when we support multiple agents").
- **SessionAgent** (`agent.go`): Wraps `fantasy.Agent` with session-aware streaming. Key features:
  - Concurrent-safe via `csync.Value`, `csync.Slice`, `csync.Map` for models, tools, prompts, active requests.
  - Message queue for sequential prompt processing per session.
  - Loop detection (`loop_detection.go`) with configurable window size and max repeats.
  - Auto-summarize with token threshold monitoring in `StopWhen` conditions.
  - Reasoning content handling for Anthropic (signatures), Google (thought signatures), OpenAI (encrypted reasoning).
- **Sub-agents**: `agent_tool.go` creates task agent sessions as children of the main session. Cost accumulates to parent.

## Provider Abstraction
- **Fantasy library** (`charm.land/fantasy`): Core LLM abstraction. `fantasy.Agent` handles streaming, tool calls, step management. `fantasy.LanguageModel` interface per provider.
- **Provider builder** (`coordinator.go:buildProvider()`): Switch on `providerCfg.Type` to construct provider-specific SDK instances. Each provider has its own builder method (e.g., `buildAnthropicProvider`, `buildOpenaiProvider`, etc.).
- **Provider options merging**: `getProviderOptions()` merges options from 3 sources (catwalk defaults → provider config → model config) using JSON merge. Provider-specific option parsing (e.g., `anthropic.ParseOptions`, `openai.ParseOptions`).
- **OAuth2 refresh**: Automatic token refresh on 401 errors for OAuth providers. API key template re-resolution for dynamic keys.

## Database Layer
- **SQLite + sqlc**: `sqlc.yaml` config generates Go code from SQL queries. Tables: sessions, messages, files.
- **Session model** (`session/session.go`): ID, title, cost, prompt/completion tokens, todos (JSON), summary message ID.
- **Message model** (`message/message.go`): Role (user/assistant/tool), parts (text, binary, tool calls, tool results), model, provider, reasoning content.
- **File tracker** (`filetracker/service.go`): Tracks files read/written per session for context.

## TUI Architecture
- **Bubble Tea v2**: Main model in `ui/model/ui.go`. Sub-models: `chat.go`, `session.go`, `sidebar.go`, `header.go`, `status.go`, `pills.go`, `landing.go`, `history.go`, `filter.go`, `mcp.go`, `onboarding.go`, `lsp.go`, `keys.go`, `clipboard.go`.
- **Chat rendering** (`ui/chat/`): Separate renderers for user/assistant/tool messages, bash output, diff views, todos, diagnostics, MCP, references, search, file content, agent output.
- **Diff view** (`ui/diffview/`): Syntax-highlighted diff rendering with Chroma. Unified and split modes.
- **Styles** (`ui/styles/`): Centralized style definitions with gradient support.

## Logging
- **Structured logging**: `log/` package provides `slog`-based logging. `log.NewHTTPClient()` wraps HTTP client for request/response logging in debug mode.
- **Log messages**: Must start with capital letter (enforced by linter `task lint:log`).

## Event System
- **PubSub**: `pubsub/` provides generic `Broker[T]` for decoupled event communication. Used by MCP state changes, session updates, etc.
- **Events** (`event/`): Typed events for prompt sent, prompt responded, tokens used, etc.

## Skills System
- **SKILL.md parsing** (`skills/skills.go`): YAML frontmatter + markdown body. Validates name pattern (`^[a-zA-Z0-9]+(-[a-zA-Z0-9]+)*$`), length limits. Skills discovered via filesystem walk.
- **agentskills.io**: Implements the open standard for agent skills.

## Concurrency Primitives
- **csync package**: `csync.Value[T]` (atomic value), `csync.Slice[T]` (concurrent slice), `csync.Map[K,V]` (concurrent map). Used throughout agent and config for thread-safe state.

## Build & Development
- **Taskfile.yaml**: Task runner with commands: `dev`, `test`, `lint`, `lint:fix`, `lint:log`, `fmt`, `modernize`.
- **GoReleaser** (`.goreleaser.yml`): Multi-platform release builds.
- **golangci-lint** (`.golangci.yml`): Linting configuration with gofumpt.
