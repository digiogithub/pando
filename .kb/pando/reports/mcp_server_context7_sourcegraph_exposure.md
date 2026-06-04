# Informe: Exposición de Context7 y Sourcegraph en modo MCP Server

**Fecha:** 2026-06-04
**Archivo modificado:** `cmd/mcp_server.go`

## Hallazgo

Las herramientas **Context7** (`c7_resolve_library_id`, `c7_get_library_docs`) y **Sourcegraph** (`sourcegraph`) estaban implementadas en:

- `internal/llm/tools/context7.go` — `NewContext7Tools()` retorna ambas tools de Context7
- `internal/llm/tools/sourcegraph.go` — `NewSourcegraphTool()` retorna la tool de Sourcegraph

Estas herramientas se usaban **solo en el agente interno** (`internal/llm/agent/tools.go`), condicionadas por los flags de configuración:

- `InternalTools.Context7Enabled` (default: `false`)
- `InternalTools.SourcegraphEnabled` (default: `false`)

**NO estaban expuestas** en la función `buildMCPServerTools()` de `cmd/mcp_server.go`, que es donde se registran las herramientas disponibles cuando Pando corre como MCP server (modo `pando mcp-server`).

Las demás herramientas (fetch, search engines, browser, remembrances, mesnada, cache, file tools, bash, gateway, self-improvement) sí estaban expuestas.

## Cambio realizado

Se añadieron Context7 y Sourcegraph en `buildMCPServerTools()` (líneas 312-325 de `cmd/mcp_server.go`), **condicionados a la misma configuración** que usa el agente:

```go
// Context7 library documentation tools
if cfg != nil && cfg.InternalTools.Context7Enabled {
    tools = append(tools, llmtools.NewContext7Tools()...)
    logging.Info("MCP server: Context7 tools enabled")
}

// Sourcegraph code search tool
if cfg != nil && cfg.InternalTools.SourcegraphEnabled {
    tools = append(tools, llmtools.NewSourcegraphTool())
    logging.Info("MCP server: Sourcegraph tool enabled")
}
```

Se colocaron justo antes del bloque de Mesnada, siguiendo el orden lógico del archivo (herramientas de búsqueda/docs → orquestación → remembrances → condicionales).

## Activación

Para que se expongan en MCP server mode, el usuario debe tener en su `.pando.toml`:

```toml
[InternalTools]
Context7Enabled = true
SourcegraphEnabled = true
# SourcegraphToken = "sgp_..." # opcional, sin token usa API pública
```

## Verificación

- Compilación exitosa (`go build ./cmd/...` sin errores)
- No se requieren cambios en config, modelo de datos ni API — solo se reutilizan los flags existentes
