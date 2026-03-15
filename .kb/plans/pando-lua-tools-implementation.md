# Plan de Implementación: Lua-Based Custom Tools para Pando

> **Proyecto**: Pando
> **Fecha**: 2026-03-15
> **Fact IDs en Remembrances**: `lua_tools_fase1_registry`, `lua_tools_fase2_runtime`, `lua_tools_fase3_agent_integration`, `lua_tools_fase4_config_ux`

---

## Resumen Ejecutivo

Extender el sistema de scripting Lua existente en Pando (gopherlua) para permitir que los usuarios definan **custom tools** mediante funciones Lua. Estas tools se integran en el agente como cualquier otra `BaseTool`, siendo invocables por el LLM de forma transparente.

### Naming Convention

En Lua, el carácter `:` no puede usarse en nombres de función directos, pero sí mediante `_G["tools:nombre"]`. Sin embargo, para mayor limpieza y consistencia, se usará una **tabla `pando_tools`** donde cada entrada define una tool:

```lua
pando_tools = {
    ["git-status"] = {
        description = "Get current git status of the repository",
        parameters = {
            verbose = { type = "boolean", description = "Show verbose output", required = false }
        },
        run = function(params)
            -- params.verbose is available here
            local handle = io.popen("git status" .. (params.verbose and " -v" or " -s"))
            local result = handle:read("*a")
            handle:close()
            return result
        end
    },
    ["count-lines"] = {
        description = "Count lines in a file",
        parameters = {
            file_path = { type = "string", description = "Path to file", required = true }
        },
        run = function(params)
            local f = io.open(params.file_path, "r")
            if not f then return nil, "file not found" end
            local count = 0
            for _ in f:lines() do count = count + 1 end
            f:close()
            return tostring(count)
        end
    }
}
```

La tool se expondrá al LLM con el nombre `lua_<tool-name>` (prefijo `lua_` para evitar colisiones con tools internas).

## Estado Actual del Código

### Lua Engine (`internal/luaengine/`)
- **lua.go**: `NewLuaState()` crea estado sandboxed con gopher-lua, preloads: strings, time, regexp, json
- **manager.go**: `FilterManager` ejecuta hooks y filtros, con timeout y strict mode
- **types.go**: `HookType`, `HookContext`, `HookResult`, `FilterType`
- **helpers.go**: Conversión Go↔Lua (tables, maps, etc.)
- Seguridad: sh/os.exec NO cargados intencionalmente

### Tools System (`internal/llm/tools/`)
- **tools.go**: `BaseTool` interface con `Info() ToolInfo` y `Run(ctx, ToolCall) (ToolResponse, error)`
- `ToolInfo`: Name, Description, Parameters (map[string]any), Required []string
- Cada tool es un struct que implementa `BaseTool`

### Agent Integration (`internal/llm/agent/`)
- **tools.go**: `CoderAgentToolsWithMesnada()` construye la lista de tools del agente
- **agent.go**: `streamAndHandleEvents()` itera sobre `a.tools` buscando por nombre y ejecuta `.Run()`

### Config (`internal/config/config.go`)
- `LuaConfig`: Enabled, ScriptPath, Timeout, StrictMode, HotReload, LogFilteredData

## Arquitectura

```
┌─────────────────────────────────────────────────────────┐
│                    Lua Script (.lua)                      │
│  pando_tools = {                                         │
│    ["tool-name"] = { description, parameters, run() }    │
│  }                                                       │
└───────────────────────────┬─────────────────────────────┘
                            │ LoadScript + DiscoverTools
┌───────────────────────────▼─────────────────────────────┐
│         FilterManager (extended)                         │
│  - DiscoverLuaTools() → []LuaToolDef                    │
│  - ExecuteLuaTool(name, params) → (string, error)       │
└───────────────────────────┬─────────────────────────────┘
                            │ wraps as BaseTool
┌───────────────────────────▼─────────────────────────────┐
│         LuaTool (internal/llm/tools/lua_tool.go)        │
│  implements BaseTool interface                           │
│  - Info() → ToolInfo (from Lua definition)              │
│  - Run() → calls FilterManager.ExecuteLuaTool()         │
└───────────────────────────┬─────────────────────────────┘
                            │ appended to agent tools
┌───────────────────────────▼─────────────────────────────┐
│         Agent (internal/llm/agent/tools.go)             │
│  CoderAgentToolsWithMesnada() adds Lua tools            │
└─────────────────────────────────────────────────────────┘
```

