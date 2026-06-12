# Integration Plan for New Features in Pando

## Current Project Status

### Pando (github.com/digiogithub/pando)
- **Language**: Go 1.24, fork of OpenCode/Crush
- **TUI**: bubbletea (charmbracelet) + lipgloss
- **Config**: Viper, supports `.pando.json` and `.pando.toml` (global `~/.pando.*` + local `.pando.*`)
- **TUI Pages**: Only 2 — ChatPage, LogsPage
- **Dialogs**: Help, Quit, Session, Command, Model, Theme, Filepicker, Init, Permissions, MultiArguments
- **LLM Providers**: Anthropic, OpenAI, Copilot, Gemini, Groq, OpenRouter, XAI, Azure, Bedrock, VertexAI
- **Tools**: bash, edit, file, glob, grep, fetch, ls, patch, write, view, diagnostics, sourcegraph
- **MCP**: via mcp-go (mark3labs/mcp-go)
- **DB**: SQLite (ncruces/go-sqlite3)
- **CLI**: spf13/cobra
- **Does NOT have**: skills, TUI configuration, subagent orchestrator

### Mesnada (github.com/sevir/mesnada) v4.1.1
- **Language**: Go 1.23.4
- **TUI**: bubbletea (charmbracelet) — standalone TUI with dashboard
- **Config**: YAML/JSON (`~/.mesnada/config.yaml`)
- **HTTP Server**: Gin + stdlib mux, MCP JSON-RPC endpoints, SSE, REST API, embedded WebUI
- **Orchestrator**: Full-featured with spawn, cancel, pause, resume, retry, task dependencies
- **Agent Spawners**: Copilot, Claude, Gemini, OpenCode, Ollama (Claude/OpenCode), Mistral, ACP
- **Store**: File-based JSON store for task persistence
- **Personas**: Markdown-based persona system
- **ACP**: Agent Communication Protocol via coder/acp-go-sdk
- **MCP Tools**: spawn_agent, get_task, list_tasks, wait_task, wait_multiple, cancel_task, pause_task, resume_task, delete_task, get_stats, get_task_output, set_progress, acp_session_control

### VTCode Skills Architecture (reference)
- **3-level loading**: Metadata (~50 tokens), Instructions (<5K tokens), Resources (on-demand)
- **SKILL.md**: YAML frontmatter + Markdown body
- **Discovery**: Multiple paths with precedence (user > project)
- **Types**: Traditional (SKILL.md), CLI Tool (executable), Hybrid
- **Routing**: via metadata (description, when-to-use, when-not-to-use)
- **Invocation**: Implicit (model-driven), Explicit (/skill), Programmatic (CLI)

---

## PHASE 1: TUI Settings Interface

**Goal**: Add a Settings page to Pando's TUI that allows viewing and modifying configuration.

### Tasks:

#### 1.1 — Create PageID and base structure
- Add `SettingsPage` to `internal/tui/page/page.go`
- Create `internal/tui/page/settings.go` with the basic bubbletea model
- Register the new page in `internal/tui/tui.go` (page map + init)
- **Delegable to subagent**: YES

#### 1.2 — Create settings components
- Create `internal/tui/components/settings/` with components:
  - `settings.go` — Main component with navigable sections
  - `section.go` — Generic section with editable fields
  - `field.go` — Form field (text input, toggle, dropdown)
- Implement section navigation with Tab/Shift+Tab
- **Delegable to subagent**: YES

#### 1.3 — Implement configuration sections
- **General Section**: theme, autoCompact, debug, shell
- **Providers Section**: list providers with status (active/disabled), edit API keys
- **Agents/Models Section**: edit model per agent (coder, summarizer, task, title)
- **MCP Servers Section**: list, add, edit, remove MCP servers
- **LSP Section**: list and edit LSP configurations
- **Delegable to subagent**: YES (subdivide by section)

#### 1.4 — Integrate config persistence
- Use existing `updateCfgFile()` to save changes
- Add real-time validation when editing
- Show visual feedback (toast/status) on save
- **Delegable to subagent**: YES

#### 1.5 — Add navigation and keybinding
- Add `ctrl+g` keybinding to open Settings
- Add "Settings" command to the command dialog (ctrl+k)
- **Delegable to subagent**: YES

---

## PHASE 2: Skills System (VTCode-style)

