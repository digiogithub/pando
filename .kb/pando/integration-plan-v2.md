# Pando Integration Plan v2 — 4 Phases, 25 Tasks

## Project Info
- **Go Module**: `github.com/digiogithub/pando`
- **Go version**: 1.24.0
- **Working dir**: /www/MCP/Pando/pando
- **Mesnada source**: /www/MCP/mesnada (github.com/sevir/mesnada)

## Task Index (fact IDs)

| ID | Phase | Task | Dependencies | Fact Key |
|----|------|-------|-------------|----------|
| T1.1 | 1 | Create PageID Settings and base structure | — | task-1.1 |
| T1.2 | 1 | Create settings components | T1.1 | task-1.2 |
| T1.3 | 1 | Implement configuration sections | T1.2 | task-1.3 |
| T1.4 | 1 | Integrate config persistence | T1.3 | task-1.4 |
| T1.5 | 1 | Add navigation and keybindings | T1.1 | task-1.5 |
| T2.1 | 2 | Create core skills package | — | task-2.1 |
| T2.2 | 2 | Implement skills discovery | T2.1 | task-2.2 |
| T2.3 | 2 | Implement 3-level progressive loading | T2.1 | task-2.3 |
| T2.4 | 2 | Integrate skills into prompt system | T2.1, T2.3 | task-2.4 |
| T2.5 | 2 | Implement skills invocation | T2.1, T2.4 | task-2.5 |
| T2.6 | 2 | Add skills management to TUI Settings | T2.5, T1.3 | task-2.6 |
| T2.7 | 2 | CLI Tool Skills support | T2.1 | task-2.7 |
| T3.1 | 3 | Migrate core Mesnada packages | — | task-3.1 |
| T3.2 | 3 | Migrate HTTP/MCP server | T3.1 | task-3.2 |
| T3.3 | 3 | Add Mesnada config to .toml | T3.1 | task-3.3 |
| T3.4 | 3 | Integrate HTTP server startup in app | T3.1, T3.2, T3.3 | task-3.4 |
| T3.5 | 3 | Create TUI Orchestrator Dashboard page | T3.4, T1.1 | task-3.5 |
| T3.6 | 3 | Register Mesnada tools in LLM registry | T3.1, T3.4 | task-3.6 |
| T3.7 | 3 | Add necessary Go dependencies | T3.1 | task-3.7 |
| T3.8 | 3 | Integrate embedded WebUI | T3.2 | task-3.8 |
| T3.9 | 3 | Add Mesnada config to TUI Settings | T3.4, T1.3 | task-3.9 |
| T4.1 | 4 | Add --yolo/--allow-all-tools flag | — | task-4.1 |
| T4.2 | 4 | Add stdin support for prompt | — | task-4.2 |
| T4.3 | 4 | Add PANDO_PROMPT env var support | — | task-4.3 |
| T4.4 | 4 | Global auto-approve for MCP tools | T4.1 | task-4.4 |

---

## PHASE 1: TUI Configuration Interface

### T1.1 — Create PageID Settings and base structure
- Add `SettingsPage PageID = "settings"` to `internal/tui/page/page.go`
- Create `internal/tui/page/settings.go` with bubbletea model implementing `tea.Model`
- Register the new page in `internal/tui/tui.go` in the pages map
- Initialize the page in the `New()` function

### T1.2 — Create settings components
- Create directory `internal/tui/components/settings/`
- `settings.go` — Main component with navigable section list (sidebar + content)
- `section.go` — Generic section with field list
- `field.go` — Field types: TextInput, Toggle (bool), Select (dropdown)
- Implement Tab/Shift+Tab navigation between sections, Up/Down between fields, Enter to edit

### T1.3 — Implement configuration sections
- **General**: theme (select), autoCompact (toggle), debug (toggle), shell.path (text), shell.args (text)
- **Providers**: list of providers with active/disabled status, edit API keys (text with mask)
- **Agents/Models**: model selector per agent (coder, summarizer, task, title)
- **MCP Servers**: CRUD table for MCP servers (name, command, args, type, url)
- **LSP**: list of LSP configurations (language, command, args, enabled)

### T1.4 — Integrate config persistence
- Use existing `config.updateCfgFile()` to save changes on Save press
- Real-time validation: verify non-empty API keys, valid models
- Visual feedback via `util.ReportInfo/ReportError` in the status bar
- Config hot-reload after saving (re-read viper)

### T1.5 — Add navigation and keybindings
- Add `Settings key.Binding` with `ctrl+g` to the keyMap in tui.go
- Register "Settings" command in the command dialog (ctrl+k) with handler
- Handle Settings→Chat navigation with Esc

---

## PHASE 2: Skills System (VTCode-style)

