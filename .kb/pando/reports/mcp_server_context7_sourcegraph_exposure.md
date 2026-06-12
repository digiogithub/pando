# Report: Context7 and Sourcegraph Exposure in MCP Server Mode

**Date:** 2026-06-04
**Modified file:** `cmd/mcp_server.go`

## Finding

**Context7** (`c7_resolve_library_id`, `c7_get_library_docs`) and **Sourcegraph** (`sourcegraph`) tools were implemented in:

- `internal/llm/tools/context7.go` — `NewContext7Tools()` returns both Context7 tools
- `internal/llm/tools/sourcegraph.go` — `NewSourcegraphTool()` returns the Sourcegraph tool

These tools were used **only in the internal agent** (`internal/llm/agent/tools.go`), gated by configuration flags:

- `InternalTools.Context7Enabled` (default: `false`)
- `InternalTools.SourcegraphEnabled` (default: `false`)

They were **NOT exposed** in the `buildMCPServerTools()` function of `cmd/mcp_server.go`, which is where available tools are registered when Pando runs as an MCP server (the `pando mcp-server` mode).

The other tools (fetch, search engines, browser, remembrances, mesnada, cache, file tools, bash, gateway, self-improvement) were exposed.

## Change made

Context7 and Sourcegraph were added in `buildMCPServerTools()` (lines 312-325 of `cmd/mcp_server.go`), **gated by the same configuration** used by the agent:

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

They were placed just before the Mesnada block, following the file's logical order (search/docs tools → orchestration → remembrances → conditional).

## Activation

For these to be exposed in MCP server mode, the user must have the following in their `.pando.toml`:

```toml
[InternalTools]
Context7Enabled = true
SourcegraphEnabled = true
# SourcegraphToken = "sgp_..." # optional, uses public API without token
```

## Verification

- Successful compilation (`go build ./cmd/...` with no errors)
- No changes to config, data model, or API are required — only existing flags are reused