---

## Fase 1: Lua Tool Registry & Discovery (fact: lua_tools_fase1_registry)

**Prioridad**: Crítica (bloquea todas las demás fases)
**Esfuerzo**: Medio
**Archivos**:
- `internal/luaengine/types.go` (añadir tipos)
- `internal/luaengine/manager.go` (añadir descubrimiento)
- `internal/luaengine/lua_tools.go` (nuevo)

### Tareas:

1. **Definir tipos para Lua Tools** en `types.go`:
   ```go
   type LuaToolDef struct {
       Name        string
       Description string
       Parameters  map[string]LuaToolParam
       Required    []string
   }
   
   type LuaToolParam struct {
       Type        string // "string", "boolean", "number", "integer"
       Description string
       Required    bool
       Default     interface{}
   }
   ```

2. **Crear `lua_tools.go`** en luaengine:
   - `DiscoverLuaTools()` — Lee la tabla global `pando_tools` del LState
   - Itera sobre cada entry, extrae description, parameters y valida
   - Retorna `[]LuaToolDef`
   - `ExecuteLuaTool(ctx, name, params map[string]interface{}) (string, error)` — Invoca la función `run` dentro de `pando_tools[name]`, pasa params como Lua table, recoge resultado string

3. **Extender `FilterManager`** en `manager.go`:
   - Añadir campo `luaTools []LuaToolDef`
   - Tras `LoadScript()` y `ReloadScript()`, llamar `DiscoverLuaTools()` automáticamente
   - `GetLuaTools() []LuaToolDef` — getter público
   - `ExecuteLuaTool(ctx, name, params) (string, error)` — delega a lua_tools.go con timeout/mutex

### Seguridad:
- Las Lua tools heredan el sandbox existente (no sh/os.exec)
- Se podría hacer configurable `io.popen` para tools que necesiten shell (opt-in en config)
- Timeout del FilterManager aplica a la ejecución de cada tool

---

## Fase 2: LuaTool BaseTool Wrapper (fact: lua_tools_fase2_runtime)

**Prioridad**: Alta
**Esfuerzo**: Bajo
**Depende de**: Fase 1
**Archivos**:
- `internal/llm/tools/lua_tool.go` (nuevo)

### Tareas:

1. **Crear `lua_tool.go`** — struct que implementa `BaseTool`:
   ```go
   type LuaTool struct {
       def        *luaengine.LuaToolDef
       filterMgr  *luaengine.FilterManager
   }
   
   func NewLuaTool(def *luaengine.LuaToolDef, fm *luaengine.FilterManager) *LuaTool
   
   func (t *LuaTool) Info() ToolInfo {
       // Convert LuaToolDef.Parameters → map[string]any (JSON Schema format)
       // Name = "lua_" + def.Name
       // Description = def.Description
   }
   
   func (t *LuaTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
       // Parse call.Input JSON → map[string]interface{}
       // Call filterMgr.ExecuteLuaTool(ctx, t.def.Name, params)
       // Return NewTextResponse(result) or NewTextErrorResponse(err)
   }
   ```

2. **Función factory**:
   ```go
   func NewLuaToolsFromManager(fm *luaengine.FilterManager) []BaseTool {
       defs := fm.GetLuaTools()
       tools := make([]BaseTool, len(defs))
       for i, def := range defs {
           tools[i] = NewLuaTool(&def, fm)
       }
       return tools
   }
   ```

### Validación de parámetros:
- Validar tipos antes de pasar a Lua (string, number, boolean, integer)
- Verificar required params presentes
- Reportar errores claros al LLM

---

## Fase 3: Agent Integration (fact: lua_tools_fase3_agent_integration)

**Prioridad**: Alta
**Esfuerzo**: Bajo
**Depende de**: Fase 2
**Archivos**:
- `internal/llm/agent/tools.go` (modificar)
- `internal/llm/agent/agent.go` (menor ajuste)

