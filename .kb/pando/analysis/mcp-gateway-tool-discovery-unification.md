---
created_at: 2026-08-17T06:36:20.292182346Z
updated_at: 2026-08-17T06:36:20.292182346Z
tags:
    - analysis
    - mcp
    - tool-discovery
    - architecture
---
# Análisis: Unificación de MCP Gateway y Tool Discovery en una única herramienta

**Fecha**: 2026-08-17
**Estado**: Análisis — pendiente de plan de implementación
**Relacionado**: [[pando-mcp-gateway-implementation]], [[plans/tool-discovery-aliasing-anthropic-gpt-plan]]

---

## Objetivo

Revisar las implementaciones actuales de **MCP Gateway** (`internal/mcpgateway`) y
**Tool Discovery** (`internal/llm/tooldiscovery` + `tool_search`) y analizar su
convergencia en **una única herramienta expuesta al LLM** que descubra de forma
general tanto tools internas como tools de servidores MCP, activable desde **un
único punto de configuración**.

---

## Estado actual: dos sistemas paralelos

### 1. MCP Gateway — `internal/mcpgateway`

| Componente | Rol |
|---|---|
| `gateway.go` `Gateway` | Orquestador: registry SQLite + stats de uso + favoritos + ejecución (`CallTool` → `callMCPTool`) |
| `registry.go` | Catálogo persistente en SQLite (`mcp_tool_registry`); búsqueda por `LIKE` sobre `tool_name`/`description` |
| `stats.go` | Uso por tool → cálculo de favoritos |
| `proxy_tools.go` | Dos tools expuestas al LLM: `mcp_query_catalog` (búsqueda/listado paginado) y `mcp_call_tool` (ejecución) |
| `clientpool.go` | Pool de clientes MCP |

**Cobertura**: solo tools de servidores MCP configurados.
**Activación**: `MCPGateway.Enabled` (config). Cableado en:
- `internal/llm/agent/tools.go:200` — `CoderAgentToolsWithMesnada` usa `GetMcpToolsWithGateway` cuando `gateway != nil` (`mcp-tools.go:287`): expone `mcp_query_catalog` + `mcp_call_tool` + favoritos directos.
- `cmd/mcp_server.go:432` — re-export vía `MCPServer.GatewayExpose.Enabled` (segundo punto de activación implícito: fuerza `MCPGateway.Enabled = true`, línea 245).

### 2. Tool Discovery — `internal/llm/tooldiscovery` + `tool_search`

| Componente | Rol |
|---|---|
| `registry.go` `Registry` | Registro in-memory de **todas** las `BaseTool` (core, internal, MCP, RAG, lua); alias, `MarkDiscovered` |
| `search.go` | Búsqueda léxica ponderada (name/alias/server/description/params) |
| `policy.go` | `SelectionPolicy` (modos `auto`/`always`/`off`, umbral `MaxDirectTools=64`), `BuildRegistry`, clasificación por fuente |
| `adapter.go` | Adapta `Registry` a `tools.ToolSearchProvider` |
| `internal/llm/tools/tool_search.go` | Tool `tool_search` (solo búsqueda, sin ejecución) |
| `internal/llm/agent/tool_discovery.go` | `ApplyToolDiscovery`: punto único de cableado, invocado al final de `CoderAgentTools` y `CoderAgentToolsWithMesnada` |

**Cobertura**: todas las tools visibles en el ensamblado (incluye MCP solo si están presentes como `BaseTool`).
**Activación**: `ToolDiscovery.Enabled` (default `true`, modo `auto`).

---

## Problemas detectados

### P1 — Duplicación funcional visible para el LLM
El LLM ve dos herramientas de descubrimiento con backends distintos:
- `mcp_query_catalog` → SQLite `LIKE`, solo MCP.
- `tool_search` → léxico in-memory, todas las tools.

Misma intención, distinto comportamiento y distinto formato de respuesta. El
modelo debe decidir cuál usar; el prompt actual documenta ambas.

### P2 — Los tools MCP "diferidos" son invisibles para `tool_search`
En modo gateway, `CoderAgentToolsWithMesnada` solo inyecta `mcp_query_catalog`,
`mcp_call_tool` y los favoritos como `BaseTool`. El catálogo completo (cientos
de tools) queda en SQLite y **nunca entra en `tooldiscovery.Registry`**.
Consecuencia: `tool_search` no puede encontrar tools MCP no favoritas; el flujo
de descubrimiento queda roto/partido en dos.

### P3 — Dos puntos de activación independientes
`MCPGateway.Enabled` y `ToolDiscovery.Enabled` son flags separados con
interacciones no definidas (p. ej. gateway activo + discovery `off`, o
viceversa). Además `cmd/mcp_server.go` activa el gateway por la vía
`GatewayExpose`, un tercer disparador implícito.

### P4 — Estado de descubrimiento efímero
`ApplyToolDiscovery` construye un `Registry` **nuevo en cada llamada** (cada
ensamblado de tools por turno/agente). `MarkDiscovered` no persiste entre
llamadas: un tool descubierta por `tool_search` puede volver a diferirse en el
siguiente ensamblado. No hay llave por sesión.

