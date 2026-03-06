# Crush Repository Structure

## Overview
Crush is a terminal-based AI coding assistant by Charmbracelet, written in Go. It provides multi-model LLM support with a rich TUI, session management, MCP/LSP integration, and extensible tool system.

## Entry Points
- **`main.go`** (root): Minimal entry — loads `.env`, optionally starts pprof, delegates to `internal/cmd.Execute()`.
- **`internal/cmd/root.go`**: Cobra CLI root command. Subcommands: `run`, `dirs`, `projects`, `update-providers`, `logs`, `schema`, `login`, `stats`. Flags include `--cwd`, `--data-dir`, `--debug`, `--yolo`.
- **`internal/app/app.go`**: Application wiring — creates DB, services (session, message, history, permission, filetracker), coordinator, LSP manager, MCP init, TUI via Bubble Tea.

## Directory Map (`internal/`)
- **`agent/`**: Core orchestration layer. Contains `coordinator.go` (multi-provider agent coordination), `agent.go` (session-based agent with queuing, auto-summarize, title generation), `agent_tool.go` (sub-agent tool), `prompts.go` (prompt templates), `event.go`, `loop_detection.go`.
  - **`agent/tools/`**: Built-in tools — `bash.go`, `edit.go`, `multiedit.go`, `view.go`, `glob.go`, `grep.go`, `fetch.go`, `write.go`, `search.go`, `sourcegraph.go`, `todos.go`, `diagnostics.go`, `references.go`, `ls.go`, `download.go`, `web_fetch.go`, `web_search.go`, `job_kill.go`, `job_output.go`, `lsp_restart.go`, `list_mcp_resources.go`, `read_mcp_resource.go`.
  - **`agent/tools/mcp/`**: MCP client integration — `init.go` (session management, connection lifecycle), `tools.go`, `resources.go`, `prompts.go`.
  - **`agent/hyper/`**: Charm's Hyper provider integration.
  - **`agent/prompt/`**: Prompt builder with template expansion.
  - **`agent/templates/`**: Go-embedded `.md.tpl` files for coder, task, agent_tool, agentic_fetch, summary, title, initialize prompts.
- **`config/`**: Configuration loading, resolution, provider management, catwalk sync, copilot OAuth. Files: `config.go`, `load.go`, `resolve.go`, `provider.go`, `hyper.go`, `catwalk.go`, `copilot.go`, `init.go`.
- **`cmd/`**: CLI commands — `root.go`, `run.go`, `dirs.go`, `models.go`, `projects.go`, `login.go`, `logs.go`, `schema.go`, `stats.go`, `update_providers.go`.
- **`ui/`**: Bubble Tea TUI with 15+ sub-packages: `model/` (main UI model), `chat/` (chat rendering), `completions/`, `styles/`, `diffview/`, `attachments/`, `anim/`, `image/`, etc.
- **`session/`**: Session CRUD with SQLite via sqlc, todo tracking.
- **`message/`**: Message types, content parts, attachments, tool calls/results.
- **`db/`**: sqlc-generated DB layer (SQLite).
- **`permission/`**: Permission service for tool execution approval.
- **`lsp/`**: LSP client manager for language server integration.
- **`skills/`**: Agent Skills standard (`SKILL.md` parser, agentskills.io spec).
- **`history/`**: File edit history tracking.
- **`filetracker/`**: Tracks files touched during sessions.
- **`shell/`**: Shell execution utilities.
- **`diff/`**: Diff computation.
- **`format/`**: Output formatting.
- **`pubsub/`**: Event broker for internal messaging.
- **`csync/`**: Concurrent-safe generic containers (Value, Slice, Map).
- **`oauth/`**: OAuth2 flows for Copilot and Hyper.
- **`event/`**: Event types for pubsub.
- **`env/`**, **`home/`**, **`log/`**, **`update/`**, **`version/`**, **`stringext/`**, **`filepathext/`**, **`fsext/`**, **`ansiext/`**: Utility packages.

## Key Dependencies
- **`charm.land/fantasy`**: LLM abstraction layer (Agent, LanguageModel, AgentTool, streaming).
- **`charm.land/catwalk`**: Model catalog / provider registry with cost tracking.
- **`charm.land/bubbletea/v2`**: TUI framework.
- **`charm.land/lipgloss/v2`**: Terminal styling.
- **`github.com/modelcontextprotocol/go-sdk/mcp`**: Official Go MCP SDK.
- **`github.com/spf13/cobra`**: CLI framework.
- **`github.com/openai/openai-go/v2`**: OpenAI SDK.

## Architecture Pattern
Coordinator → SessionAgent → fantasy.Agent → Provider (Anthropic/OpenAI/Google/etc.)
Each provider is built via `buildProvider()` switch in `coordinator.go` supporting: openai, anthropic, openrouter, vercel, azure, bedrock, google, google-vertex, openai-compat, hyper.