### T2.1 — Create core skills package
- Create `internal/skills/` package
- `types.go`: SkillMetadata (name, description, version, author, when-to-use, when-not-to-use, allowed-tools, user-invocable), SkillInstruction (markdown body), SkillResource (path, content), Skill (metadata + state)
- `parser.go`: SKILL.md parser — extract YAML frontmatter between `---`, body as markdown
- `manager.go`: SkillManager with: LoadAll(), GetMetadata(), GetInstructions(), EvictLRU(), Recall()
- LRU cache for instructions with configurable capacity

### T2.2 — Implement skills discovery
- `discovery.go`: Filesystem scanner with precedence paths:
  1. `~/.pando/skills/` (user global)
  2. `.pando/skills/` (project local)
  3. `~/.claude/skills/` (compatibility)
  4. `.claude/skills/` (project compatibility)
- Search for `**/SKILL.md` recursively in each path
- Skills from higher paths override same-named ones from lower paths
- Return list ordered by precedence

### T2.3 — Implement 3-level progressive loading
- `context.go`: ContextManager with used token tracking
- Level 1: metadata always loaded (~50 tokens/skill) — in `SkillManager.LoadAll()`
- Level 2: instructions on demand — `SkillManager.GetInstructions(name)` with LRU cache
- Level 3: resources only when requested — `SkillManager.GetResource(name, path)`
- Eviction at 80% of configurable context window
- `EstimateTokens(text)` helper to estimate tokens (~4 chars/token)

### T2.4 — Integrate skills into prompt system
- Modify `internal/llm/prompt/prompt.go` to accept `SkillManager`
- Inject active skills metadata into system prompt (Level 1)
- Inject activated skill instructions into user context (Level 2)
- Add `Skills SkillsConfig` field to `config.Config` with `paths []string`, `enabled bool`
- Add `[skills]` section to .toml schema

### T2.5 — Implement skills invocation
- Create slash command handler for `/skills` in `internal/tui/components/dialog/commands.go`
- Subcommands: `/skills list`, `/skills info <name>`, `/skills activate <name>`, `/skills deactivate <name>`
- Automatic routing: in `agent.go`, before sending prompt, evaluate `when-to-use` metadata against prompt
- Automatically activate matching skills

### T2.6 — Add skills management to TUI Settings
- New "Skills" section in the Settings page (depends on T1.3)
- Show discovered skills: name, version, status (loaded/unloaded), loading level
- Toggle to activate/deactivate skills
- Preview of metadata/instructions

### T2.7 — CLI Tool Skills support
- `tool_bridge.go`: Detect directories with `tool` executable + `README.md`
- Create BaseTool wrapper that executes the CLI tool
- Register discovered CLI tools in the tool registry via `CoderAgentTools()`
- Read `schema.json` if it exists to validate arguments

---

## PHASE 3: Mesnada Integration (Subagent Orchestrator)

### T3.1 — Migrate core Mesnada packages
- Create `internal/mesnada/` structure in Pando
- Copy and adapt (rewrite imports to `github.com/digiogithub/pando/internal/mesnada/...`):
  - `internal/mesnada/orchestrator/` ← /www/MCP/mesnada/internal/orchestrator/
  - `internal/mesnada/agent/` ← /www/MCP/mesnada/internal/agent/ (all spawners)
  - `internal/mesnada/store/` ← /www/MCP/mesnada/internal/store/
  - `internal/mesnada/persona/` ← /www/MCP/mesnada/internal/persona/
  - `internal/mesnada/acp/` ← /www/MCP/mesnada/internal/acp/
  - `internal/mesnada/mcpconv/` ← /www/MCP/mesnada/internal/mcpconv/
  - `pkg/mesnada/models/` ← /www/MCP/mesnada/pkg/models/
- Adapt internal config imports

### T3.2 — Migrate HTTP/MCP server
- Copy and adapt:
  - `internal/mesnada/server/` ← /www/MCP/mesnada/internal/server/ (server.go, tools.go, api.go, gin.go, ui.go)
