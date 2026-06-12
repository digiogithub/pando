# Plan: Settings TUI — Full Config Coverage

**Objective:** Expose all options currently only configurable via TOML/JSON in the TUI configuration panel.

**Date:** 2026-03-18  
**Status:** Pending implementation

---

## Gap analysis (options in config.go NOT exposed in TUI)

### Existing sections that need additional fields

| Section | Missing field | Type |
|---------|---------------|------|
| General | WorkingDir | FieldText |
| General | LogFile | FieldText |
| General | DebugLSP | FieldToggle |
| General | ContextPaths | FieldText (comma-separated) |
| General | Data.Directory | FieldText |
| Agents | MaxTokens (per agent) | FieldText (int64) |
| Agents | ReasoningEffort (per agent) | FieldSelect (low/medium/high) |
| Agents | AutoCompact (per agent) | FieldToggle |
| Agents | AutoCompactThreshold (per agent) | FieldText (float 0–1) |
| Providers | UseOAuth (Anthropic) | FieldToggle |
| MCP Servers | Env (per server) | FieldText (space-sep KEY=VAL) |
| MCP Servers | Headers (per server) | FieldText (Key:Val format) |
| LSP | Args (per language) | FieldText (space-sep) |
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

### New complete sections (not exposed at all)

| New section | Struct in config.go | # fields |
|--------------|--------------------| ---------|
| API Server | APIServerConfig | 4 |
| Lua Engine | LuaConfig | 6 |
| MCP Gateway | MCPGatewayConfig | 5 |
| Snapshots | SnapshotsConfig | 5 |
| Self-Improvement | EvaluatorConfig | 12 |

### Missing Update* functions in config.go

- `UpdateAgent(AgentName, Agent)` — full agent config
- `UpdateGeneral(workingDir, logFile string, debugLSP bool, contextPaths []string, dataDir string)`
- `UpdateServer(APIServerConfig)`
- `UpdateLua(LuaConfig)`
- `UpdateMCPGateway(MCPGatewayConfig)`
- `UpdateSnapshots(SnapshotsConfig)`
- `UpdateEvaluator(EvaluatorConfig)`

---

## Implementation phases

### Phase 1: Config Backend — New Update Functions
**Fact ID:** `settings_tui_phase1_config_backend`  
**File:** `internal/config/config.go`  
Add 7 new `Update*` functions for all uncovered subsystems.

### Phase 2: Extend Existing TUI Sections
**Fact ID:** `settings_tui_phase2_extend_existing_sections`  
**File:** `internal/tui/page/settings.go`  
Add missing fields in General, Agents, Providers, MCP Servers, LSP, and Mesnada.

### Phase 3: New TUI Sections — API Server, Lua, MCP Gateway
**Fact ID:** `settings_tui_phase3_new_sections_server_lua_gateway`  
**File:** `internal/tui/page/settings.go`  
Create `buildServerSection`, `buildLuaSection`, `buildMCPGatewaySection`.

### Phase 4: New TUI Sections — Snapshots & Evaluator
**Fact ID:** `settings_tui_phase4_new_sections_snapshots_evaluator`  
**File:** `internal/tui/page/settings.go`  
Create `buildSnapshotsSection`, `buildEvaluatorSection`.

### Phase 5: Persistence Layer — New Save Functions
**Fact ID:** `settings_tui_phase5_persistence_layer`  
**File:** `internal/tui/page/settings.go`  
Add/extend `saveGeneral`, `saveAgent`, `saveProvider`, `saveMCPServer`, `saveLSP`, `saveMesnada`, `saveServer`, `saveLua`, `saveMCPGateway`, `saveSnapshots`, `saveEvaluator`.

### Phase 6: Integration & Testing
**Fact ID:** `settings_tui_phase6_integration_testing`  
**Files:** `internal/tui/page/settings.go`, `tests/test_settings_config.py`  
Update `buildSections()`, manual verification, Python tests.

---

## Phase dependencies

```
Phase 1 (config.go backend)
    └─> Phase 2 (extend existing sections)
    └─> Phase 3 (new sections: Server, Lua, Gateway)
    └─> Phase 4 (new sections: Snapshots, Evaluator)
         └─> Phase 5 (persistence layer - all save functions)
              └─> Phase 6 (integration + testing)
```

Phases 2, 3, 4 can be developed in parallel once Phase 1 is complete.
Phase 5 depends on Phase 1 Update* functions being available.