**Goal**: Implement a skills system with 3-level progressive loading following the VTCode architecture.

### Tasks:

#### 2.1 — Create core skills package
- Create `internal/skills/types.go` — Types: SkillMetadata, SkillInstruction, SkillResource, Skill
- Create `internal/skills/parser.go` — SKILL.md parser (YAML frontmatter + markdown)
- Create `internal/skills/manager.go` — SkillManager with LRU cache for instructions
- **Delegable to subagent**: YES

#### 2.2 — Implement skills discovery
- Create `internal/skills/discovery.go` — Filesystem scanner
- Search paths with precedence:
  1. `~/.pando/skills/` (user global)
  2. `.pando/skills/` (project local)
  3. `~/.claude/skills/` (Claude compatibility)
  4. `.claude/skills/` (Claude project compatibility)
- Support recursive discovery
- **Delegable to subagent**: YES

#### 2.3 — Implement 3-level progressive loading
- **Level 1**: Load metadata for all skills at startup (~50 tokens/skill)
- **Level 2**: Load instructions on demand with LRU cache, eviction at 80% context
- **Level 3**: Load resources only when the model explicitly requests them
- Create `internal/skills/context.go` — Context management and eviction
- **Delegable to subagent**: YES

#### 2.4 — Integrate skills into the prompt system
- Modify `internal/llm/prompt/prompt.go` to inject active skills metadata
- Modify `internal/llm/prompt/coder.go` to include activated skills instructions
- Add `Skills` field to configuration (`internal/config/config.go`)
- **Delegable to subagent**: YES

#### 2.5 — Implement skills invocation
- Add `/skills` slash command (list, info, activate, deactivate)
- Integrate into the TUI command dialog
- Automatic routing based on metadata (when-to-use)
- **Delegable to subagent**: YES

#### 2.6 — Add skills management to TUI Settings
- Add "Skills" section in the Settings page (Phase 1)
- Show discovered skills with status, loading level, and controls
- Allow enabling/disabling skills from the TUI
- **Delegable to subagent**: YES

#### 2.7 — CLI Tool Skills Support
- Create `internal/skills/tool_bridge.go` — Bridge for CLI executables as skills
- Register discovered CLI tools in Pando's tool registry
- **Delegable to subagent**: YES

---

## PHASE 3: Mesnada Integration (Subagent Orchestrator)

**Goal**: Integrate the full Mesnada functionality into the Pando binary, including orchestrator, HTTP server, agent spawners, and ACP protocol.

### Tasks:

#### 3.1 — Migrate Mesnada core packages to Pando
- Copy and adapt (change module paths) the following packages:
  - `internal/mesnada/orchestrator/` ← mesnada/internal/orchestrator/
  - `internal/mesnada/agent/` ← mesnada/internal/agent/ (all spawners)
  - `internal/mesnada/store/` ← mesnada/internal/store/
  - `internal/mesnada/persona/` ← mesnada/internal/persona/
  - `internal/mesnada/acp/` ← mesnada/internal/acp/
  - `pkg/mesnada/models/` ← mesnada/pkg/models/
- Adapt imports to `github.com/digiogithub/pando/internal/mesnada/...`
- **Delegable to subagent**: YES (may be mechanical work)

#### 3.2 — Migrate HTTP/MCP server
- Copy and adapt:
  - `internal/mesnada/server/` ← mesnada/internal/server/ (server.go, tools.go, api.go, gin.go, ui.go)
  - `internal/mesnada/mcpconv/` ← mesnada/internal/mcpconv/
