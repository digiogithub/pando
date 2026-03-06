# Pando Repository Structure

## Overview
Pando is a Go-based terminal AI assistant forked from the archived **OpenCode** project (github.com/digiogithub/pando). The original project evolved into **Crush** by Charmbracelet with a license change. Pando continues under MIT license.

## Entrypoint & CLI
- **[main.go]**: Application entrypoint. Calls `cmd.Execute()` wrapped in `logging.RecoverPanic`.
- **[cmd/root.go]**: Cobra root command `opencode`. Parses flags (`-d` debug, `-c` cwd, `-p` prompt, `-f` format, `-q` quiet). Loads config via `config.Load()`, connects SQLite via `db.Connect()`, creates `app.New()`, initializes MCP tools, then launches either non-interactive prompt mode or interactive TUI via BubbleTea.
- **[cmd/schema/]**: JSON schema definitions for the config format.

## Runtime Architecture
- **CLI mode** (`-p` flag): Creates session, auto-approves permissions, runs `CoderAgent.Run()`, prints output, exits.
- **TUI mode** (default): Uses [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) with `bubblezone` for mouse support. PubSub system routes events from services (sessions, messages, permissions, coderAgent) to TUI via channels.
- **LSP integration**: Background-initialized LSP clients per language. Supports diagnostics exposed to AI.
- **Database**: SQLite via `database/sql` + sqlc-generated queries. Stores sessions, messages, and file history.

## Directory Layout
- **[internal/app/]**: Core `App` struct orchestrating Sessions, Messages, History, Permissions, CoderAgent, LSPClients. `app.go` is the central wiring point.
- **[internal/config/]**: Viper-based config loading from `$HOME/.opencode.json`, `$XDG_CONFIG_HOME/opencode/.opencode.json`, or `./.opencode.json`. Defines MCPServer, Agent, Provider, LSPConfig, ShellConfig structs.
- **[internal/db/]**: sqlc-generated code. Tables: sessions, messages, files. Migrations in `[internal/db/migrations/]` (initial 20250424, add_summary 20250515). `connect.go` handles SQLite connection setup.
- **[internal/llm/]**: LLM subsystem with subdirs: `agent/` (agent orchestration), `models/` (model definitions), `prompt/` (system prompts), `provider/` (per-provider implementations), `tools/` (AI tool definitions).
- **[internal/llm/agent/]**: `agent.go` (agent service interface + run loop), `agent-tool.go` (sub-agent tool), `mcp-tools.go` (MCP tool discovery), `tools.go` (tool registry).
- **[internal/llm/provider/]**: Provider implementations: `anthropic.go`, `openai.go`, `gemini.go`, `bedrock.go`, `copilot.go`, `azure.go`, `vertexai.go`, `provider.go` (common interface).
- **[internal/llm/tools/]**: Built-in tools: `bash.go`, `edit.go`, `write.go`, `view.go`, `glob.go`, `grep.go`, `ls.go`, `patch.go`, `fetch.go`, `diagnostics.go`, `sourcegraph.go`.
- **[internal/tui/]**: TUI components: `components/`, `layout/`, `page/`, `styles/`, `theme/`, `image/`, `util/`. Entry `tui.go`.
- **[internal/lsp/]**: LSP client: `client.go`, `transport.go`, `handlers.go`, `methods.go`, `language.go`, `protocol/`, `watcher/`.
- **[internal/session/]**: Session service (CRUD + pubsub).
- **[internal/message/]**: Message service (CRUD + pubsub).
- **[internal/history/]**: File change history tracking per session.
- **[internal/permission/]**: Permission service for tool execution approval.
- **[internal/pubsub/]**: Generic pub/sub event system used across services.
- **[internal/logging/]**: Structured logging with panic recovery.
- **[internal/diff/]**: Diff utilities for file changes.
- **[internal/fileutil/]**: File utility helpers.
- **[internal/format/]**: Output formatting (text/json) and spinner.
- **[internal/completions/]**: Shell completion support.
- **[internal/version/]**: Version info.

## Key Milestones (from git)
- Initial schema migration: `[internal/db/migrations/20250424200609_initial.sql]`
- Summary message support: `[internal/db/migrations/20250515105448_add_summary_message_id.sql]`
- GitHub Copilot provider added: commit `b9bedba` (`[internal/llm/provider/copilot.go]`)
- MCP nil fix: commit `1f6eef4`
- Grep tool fix (always show filenames): commit `f0571f5` (`[internal/llm/tools/grep.go]` via rg)

## Build & Release
- **[.goreleaser.yml]**: GoReleaser config for cross-platform builds.
- **[sqlc.yaml]**: sqlc config for DB query code generation.
- Go 1.24.0+ required.