### P5 — Ejecución dividida
- MCP diferidas → solo vía `mcp_call_tool`.
- Internas diferidas → se hacen visibles y se invocan directamente (si el
  proveedor recibe el set actualizado en el siguiente turno).
Dos rutas de ejecución, permisos y logging distintos.

### P6 — Interacción favoritos ↔ política de diferimiento
Los favoritos se exponen como `BaseTool` directas y la `SelectionPolicy` puede
diferirlos si no están en `NonDeferredTools`; el mecanismo de favoritos del
gateway y el umbral de discovery pueden contradecirse.

### P7 — Búsquedas de distinta calidad
`LIKE` (SQLite) vs. scoring léxico ponderado (tooldiscovery). Ninguno usa FTS5
ni embeddings (previsto como fase 5 del plan original, no implementado).

---

## Diseño objetivo (propuesta)

### Una única herramienta de descubrimiento
Unificar en **una sola tool** orientada al modelo (sugerencia: conservar el
nombre `tool_search`, ya presente en prompts/tests) con capacidades:

1. **Search/query**: búsqueda semántica/léxica sobre el catálogo unificado
   (internas + MCP) con paginación — absorbe `mcp_query_catalog`.
2. **Ejecución**: o bien la tool única añade un modo `call`, o se mantiene
   `mcp_call_tool` como ejecutor universal que resuelve cualquier tool
   registrada (MCP vía gateway, internas vía invocación directa). Decisión
   abierta: ¿1 tool con verbo search/call o 2 tools con un solo backend?
   El requisito del usuario apunta a **una única herramienta**.

### Registro unificado
- `tooldiscovery.Registry` se convierte en el índice de **todo**: tools
  internas (core, RAG, browser, lua, mesnada…) + catálogo completo del gateway
  (SQLite) sincronizado al `Registry` en `Initialize`/`RefreshServer`.
- El `Registry` deja de reconstruirse por turno: pasa a ser un servicio con
  ciclo de vida de `App` (como `Gateway`), con estado de descubrimiento por
  sesión.
- El gateway aporta persistencia + estadísticas + ejecución MCP; tooldiscovery
  aporta índice general + política de visibilidad. Uno consume al otro, no dos
  rutas paralelas.

### Punto único de activación
Una sola sección de configuración, p. ej.:

```toml
[ToolDiscovery]
Enabled = true
Mode = "auto"          # auto | always | off
MaxDirectTools = 64
SearchLimit = 8
Favorites = true       # promocionar favoritas a directas
```

- `MCPGateway.Enabled` deja de ser un flag de usuario: el gateway se activa
  internamente cuando hay servidores MCP configurados y discovery está activo.
- `MCPServer.GatewayExpose` se mantiene solo para el modo `pando mcp-server`
  (re-export), derivando del mismo estado.

### Flujo resultante

```
Arranque: App → Gateway.Initialize (catálogo MCP a SQLite)
                → Registry unificado (internas + catálogo)
                → SelectionPolicy → tools visibles + tool_search
Turno:    LLM → tool_search(query) → Registry.Search → MarkDiscovered(sess)
          LLM → tool_search(call)  → Gateway.CallTool (MCP) | dispatch interno
```

---

## Decisiones abiertas

1. **Una tool o dos**: `tool_search` con verbo `action: search|call` vs.
   mantener `mcp_call_tool` renombrada a `tool_call` universal. Requisito del
   usuario: una única herramienta → opción A favorita.
2. **Ejecución de tools internas diferidas**: dispatch directo desde la tool
   única (necesita acceso al set completo) vs. hacerlas visibles al turno
   siguiente (comportamiento actual).
3. **Persistencia del estado descubierto**: por sesión (DB de sesiones) vs.
   in-memory con TTL en el servicio.
4. **Migración de configuración**: compatibilidad hacia atrás con
   `MCPGateway.Enabled` existente en configs de usuario (alias/deprecación).
5. **FTS5**: aprovechar para unificar la búsqueda (fase 5 del plan original).

## Impacto estimado

- Paquetes: `internal/mcpgateway` (proxy_tools → deprecar/absorber),
  `internal/llm/tooldiscovery` (registro persistente + sync gateway),
  `internal/llm/tools/tool_search.go` (capacidad call),
  `internal/llm/agent/tools.go` + `tool_discovery.go` (cableado único),
  `internal/config/config.go` (unificación de flags),
  `cmd/mcp_server.go` (adaptar re-export),
  prompts/templates que mencionan `mcp_query_catalog`.
- Tests existentes afectados: `internal/mcpgateway/*_test.go`,
  `internal/llm/tooldiscovery/*_test.go`, `tests/test_settings_config.py`
  (`mcp_gateway` section).

## Verificación

Análisis estático basado en: `kb_get_document` de
`pando-mcp-gateway-implementation.md` y
`plans/tool-discovery-aliasing-anthropic-gpt-plan.md`, más lectura de
`internal/mcpgateway/{gateway,registry,proxy_tools}.go`,
`internal/llm/tooldiscovery/{registry,search,policy,adapter}.go`,
`internal/llm/tools/tool_search.go`, `internal/llm/agent/{tools,tool_discovery,mcp-tools}.go`,
`internal/config/config.go` y `cmd/mcp_server.go`.