### Tareas:

1. **Modificar `CoderAgentToolsWithMesnada()`** en tools.go:
   - Añadir parámetro `luaMgr *luaengine.FilterManager`
   - Si `luaMgr != nil && luaMgr.IsEnabled()`:
     ```go
     luaTools := tools.NewLuaToolsFromManager(luaMgr)
     baseTools = append(baseTools, luaTools...)
     ```
   - También añadir a `CoderAgentTools()` si se usa sin mesnada

2. **Propagar el FilterManager** desde donde se construye el agent:
   - El agent ya tiene `luaMgr` field y `SetLuaManager()`
   - Necesitamos que las Lua tools estén disponibles ANTES de crear el agent
   - Mover la discovery al punto donde se crea la lista de tools (app initialization)

3. **Hot Reload de tools**:
   - Si `HotReload` está activado, al recargar el script re-descubrir tools
   - Problema: el agent ya tiene la lista de tools fija — solución: el agent usa referencia al FilterManager y resuelve Lua tools dinámicamente (o se recrean al reload)
   - Opción pragmática: las Lua tools se resuelven al inicio de cada `streamAndHandleEvents`, no solo al startup

### Flujo de ejecución:
```
LLM solicita tool "lua_git-status" 
  → agent.streamAndHandleEvents() busca en a.tools
  → encuentra LuaTool{name: "lua_git-status"}
  → LuaTool.Run() → FilterManager.ExecuteLuaTool("git-status", params)
  → Lua VM ejecuta pando_tools["git-status"].run(params_table)
  → Resultado string → ToolResponse → al LLM
```

---

## Fase 4: Configuración & UX (fact: lua_tools_fase4_config_ux)

**Prioridad**: Media
**Esfuerzo**: Bajo
**Depende de**: Fase 3
**Archivos**:
- `internal/config/config.go` (ajustes menores)
- `examples/lua-hooks-example.lua` (actualizar)
- `examples/lua-tools-example.lua` (nuevo)

### Tareas:

1. **Configuración extendida** en `LuaConfig`:
   ```go
   type LuaConfig struct {
       // ... campos existentes ...
       ToolsEnabled    bool     `json:"tools_enabled,omitempty" toml:"ToolsEnabled"`
       AllowedModules  []string `json:"allowed_modules,omitempty" toml:"AllowedModules"` // e.g. ["io"]
   }
   ```
   - `ToolsEnabled`: permite desactivar tools sin desactivar hooks/filtros
   - `AllowedModules`: módulos extra a cargar (e.g., `io` para `io.popen`)

2. **Actualizar ejemplo Lua** con section de tools
3. **Crear ejemplo dedicado** `lua-tools-example.lua` con 2-3 tools útiles
4. **Logging**: Log cuando se descubren Lua tools al inicio
5. **TUI info**: Mostrar Lua tools en el panel de información del agente

### Config TOML ejemplo:
```toml
[lua]
enabled = true
script_path = "/home/user/.config/pando/hooks.lua"
timeout = "10s"
tools_enabled = true
allowed_modules = ["io"]
```

---

## Consideraciones de Seguridad

1. **Sandbox**: Por defecto, `io` y `os` NO están cargados. Las tools operan en un entorno restringido.
2. **Opt-in shell**: Solo si `allowed_modules` incluye "io", se precarga `io` (necesario para `io.popen`)
3. **Timeout**: Cada ejecución de tool tiene timeout configurable
4. **Strict mode**: En strict mode, un error en una Lua tool devuelve error al LLM; en non-strict, devuelve error message como texto
5. **Sin inyección**: Los params se pasan como Lua table tipada, no como string interpolation

## Dependencias

- No se necesitan nuevas dependencias Go
- Usa los mismos paquetes gopher-lua existentes (github.com/yuin/gopher-lua, gopher-json, gopher-lua-libs)

## Testing

- Unit tests en `internal/luaengine/lua_tools_test.go`: discovery, ejecución, timeout, errores
- Unit tests en `internal/llm/tools/lua_tool_test.go`: wrapper BaseTool
- Integration test: script Lua con tools + agent que las invoca
