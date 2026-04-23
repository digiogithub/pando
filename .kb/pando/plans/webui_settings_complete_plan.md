# Plan: Web-UI Settings Completeness + Hot-Reload Config
**Fecha:** 2026-03-27  
**Estado:** Planificado

## Contexto

La Web-UI de Pando actualmente solo tiene implementada la sección "General" en Settings.  
La TUI tiene 16+ secciones organizadas en 6 grupos. Este plan cubre la paridad completa y añade hot-reload de configuración para TUI y Web-UI.

### Estado actual Web-UI Settings
- ✅ General (theme, working dir, language, auto_save, markdown_preview, custom_instructions)
- ⏳ Providers — stub "Coming Soon"
- ⏳ Tools — stub "Coming Soon"
- ⏳ Prompts — stub "Coming Soon"
- ⏳ Models — stub "Coming Soon"
- ⏳ Plugins — stub "Coming Soon"
- ⏳ RAG — stub "Coming Soon"

### Grupos TUI que necesitan implementación en Web-UI
- **Core**: General (parcialmente hecho)
- **AI**: Providers, Agents, Persona Auto Select, Evaluator
- **Extensions**: Skills, Skills Catalog, Lua Engine
- **Integrations**: MCP Servers, MCP Gateway, LSP
- **Tools**: Internal Tools (fetch/search/browser), Bash restrictions
- **Services**: Mesnada, Remembrances, API Server, Snapshots

---

## Fases

### Phase 1: Backend API - Config Endpoints Completos
**Fact ID:** `webui_settings_phase1_backend_api`

- Crear `internal/api/handlers/config.go` con handlers para todos los grupos
- Registrar rutas en `internal/api/routes.go`
- Endpoints: `/api/v1/config/providers`, `/api/v1/config/agents`, `/api/v1/config/mcp-servers`, `/api/v1/config/mcp-gateway`, `/api/v1/config/lsp`, `/api/v1/config/tools`, `/api/v1/config/bash`, `/api/v1/config/extensions`, `/api/v1/config/services`, `/api/v1/config/evaluator`
- Cada GET devuelve la sección del config; cada PUT llama a las funciones `config.UpdateXYZ()` existentes
- API keys enmascaradas en GET (solo últimos 4 chars)
- **Prerrequisito de todas las fases web-ui (Fases 3-7)**

---

### Phase 2: Hot-Reload de Configuración (TUI + Web-UI)
**Fact ID:** `webui_settings_phase2_hot_reload`

El objetivo es que cualquier cambio en la config se propague en tiempo real sin reiniciar la app.

#### Subcomponentes:
- **2a. Config File Watcher** (`internal/config/watcher.go`): fsnotify + debounce 200ms → `config.Reload()` → EventBus
- **2b. Config Event Bus** (`internal/config/eventbus.go`): pub/sub con `Subscribe/Unsubscribe/Publish`, singleton `config.Bus`
- **2c. TUI Hot-Reload**: suscripción al bus en `settings.go`, recarga viewport al recibir eventos
- **2d. SSE Endpoint** (`GET /api/v1/config/events`): stream de `ConfigChangeEvent` como JSON
- **2e. Zustand SSE Client** (`web-ui/src/stores/configEventsStore.ts`): `EventSource` → actualiza settingsStore, reconexión con backoff

**Nota anti-loop:** los cambios guardados desde la propia app se marcan con origen para no generar bucle en el receptor.

---

### Phase 3: Web-UI - Providers & Agents
**Fact ID:** `webui_settings_phase3_providers_agents`

- `ProvidersSettings.tsx`: API keys (Anthropic, OpenAI, Ollama, Gemini, GROQ, OpenRouter, XAI, Copilot), base URL, enabled toggle. Componente genérico `ProviderCard`.
- `AgentsSettings.tsx`: por agente (Coder, Summarizer, Task, Title, CLIAssist, PersonaSelector) — model select, maxTokens, timeout, system prompt textarea, temperature slider
- Persona Auto Select: enabled toggle + personaPath
- Dirty tracking por sección en store

