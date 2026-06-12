# Plan: Web-UI Settings Completeness + Hot-Reload Config
**Date:** 2026-03-27  
**Status:** Planned

## Context

Pando's Web-UI currently only has the "General" section implemented in Settings.  
The TUI has 16+ sections organized into 6 groups. This plan covers full parity and adds hot-reload configuration for TUI and Web-UI.

### Current Web-UI Settings Status
- ✅ General (theme, working dir, language, auto_save, markdown_preview, custom_instructions)
- ⏳ Providers — stub "Coming Soon"
- ⏳ Tools — stub "Coming Soon"
- ⏳ Prompts — stub "Coming Soon"
- ⏳ Models — stub "Coming Soon"
- ⏳ Plugins — stub "Coming Soon"
- ⏳ RAG — stub "Coming Soon"

### TUI Groups that need Web-UI implementation
- **Core**: General (partially done)
- **AI**: Providers, Agents, Persona Auto Select, Evaluator
- **Extensions**: Skills, Skills Catalog, Lua Engine
- **Integrations**: MCP Servers, MCP Gateway, LSP
- **Tools**: Internal Tools (fetch/search/browser), Bash restrictions
- **Services**: Mesnada, Remembrances, API Server, Snapshots

---

## Phases

### Phase 1: Backend API - Complete Config Endpoints
**Fact ID:** `webui_settings_phase1_backend_api`

- Create `internal/api/handlers/config.go` with handlers for all groups
- Register routes in `internal/api/routes.go`
- Endpoints: `/api/v1/config/providers`, `/api/v1/config/agents`, `/api/v1/config/mcp-servers`, `/api/v1/config/mcp-gateway`, `/api/v1/config/lsp`, `/api/v1/config/tools`, `/api/v1/config/bash`, `/api/v1/config/extensions`, `/api/v1/config/services`, `/api/v1/config/evaluator`
- Each GET returns the config section; each PUT calls the existing `config.UpdateXYZ()` functions
- API keys masked in GET (last 4 chars only)
- **Prerequisite for all web-ui phases (Phases 3-7)**

---

### Phase 2: Configuration Hot-Reload (TUI + Web-UI)
**Fact ID:** `webui_settings_phase2_hot_reload`

The goal is for any config change to propagate in real-time without restarting the app.

#### Subcomponents:
- **2a. Config File Watcher** (`internal/config/watcher.go`): fsnotify + debounce 200ms → `config.Reload()` → EventBus
- **2b. Config Event Bus** (`internal/config/eventbus.go`): pub/sub with `Subscribe/Unsubscribe/Publish`, singleton `config.Bus`
- **2c. TUI Hot-Reload**: bus subscription in `settings.go`, reload viewport on receiving events
- **2d. SSE Endpoint** (`GET /api/v1/config/events`): stream `ConfigChangeEvent` as JSON
- **2e. Zustand SSE Client** (`web-ui/src/stores/configEventsStore.ts`): `EventSource` → update settingsStore, reconnection with backoff

**Anti-loop note:** changes saved from the app itself are tagged with an origin to prevent loops in the receiver.

---

### Phase 3: Web-UI - Providers & Agents
**Fact ID:** `webui_settings_phase3_providers_agents`

- `ProvidersSettings.tsx`: API keys (Anthropic, OpenAI, Ollama, Gemini, GROQ, OpenRouter, XAI, Copilot), base URL, enabled toggle. Generic `ProviderCard` component.
- `AgentsSettings.tsx`: per agent (Coder, Summarizer, Task, Title, CLIAssist, PersonaSelector) — model select, maxTokens, timeout, system prompt textarea, temperature slider
- Persona Auto Select: enabled toggle + personaPath
- Dirty tracking per section in store

---

### Phase 4: Web-UI - MCP Servers, Gateway & LSP
**Fact ID:** `webui_settings_phase4_mcp_lsp`

- `MCPServersSettings.tsx`: servers table, add/edit modal (name, command, args array, env vars, enabled), confirm delete, "Reload server" button
- `MCPGatewaySettings.tsx`: enabled, favorites list
- `LSPSettings.tsx`: per language (command, args, enabled), "Test connection" button
- Reusable components: `KeyValueEditor`, `ServerStatusBadge`, `ConfirmDeleteDialog`
- New backend endpoint: `POST /api/v1/mcp-servers/{name}/reload`

---

### Phase 5: Web-UI - Internal Tools & Bash
**Fact ID:** `webui_settings_phase5_tools_bash`

- `InternalToolsSettings.tsx`: per tool (Fetch, Google, Brave, Perplexity, Exa, Context7, Browser) with enabled toggle and specific fields (API keys, limits, URLs). `ToolCard` component.
- `BashSettings.tsx`: bannedCommands and allowedCommands as editable lists with chips/tags. Warning on critical commands.

---

### Phase 6: Web-UI - Extensions (Skills, Lua, Evaluator)
**Fact ID:** `webui_settings_phase6_extensions`

- `SkillsSettings.tsx`: installed skills list (enable/disable/uninstall/update), paths editor
- Skills Catalog: enabled, baseURL, autoUpdate, defaultScope, "Browse Catalog" modal with installation
- `LuaSettings.tsx`: enabled, scriptPath, timeout, strictMode, hotReload (integrates with Phase 2), logFilteredData
- `EvaluatorSettings.tsx`: enabled, model, alpha/beta sliders, UCB factor, patterns list
- New backend endpoints: `GET /api/v1/skills/catalog`, `POST /api/v1/skills/install`, `DELETE /api/v1/skills/{name}`

---

### Phase 7: Web-UI - Services (Mesnada, Remembrances, Snapshots, API Server)
**Fact ID:** `webui_settings_phase7_services`

- `MesnadaSettings.tsx`: Server (host/port), Orchestrator (storePath, logDir, maxParallel, defaultEngine/Model, personaPath), ACP (enabled, defaultAgent, autoPermission, server config), TUI (enabled, webui)
- `RemembrancesSettings.tsx`: enabled, embedding provider/model, chunk size/overlap, code indexing toggle + projects list
- `SnapshotsSettings.tsx`: enabled, maxCount, maxFileSize, autoCleanup, storagePath, info "Current snapshots: N"
- `APIServerSettings.tsx`: host, port, auth enabled, auth token (masked + regenerate), CORS origins. Warning banner for changes requiring restart.

---

## Phase Dependencies

```
Phase 1 (Backend API)
    ├── Phase 3 (Providers & Agents)
    ├── Phase 4 (MCP & LSP)
    ├── Phase 5 (Tools & Bash)
    ├── Phase 6 (Extensions)
    └── Phase 7 (Services)

Phase 2 (Hot-Reload)  ← independent, can run in parallel with Phase 1
    └── Integrates with Phases 3-7 for real-time updates
```

## Recommended Implementation Order
1. Phase 1 (API base)
2. Phase 2 (hot-reload, in parallel or right after)
3. Phases 3-7 in order (or in parallel with multiple agents)

## Reusable Web-UI Components to Create
- `MaskedInput` — input with show/hide toggle for secrets
- `TagListEditor` — list editor with chips (banned commands, CORS origins, etc.)
- `KeyValueEditor` — key-value pair editor (env vars)
- `ConfirmDialog` — generic destructive confirmation dialog
- `RestartRequiredBanner` — yellow warning for changes requiring restart
- `ToolCard` — collapsible card for tools with enabled toggle
