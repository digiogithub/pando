# Plan: Settings TUI — Full Config Coverage

**Objetivo:** Exponer en el panel de configuración TUI todas las opciones que actualmente sólo son configurables vía TOML/JSON.

**Fecha:** 2026-03-18  
**Estado:** Pendiente de implementación

---

## Análisis de gaps (opciones en config.go NO expuestas en TUI)

### Secciones existentes que necesitan campos adicionales

| Sección | Campo faltante | Tipo |
|---------|---------------|------|
| General | WorkingDir | FieldText |
| General | LogFile | FieldText |
| General | DebugLSP | FieldToggle |
| General | ContextPaths | FieldText (comma-separated) |
| General | Data.Directory | FieldText |
| Agents | MaxTokens (por agente) | FieldText (int64) |
| Agents | ReasoningEffort (por agente) | FieldSelect (low/medium/high) |
| Agents | AutoCompact (por agente) | FieldToggle |
| Agents | AutoCompactThreshold (por agente) | FieldText (float 0–1) |
| Providers | UseOAuth (Anthropic) | FieldToggle |
| MCP Servers | Env (por server) | FieldText (space-sep KEY=VAL) |
| MCP Servers | Headers (por server) | FieldText (Key:Val format) |
| LSP | Args (por language) | FieldText (space-sep) |
| Mesnada | Orchestrator.StorePath | FieldText |
| Mesnada | Orchestrator.LogDir | FieldText |
| Mesnada | Orchestrator.DefaultModel | FieldText |
| Mesnada | Orchestrator.DefaultMCPConfig | FieldText |
| Mesnada | ACP.DefaultAgent | FieldText |
| Mesnada | ACP.Server.Enabled | FieldToggle |
| Mesnada | ACP.Server.Host | FieldText |
| Mesnada | ACP.Server.Port | FieldText (int) |
| Mesnada | ACP.Server.MaxSessions | FieldText (int) |
| Mesnada | ACP.Server.SessionTimeout | FieldText (e.g. "30m") |
| Mesnada | ACP.Server.RequireAuth | FieldToggle |
| Mesnada | TUI.Enabled | FieldToggle |
| Mesnada | TUI.WebUI | FieldToggle |

### Secciones nuevas completas (no expuestas en absoluto)

| Sección nueva | Struct en config.go | # campos |
|--------------|--------------------| ---------|
| API Server | APIServerConfig | 4 |
| Lua Engine | LuaConfig | 6 |
| MCP Gateway | MCPGatewayConfig | 5 |
| Snapshots | SnapshotsConfig | 5 |
| Self-Improvement | EvaluatorConfig | 12 |

### Funciones Update* faltantes en config.go

- `UpdateAgent(AgentName, Agent)` — full agent config
- `UpdateGeneral(workingDir, logFile string, debugLSP bool, contextPaths []string, dataDir string)`
- `UpdateServer(APIServerConfig)`
- `UpdateLua(LuaConfig)`
- `UpdateMCPGateway(MCPGatewayConfig)`
- `UpdateSnapshots(SnapshotsConfig)`
- `UpdateEvaluator(EvaluatorConfig)`

---

## Fases de implementación

### Phase 1: Config Backend — New Update Functions
**Fact ID:** `settings_tui_phase1_config_backend`  
**Archivo:** `internal/config/config.go`  
Añadir 7 nuevas funciones `Update*` para todos los subsistemas no cubiertos.

### Phase 2: Extend Existing TUI Sections
**Fact ID:** `settings_tui_phase2_extend_existing_sections`  
**Archivo:** `internal/tui/page/settings.go`  
Añadir campos faltantes en General, Agents, Providers, MCP Servers, LSP, y Mesnada.

### Phase 3: New TUI Sections — API Server, Lua, MCP Gateway
**Fact ID:** `settings_tui_phase3_new_sections_server_lua_gateway`  
**Archivo:** `internal/tui/page/settings.go`  
Crear `buildServerSection`, `buildLuaSection`, `buildMCPGatewaySection`.

### Phase 4: New TUI Sections — Snapshots & Evaluator
**Fact ID:** `settings_tui_phase4_new_sections_snapshots_evaluator`  
**Archivo:** `internal/tui/page/settings.go`  
Crear `buildSnapshotsSection`, `buildEvaluatorSection`.

### Phase 5: Persistence Layer — New Save Functions
**Fact ID:** `settings_tui_phase5_persistence_layer`  
**Archivo:** `internal/tui/page/settings.go`  
Añadir/extender `saveGeneral`, `saveAgent`, `saveProvider`, `saveMCPServer`, `saveLSP`, `saveMesnada`, `saveServer`, `saveLua`, `saveMCPGateway`, `saveSnapshots`, `saveEvaluator`.

### Phase 6: Integration & Testing
**Fact ID:** `settings_tui_phase6_integration_testing`  
**Archivos:** `internal/tui/page/settings.go`, `tests/test_settings_config.py`  
Actualizar `buildSections()`, verificación manual, tests Python.

---

## Dependencias entre fases

```
Phase 1 (config.go backend)
    └─> Phase 2 (extend existing sections)
    └─> Phase 3 (new sections: Server, Lua, Gateway)
    └─> Phase 4 (new sections: Snapshots, Evaluator)
         └─> Phase 5 (persistence layer - all save functions)
              └─> Phase 6 (integration + testing)
```

Phases 2, 3, 4 pueden desarrollarse en paralelo una vez completada la Phase 1.
Phase 5 depende de que existan las funciones Update* de Phase 1.