- Maintain endpoints: /mcp, /mcp/sse, /health, /api/*, /ui
- **Delegable to subagent**: YES

#### 3.3 — Add Mesnada configuration to Pando's .toml
- Extend `internal/config/config.go` with new sections:
```toml
[mesnada]
enabled = true

[mesnada.server]
host = "127.0.0.1"
port = 9767

[mesnada.orchestrator]
store_path = "~/.pando/mesnada/tasks.json"
log_dir = "~/.pando/mesnada/logs"
max_parallel = 5
default_engine = "copilot"
default_mcp_config = ""
persona_path = "~/.pando/mesnada/personas"

[mesnada.tui]
enabled = true
webui = true

[mesnada.acp]
enabled = false

[mesnada.engines.copilot]
default_model = "gpt-4.1"
[[mesnada.engines.copilot.models]]
id = "gpt-4.1"
description = "GPT-4.1 via GitHub Copilot"
```
- **Delegable to subagent**: YES

#### 3.4 — Integrate HTTP server startup in Pando
- Modify `internal/app/app.go` to initialize the Mesnada orchestrator
- Start HTTP server in a background goroutine when `mesnada.enabled = true`
- Register shutdown in the app lifecycle
- Expose the orchestrator to the rest of Pando's components
- **Delegable to subagent**: YES

#### 3.5 — Create Orchestrator Dashboard TUI Page
- Create `internal/tui/page/orchestrator.go` — New page with task dashboard
- Components:
  - Task list with status, progress, engine, model
  - Detail panel for selected task
  - Actions: spawn, cancel, pause, resume, delete
- Add `ctrl+m` keybinding to access the dashboard
- **Delegable to subagent**: YES

#### 3.6 — Register Mesnada tools in Pando's LLM registry
- Create `internal/llm/tools/mesnada.go` with tools:
  - spawn_agent, get_task, list_tasks, wait_task, cancel_task, etc.
- Register in `internal/llm/tools/tools.go`
- Allow Pando's coder agent to orchestrate subagents directly
- **Delegable to subagent**: YES

#### 3.7 — Add required Go dependencies
- Add to go.mod:
  - `github.com/gin-gonic/gin` (HTTP framework for API/WebUI)
  - `github.com/coder/acp-go-sdk` (ACP protocol)
  - `gopkg.in/yaml.v2` (already has viper indirectly)
- Resolve version conflicts with existing dependencies
- **Delegable to subagent**: YES

#### 3.8 — Integrate embedded WebUI
- Copy `ui/` (embedded web assets) from mesnada
- Adapt embed.go to include in the Pando binary
- **Delegable to subagent**: YES

#### 3.9 — Add Mesnada configuration to TUI Settings (Phase 1)
- "Mesnada/Orchestrator" section in Settings
- Configure: host, port, max_parallel, default_engine, personas
- Manage available engines and models
- Manage ACP agents
- **Delegable to subagent**: YES

---

## Recommended Execution Order

```
PHASE 1 (TUI Config) ──────────────────────────────────────────→
PHASE 2 (Skills)      ─────────────────────────→ (2.6 depends on Phase 1)
PHASE 3 (Mesnada)     ────────────────────────────────────────→ (3.5, 3.9 depend on Phase 1)
```

- **Phase 1 and Phase 2 (2.1-2.5)** can run in parallel
- **Phase 2.6** depends on Phase 1 being complete (skills section in settings)
- **Phase 3.5 and 3.9** depend on Phase 1 (TUI pages and settings)
- **Phase 3.1-3.4** are independent and can start in parallel with Phases 1 and 2

## Subdivision for Mesnada Subagents

Each task marked as "Delegable to subagent: YES" can be assigned to a Mesnada subagent with:
- **Engine**: copilot
- **Model**: gpt-4.1 (or gpt-4.6 if available)
- **WorkDir**: /www/MCP/Pando/pando
- **MCP Config**: Access to search and editing tools

### Suggested parallel task grouping:

**Batch 1 (parallel)**:
- Subagent A: Phase 1.1 + 1.2 (base settings structure)
- Subagent B: Phase 2.1 + 2.2 (core skills + discovery)
- Subagent C: Phase 3.1 (Mesnada package migration)

**Batch 2 (after Batch 1)**:
- Subagent D: Phase 1.3 (config sections)
- Subagent E: Phase 2.3 + 2.4 (progressive loading + prompts)
- Subagent F: Phase 3.2 + 3.3 (HTTP server + config)

**Batch 3 (after Batch 2)**:
- Subagent G: Phase 1.4 + 1.5 (persistence + keybindings)
- Subagent H: Phase 2.5 + 2.6 + 2.7 (invocation + TUI + CLI tools)
- Subagent I: Phase 3.4 + 3.7 (app integration + dependencies)

**Batch 4 (final)**:
- Subagent J: Phase 3.5 (TUI orchestrator dashboard)
- Subagent K: Phase 3.6 (Mesnada LLM tools)
- Subagent L: Phase 3.8 + 3.9 (WebUI + Mesnada settings)
