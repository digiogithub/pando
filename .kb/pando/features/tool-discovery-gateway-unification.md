---
created_at: 2026-08-17T07:34:19.853215356Z
updated_at: 2026-08-17T07:34:19.853215356Z
tags:
    - feature
    - mcp
    - tool-discovery
    - implementation
---
# Implementación: Unificación de MCP Gateway y Tool Discovery en una única herramienta

**Fecha**: 2026-08-17
**Estado**: Implementado y verificado
**Análisis previo**: [[pando/analysis/mcp-gateway-tool-discovery-unification]]
**Planes base**: [[pando-mcp-gateway-implementation]], [[plans/tool-discovery-aliasing-anthropic-gpt-plan]]

---

## Qué se cambió

Se unificaron los dos mecanismos paralelos de descubrimiento (MCP Gateway con
`mcp_query_catalog`/`mcp_call_tool` y Tool Discovery con `tool_search`) en
**una única herramienta `tool_search`** que descubre y ejecuta de forma general
tanto tools internas como tools de servidores MCP, activable desde **un único
punto** (`ToolDiscovery.Enabled`).

### Cambios por archivo

| Archivo | Cambio |
|---|---|
| `internal/llm/tools/tool_search.go` | `tool_search` ahora es search + call: nuevos parámetros `tool_name` y `parameters`; nueva interfaz `ToolExecutor`; nuevo constructor `NewToolSearchToolWithExecutor`. Sin `tool_name` busca, con `tool_name` ejecuta. |
| `internal/llm/tooldiscovery/registry.go` | `Register` con semántica upsert por nombre canónico; entradas de catálogo sin `BaseTool` (`RegisterCatalogEntry`, `SyncCatalogEntries`); `RemoteToolExecutor` + `SetRemoteExecutor`; `ExecuteTool` (tools locales directas, MCP vía ejecutor remoto); `HasRemoteEntries`; `AllTools`/`Resolve`/`DiscoveredTools` ignoran entradas sin tool. Estado "discovered" persiste al hacer upsert. |
| `internal/llm/tooldiscovery/metadata.go` | `ToolMetadata` gana `Description` y `Parameters` para respaldar entradas de catálogo. |
| `internal/llm/tooldiscovery/search.go` | Búsqueda nil-safe: las entradas de catálogo puntúan y se muestran con la descripción de sus metadatos. |
| `internal/llm/tooldiscovery/policy.go` | `Apply` incluye `tool_search` también por debajo del umbral cuando hay entradas remotas (son inalcanzables de otro modo); `classifySource` → exportada `ClassifySource`. |
| `internal/llm/agent/tool_discovery.go` | Registro compartido process-wide (`SharedDiscoveryRegistry`, sobrevive entre reconstrucciones de tools); `ApplyToolDiscovery(allTools, gateway)` sincroniza tools vivas + catálogo completo del gateway + cablea `makeGatewayExecutor` (`gw.CallTool` con ID `server/tool` y sessionID del contexto) + `syncGatewayCatalog` (nombres `<server>_<toolname>`). |
| `internal/llm/agent/mcp-tools.go` | Nuevo `GetMcpFavoriteTools` (solo favoritas como tools directas); `GetMcpToolsWithGateway` queda como path legacy (ToolDiscovery desactivado / modo mcp-server). |
| `internal/llm/agent/tools.go` | `CoderAgentToolsWithMesnada`: con gateway y `ToolDiscovery.Enabled` → solo favoritas directas (el resto del catálogo vía `tool_search`); sin discovery → path legacy con proxies. |
| `internal/app/app.go` | Punto único de activación: el gateway se inicializa si `MCPGateway.Enabled` **o** `ToolDiscovery.Enabled && len(MCPServers) > 0`. `GatewayExposeEnabled` ya no depende del flag `MCPGateway.Enabled` sino de la misma condición compuesta. |

### Comportamiento resultante

```
Arranque: App → Gateway (auto si ToolDiscovery.Enabled + servidores MCP)
Turno:    CoderAgentToolsWithMesnada
            → favoritas MCP como tools directas
            → ApplyToolDiscovery: registry compartido (internas + catálogo MCP)
            → policy: core + tool_search (+ descubiertas en sesión)
LLM:      tool_search(query)            → busca internas y MCP
          tool_search(tool_name, params) → ejecuta: locales directas,
                                           MCP vía gateway.CallTool (stats incluidas)
```

- Las favoritas del gateway ya no pueden ser diferidas por la política
  (están registradas como tools vivas y la sincronización de catálogo las
  respeta: una tool viva siempre gana a su entrada de catálogo).
- El estado de descubrimiento (`MarkDiscovered`) ya no se pierde entre turnos:
  el registro es compartido y el upsert preserva el mapa `discovered`.
- `mcp_query_catalog`/`mcp_call_tool` siguen existiendo solo en el path legacy
  (discovery desactivado con gateway activo, y modo `pando mcp-server` con
  `GatewayExpose`).

## Verificación

- `go build ./...` — OK.
- `go test ./...` — todo el suite pasa.
- Tests nuevos:
  - `internal/llm/tooldiscovery/unified_test.go`: upsert, `SyncCatalogEntries`
    (tool viva gana, entradas obsoletas se eliminan, discovered sobrevive),
    `ExecuteTool` local/remoto/no encontrado, búsqueda de entradas de
    catálogo, política que fuerza `tool_search` con entradas remotas.
  - `internal/llm/tools/tool_search_test.go`: modo call con/sin ejecutor,
    validación query-o-tool_name.
  - `internal/llm/agent/tool_discovery_unified_test.go`: integración con
    gateway SQLite real — catálogo buscable vía `tool_search`, ejecución
    enrutada al gateway, ausencia de proxies legacy, fallback con discovery
    desactivado.
  - `internal/llm/tooldiscovery/policy_test.go`: helper corregido para generar
    nombres únicos (el upsert ahora deduplica con razón).
- Corregido comentario con texto corrupto en `policy.go:189` y evitada su
  propagación a `tool_discovery.go` (ambos decían `<server>_[PROMPT_INJECTION]`
  en lugar de `<server>_<toolname>`).

## Decisiones tomadas

1. **Una sola herramienta** (`tool_search` con `tool_name`+`parameters`) en
   lugar de conservar `mcp_call_tool` renombrada — requisito del usuario.
2. **Ejecución de internas diferidas**: directa desde el registro en la misma
   llamada (no esperar al turno siguiente).
3. **Estado de descubrimiento**: registro compartido process-wide (no por
   sesión en DB); el mapa `discovered` es global por proceso, suficiente pues
   solo amplía visibilidad.
4. **Compatibilidad**: `MCPGateway.Enabled` sigue funcionando como override;
   `GatewayExpose` del modo mcp-server intacto.

## Riesgos / seguimiento

- La sincronización del catálogo ocurre al reconstruir tools; un refresh de
  servidor MCP (`refreshMCPServerTools`) se refleja en el próximo ensamblado.
- Con muchos servidores, `GetAllTools` en cada ensamblado es una consulta
  SQLite completa; si se vuelve costosa, añadir caché/invalidación.
- Fase pendiente del plan original: FTS5/embeddings para el ranking
  (hoy léxico ponderado).