- Maintain endpoints: /mcp, /mcp/sse, /health, /api/*, /ui
- Adapt config and orchestrator imports to new path

### T3.3 — Add Mesnada config to .toml
- Extend `internal/config/config.go` with structs:
  - `MesnadaConfig` with fields: Enabled, Server (host,port), Orchestrator (store_path, log_dir, max_parallel, default_engine, default_mcp_config, persona_path), TUI (enabled, webui), ACP (enabled, default_agent, agents map), Engines map
- Add `Mesnada MesnadaConfig` field to `Config` struct
- Register defaults in viper: `mesnada.enabled=false`, `mesnada.server.host=127.0.0.1`, `mesnada.server.port=9767`
- Update `pando-schema.json` with new properties

### T3.4 — Integrate HTTP server startup in app
- Modify `internal/app/app.go`:
  - Add `Orchestrator` and `MesnadaServer` fields to App struct
  - In `New()`: if `config.Get().Mesnada.Enabled`, create orchestrator and server
  - Start HTTP server in background goroutine
  - Register shutdown in `App.Shutdown()`
- Expose orchestrator for use by other components

### T3.5 — Create TUI Orchestrator Dashboard page
- Create `internal/tui/page/orchestrator.go` — new page PageID "orchestrator"
- Layout: task table (ID, status, engine, model, progress) + detail panel
- Actions with keybindings: (s)pawn, (c)ancel, (p)ause, (r)esume, (d)elete, (l)og
- Input dialog for spawn (prompt, engine, model, work_dir)
- Auto-refresh every 2s for running tasks

### T3.6 — Register Mesnada tools in LLM registry
- Create `internal/llm/tools/mesnada.go` with tools adapted as BaseTool:
  - `mesnada_spawn_agent`, `mesnada_get_task`, `mesnada_list_tasks`, `mesnada_wait_task`, `mesnada_cancel_task`, `mesnada_get_task_output`, `mesnada_set_progress`
- Each tool calls the orchestrator directly (not via HTTP)
- Register in `internal/llm/agent/tools.go` inside `CoderAgentTools()`
- Only register if mesnada.enabled=true in config

### T3.7 — Add necessary Go dependencies
- `go get github.com/gin-gonic/gin`
- `go get github.com/coder/acp-go-sdk`
- `go get gopkg.in/yaml.v2`
- Resolve version conflicts with existing dependencies
- Run `go mod tidy`

### T3.8 — Integrate embedded WebUI
- Copy `ui/` directory from /www/MCP/mesnada/ui/ to pando
- Create `internal/mesnada/ui/embed.go` with `//go:embed`
- Connect to the HTTP server to serve static assets at /ui

### T3.9 — Add Mesnada config to TUI Settings
- New "Mesnada" section in the Settings page
- Fields: enabled (toggle), host (text), port (number), max_parallel (number), default_engine (select), persona_path (text)
- ACP subsection: enabled (toggle), default_agent (select), auto_permission (toggle)

---

## PHASE 4: Enhanced Non-Interactive Mode

### T4.1 — Add --yolo/--allow-all-tools flag
- In `cmd/root.go init()`: add `--yolo` flag (alias `--allow-all-tools`) of type bool
- Both flags are aliases of the same behavior
- When active: set a global/context variable `AllToolsAutoApproved`
- Pass this flag to the App so it propagates
- In non-interactive mode (`-p`): `--yolo` auto-approves EVERYTHING including MCP tools
- In interactive mode: `--yolo` also auto-approves (useful for scripts with TUI)

### T4.2 — Add stdin support for prompt
- In `cmd/root.go RunE`: if `prompt == ""` and stdin is NOT a terminal (piped), read entire stdin as prompt
- Detect terminal with `os.Stdin.Stat()` checking `ModeCharDevice`
- Read with `io.ReadAll(os.Stdin)` and trim whitespace
- If stdin is empty and there is no `-p`, launch normal interactive mode
- Example: `echo "Explain Go context" | pando` or `cat prompt.txt | pando`

### T4.3 — Add PANDO_PROMPT env var support
- In `cmd/root.go RunE`: if `prompt == ""` (neither flag nor stdin), check `os.Getenv("PANDO_PROMPT")`
- Priority: `-p` flag > piped stdin > `PANDO_PROMPT` env var > interactive mode
- Document in command help text

### T4.4 — Global auto-approve for MCP tools with --yolo
- Modify `internal/permission/permission.go`:
  - Add `globalAutoApprove bool` field to `permissionService`
  - Add `SetGlobalAutoApprove(bool)` method to `Service` interface
  - In `Request()`: if `globalAutoApprove == true`, return `true` immediately (before any check)
- Modify `internal/app/app.go`:
  - In `RunNonInteractive()`: if yolo flag active, call `a.Permissions.SetGlobalAutoApprove(true)`
  - This covers ALL tools including MCP, bash, write, edit, etc.
- Modify `cmd/root.go`: pass yolo flag to `app.RunNonInteractive()` or via config

---

## Dependency Graph

```
T1.1 ──→ T1.2 ──→ T1.3 ──→ T1.4
  └────→ T1.5       └──────→ T2.6
                     └──────→ T3.9

T2.1 ──→ T2.2
  ├────→ T2.3 ──→ T2.4 ──→ T2.5 ──→ T2.6
  └────→ T2.7

T3.1 ──→ T3.2 ──→ T3.8
  ├────→ T3.3
  ├────→ T3.7
  └──(T3.1+T3.2+T3.3)──→ T3.4 ──→ T3.5
                            ├────→ T3.6
                            └────→ T3.9

T4.1 ──→ T4.4
T4.2 (independent)
T4.3 (independent)
```

## Immediate Parallel Tasks (no dependencies)
T1.1, T2.1, T2.7, T3.1, T3.7, T4.1, T4.2, T4.3