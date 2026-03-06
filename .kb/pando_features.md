# Pando Features & Capabilities

## User-Facing Capabilities

### Interactive TUI
- Built with BubbleTea framework for smooth terminal experience
- Vim-like keybindings: `i` to focus editor, `Esc` to blur, `Ctrl+S` to send
- Session management: `Ctrl+N` new session, `Ctrl+A` switch session
- Model selection dialog: `Ctrl+O` with provider navigation (`h/l` or arrows)
- Command dialog: `Ctrl+K` for custom and built-in commands
- Permission dialog: `a` allow, `A` allow for session, `d` deny
- External editor support: `Ctrl+E` opens `$EDITOR`
- Theme support via `tui.theme` config option
- Log viewer: `Ctrl+L`
- Help dialog: `Ctrl+?` or `?`

### Non-Interactive Mode
- Run with `-p "prompt"` for scripting/automation
- Output formats: `text` (default), `json` (`-f json`)
- Quiet mode: `-q` suppresses spinner
- Auto-approves all permissions in non-interactive sessions

### AI Provider Integrations
- **OpenAI**: GPT-4.1 family, GPT-4.5, GPT-4o, O1/O3/O4 models [internal/llm/provider/openai.go]
- **Anthropic**: Claude 4 Opus/Sonnet, Claude 3.x family [internal/llm/provider/anthropic.go]
- **Google Gemini**: Gemini 2.0/2.5 models [internal/llm/provider/gemini.go]
- **GitHub Copilot**: Experimental, uses copilot tokens [internal/llm/provider/copilot.go]
- **AWS Bedrock**: Claude 3.7 Sonnet [internal/llm/provider/bedrock.go]
- **Azure OpenAI**: GPT/O model families [internal/llm/provider/azure.go]
- **Google VertexAI**: Gemini 2.5 models [internal/llm/provider/vertexai.go]
- **Groq**: Llama 4, QWEN, Deepseek models (configured via env)
- **OpenRouter**: Routing layer to multiple providers (configured via env)
- **Local/Self-hosted**: OpenAI-compatible endpoint via `LOCAL_ENDPOINT`

### Agent System
- **Coder Agent** (`AgentCoder`): Primary agent for coding tasks. Configured via `agents.coder` in config.
- **Task Agent** (`AgentTask`): Sub-agent for delegated tasks via the `agent` tool. Configured via `agents.task`.
- **Summarizer Agent** (`AgentSummarizer`): Auto-compact sessions when approaching context limits (95% threshold).
- **Title Agent** (`AgentTitle`): Generates session titles (maxTokens: 80).
- Agent tools defined in [internal/llm/agent/tools.go] with tool registry pattern.

### Built-in AI Tools
- **File tools**: `glob` (find files), `grep` (search contents with rg), `ls` (list dirs), `view` (read files), `write` (create/overwrite), `edit` (modify files), `patch` (apply diffs)
- **System tools**: `bash` (execute shell commands with configurable shell), `fetch` (HTTP fetching with format control)
- **Code intelligence**: `diagnostics` (LSP diagnostics), `sourcegraph` (public code search)
- **Sub-agent**: `agent` tool for task delegation

### MCP (Model Context Protocol)
- Supports `stdio` and `sse` transport types
- Configured in `.opencode.json` under `mcpServers`
- Auto-discovery of tools from MCP servers with 30s timeout
- Permission model applies to MCP tools same as built-in tools
- MCP tools initialized early in both interactive and non-interactive modes [cmd/root.go:initMCPTools]

### LSP Integration
- Multi-language support via configurable language servers
- Diagnostics exposed to AI via `diagnostics` tool
- File watching for automatic LSP notification on changes [internal/lsp/watcher/]
- Full LSP protocol client (completions, hover, definition) but only diagnostics exposed to AI currently

### Session & History
- SQLite-backed persistent sessions and messages
- File change tracking per session [internal/history/]
- Auto-compact feature: summarizes conversation at 95% context usage
- Manual compact via built-in command

### Custom Commands
- User commands: `$XDG_CONFIG_HOME/opencode/commands/` or `~/.opencode/commands/` (prefix: `user:`)
- Project commands: `<PROJECT_DIR>/.opencode/commands/` (prefix: `project:`)
- Markdown files with `RUN` and `READ` directives
- Named arguments via `$PLACEHOLDER` syntax
- Subdirectory organization (e.g., `git/commit.md` → `user:git:commit`)

### Configuration
- JSON config at `$HOME/.opencode.json`, `$XDG_CONFIG_HOME/opencode/.opencode.json`, `./.opencode.json`
- Per-agent model/maxTokens/reasoningEffort settings
- Per-provider apiKey/disabled toggles
- Shell path/args customization
- Data directory override
- Debug and debugLSP flags
