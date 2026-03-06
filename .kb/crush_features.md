# Crush Features

## Multi-Model LLM Support
- **12+ provider backends**: OpenAI, Anthropic, Google Gemini, Azure, AWS Bedrock, OpenRouter, Vercel AI Gateway, Google Vertex, Groq, Cerebras, HuggingFace, io.net, MiniMax, Z.ai, Synthetic, Copilot — all via `coordinator.go:buildProvider()` switch.
- **Mid-session model switching**: Models can be changed while preserving conversation context. Large/small model pair for main inference vs. title generation (`agent.go:generateTitle()`).
- **Catwalk model catalog**: Auto-updates provider/model lists from `charm.land/catwalk`. Includes cost tracking per 1M tokens (in/out/cached). See `config/provider.go:Providers()`.
- **Hyper provider**: Charm's own inference service with credit-based billing, embedded fallback model list. See `agent/hyper/provider.go`.

## Session Management
- **SQLite-backed sessions**: Full CRUD via sqlc-generated queries (`db/`, `session/session.go`). Sessions track title, cost, token usage, todos, summary message ID.
- **Auto-summarization**: When context window nears capacity, automatically summarizes and continues. Thresholds: 20K buffer for >200K context, 20% ratio for smaller. See `agent.go` stop conditions.
- **Message queuing**: If agent is busy, new prompts queue and execute sequentially (`agent.go:messageQueue`).
- **Title generation**: Automatic session titling via small model on first message.

## Tool System
- **20+ built-in tools** in `agent/tools/`: bash, edit, multiedit, view, glob, grep, write, fetch, web_fetch, web_search, search, sourcegraph, todos, diagnostics, references, ls, download, job_kill, job_output, lsp_restart, list_mcp_resources, read_mcp_resource.
- **Sub-agent tool**: `agent_tool.go` — spawns a sub-agent (task agent) in a child session with its own context, propagates cost to parent. Uses `fantasy.NewParallelAgentTool` for parallel execution.
- **Agentic fetch tool**: `agentic_fetch_tool.go` — dedicated fetch agent with separate prompt.
- **Permission system**: All tool executions go through `permission.Service` with approval prompts. YOLO mode skips permission checks (`Permissions.SkipRequests`).

## MCP Integration
- **Full MCP client support**: stdio, SSE, and HTTP transports (`config.go:MCPConfig`, `agent/tools/mcp/init.go`).
- **Dynamic tool discovery**: MCP tools registered as `mcp_{serverName}_{toolName}`. Supports tool disabling per-server.
- **MCP resources**: List and read MCP resources via dedicated tools.
- **MCP instructions**: Server instructions injected into system prompt at runtime.
- **Agent-level MCP filtering**: `AllowedMCP` per agent config restricts which MCP servers/tools are accessible.

## LSP Integration
- **Language Server Protocol**: Auto-detect or manually configure LSPs per file type (`config.go:LSPConfig`).
- **LSP-powered tools**: `diagnostics`, `references`, `lsp_restart` tools leverage LSP for code intelligence.
- **Auto-LSP**: Enabled by default, can be disabled. Root markers and file types configurable.

## Agent Skills
- **SKILL.md standard**: Implements agentskills.io spec (`skills/skills.go`). Parses skill files with name, description, license, compatibility metadata.
- **Skills paths**: Configurable via `Options.SkillsPaths`.

## Configuration
- **crush.json / AGENTS.md**: Project-level config. Supports context paths from multiple conventions: `.cursorrules`, `CLAUDE.md`, `GEMINI.md`, `crush.md`, `AGENTS.md`, etc.
- **Provider config**: API keys via env vars or direct config, OAuth2, custom headers, extra body params, system prompt prefix per provider.
- **Model config**: Per-model temperature, top_p, top_k, max_tokens, reasoning_effort, think mode, frequency/presence penalty, provider_options.

## TUI
- **Rich terminal interface**: Built on Bubble Tea v2 + Lipgloss v2. Features: chat view, diff view (unified/split), completions, sidebar, attachments, animations, image rendering, status bar.
- **Compact mode**: Optional `tui.compact_mode`.
- **Cross-platform**: macOS, Linux, Windows (PowerShell + WSL), Android, FreeBSD, OpenBSD, NetBSD.

## Non-interactive Mode
- **`crush run`**: Execute prompts non-interactively, pipe-friendly.

## Provider-Specific Features
- **Anthropic**: Cache control (ephemeral), interleaved thinking, reasoning signatures.
- **OpenAI**: Responses API, reasoning effort, encrypted reasoning content.
- **Google**: Thinking config with budget, thought signatures.
- **OpenRouter**: Exacto mode for supported models, cost tracking from metadata.
- **Copilot**: GitHub Copilot integration with OAuth device flow.