---

### Phase 4: Web-UI - MCP Servers, Gateway & LSP
**Fact ID:** `webui_settings_phase4_mcp_lsp`

- `MCPServersSettings.tsx`: tabla de servers, modal add/edit (name, command, args array, env vars, enabled), confirm delete, botón "Reload server"
- `MCPGatewaySettings.tsx`: enabled, favorites list
- `LSPSettings.tsx`: por lenguaje (command, args, enabled), botón "Test connection"
- Componentes reutilizables: `KeyValueEditor`, `ServerStatusBadge`, `ConfirmDeleteDialog`
- Nuevo endpoint backend: `POST /api/v1/mcp-servers/{name}/reload`

---

### Phase 5: Web-UI - Internal Tools & Bash
**Fact ID:** `webui_settings_phase5_tools_bash`

- `InternalToolsSettings.tsx`: por tool (Fetch, Google, Brave, Perplexity, Exa, Context7, Browser) con enabled toggle y campos específicos (API keys, limits, URLs). Componente `ToolCard`.
- `BashSettings.tsx`: bannedCommands y allowedCommands como listas editables con chips/tags. Advertencia en comandos críticos.

---

### Phase 6: Web-UI - Extensions (Skills, Lua, Evaluator)
**Fact ID:** `webui_settings_phase6_extensions`

- `SkillsSettings.tsx`: lista de skills instalados (enable/disable/uninstall/update), paths editor
- Skills Catalog: enabled, baseURL, autoUpdate, defaultScope, modal "Browse Catalog" con instalación
- `LuaSettings.tsx`: enabled, scriptPath, timeout, strictMode, hotReload (integra con Fase 2), logFilteredData
- `EvaluatorSettings.tsx`: enabled, model, alpha/beta sliders, UCB factor, patterns list
- Nuevos endpoints backend: `GET /api/v1/skills/catalog`, `POST /api/v1/skills/install`, `DELETE /api/v1/skills/{name}`

---

### Phase 7: Web-UI - Services (Mesnada, Remembrances, Snapshots, API Server)
**Fact ID:** `webui_settings_phase7_services`

- `MesnadaSettings.tsx`: Server (host/port), Orchestrator (storePath, logDir, maxParallel, defaultEngine/Model, personaPath), ACP (enabled, defaultAgent, autoPermission, server config), TUI (enabled, webui)
- `RemembrancesSettings.tsx`: enabled, embedding provider/model, chunk size/overlap, code indexing toggle + projects list
- `SnapshotsSettings.tsx`: enabled, maxCount, maxFileSize, autoCleanup, storagePath, info "Current snapshots: N"
- `APIServerSettings.tsx`: host, port, auth enabled, auth token (masked + regenerate), CORS origins. Banner de advertencia para cambios que requieren reinicio.

---

## Dependencias entre fases

```
Phase 1 (Backend API)
    ├── Phase 3 (Providers & Agents)
    ├── Phase 4 (MCP & LSP)
    ├── Phase 5 (Tools & Bash)
    ├── Phase 6 (Extensions)
    └── Phase 7 (Services)

Phase 2 (Hot-Reload)  ← independiente, puede ir en paralelo con Phase 1
    └── Integra con Fases 3-7 para actualizaciones en tiempo real
```

## Orden de implementación recomendado
1. Phase 1 (base del API)
2. Phase 2 (hot-reload, en paralelo o justo después)
3. Phases 3-7 en orden (o en paralelo con múltiples agentes)

## Componentes web-ui reutilizables a crear
- `MaskedInput` — input con toggle show/hide para secrets
- `TagListEditor` — editor de listas con chips (banned commands, CORS origins, etc.)
- `KeyValueEditor` — editor de pares clave-valor (env vars)
- `ConfirmDialog` — dialog genérico de confirmación destructiva
- `RestartRequiredBanner` — aviso amarillo para cambios que requieren reinicio
- `ToolCard` — card colapsable para herramientas con enabled toggle
