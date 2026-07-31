---
created_at: 2026-07-31T12:19:29.810774669Z
updated_at: 2026-07-31T12:19:29.810774669Z
tags:
    - fix
    - mcp
    - tui
    - webui
    - tools
---
# Fix: MCP servers showed 0 tools + TUI rejected header values with spaces (2026-07-31)

## Symptoms reported

Trying to configure the Figma MCP server (remote `https://mcp.figma.com/mcp` and
local stdio/http variants):

1. **TUI**: header values containing spaces (`Authorization: Bearer <token>`)
   were rejected with `invalid header pair ... expected Key:Value format`.
2. **WebUI**: headers + auth type could be entered and saved, but the server kept
   showing `0 tools`, and neither "reload" nor restarting the server changed it.

## Root causes

- `parseHeaderPairs` (`internal/tui/components/dialog/add_mcp_server.go`) and
  `parseHeaders` (`internal/tui/page/settings.go`) both split the input with
  `strings.Fields`, so any header VALUE containing a space was parsed as
  separate pairs and failed validation. `headersToString` rendered with the same
  space-separated syntax, so round-tripping was impossible.
- The WebUI "Tools" column reads `mcp_tool_registry`, which was only ever
  populated by `Gateway.Initialize` → `Registry.DiscoverAll` at process start
  (and only when `MCPGateway.Enabled`). Adding/editing a server never triggered
  discovery.
- `handleReloadMCPServer` was a stub: it validated the name and returned
  `{"status":"reload scheduled"}` without reconnecting, so the user got no tool
  count and, crucially, **no connection error** explaining the zero.
- Even after `agent.ResetMcpToolsCache()`, a *running* agent kept its tool slice
  (`agent.tools` is captured at construction), so new MCP tools were never
  offered to the model without a full restart.

## Changes

- `internal/config/mcp_headers.go` (new): `ParseHeaderPairs` / `FormatHeaderPairs`.
  Canonical syntax is comma- or newline-separated `Key: Value`, so values may
  contain spaces; the legacy whitespace-separated form still parses when every
  token carries its own `:`. Tests in `internal/config/mcp_headers_test.go`.
- `internal/tui/components/dialog/add_mcp_server.go`,
  `internal/tui/page/settings.go`: both parsers and `headersToString` now
  delegate to the config helpers; hints updated to the comma syntax.
- `internal/mcpgateway/registry.go`: added `Registry.DiscoverServer(ctx, name,
  srv)` — single-server discovery that replaces the server's catalog rows and
  **returns** the connection error instead of only logging it.
- `internal/mcpgateway/gateway.go`: added `Gateway.RefreshServer` — resolves
  secrets, evicts the pooled client (so new headers/credentials take effect) and
  calls `DiscoverServer`.
- `internal/api/handlers_config.go`: new `Server.refreshMCPServerTools` helper
  (works with or without the gateway, falling back to `mcpgateway.NewRegistry`
  over `s.config.DB`, 45s timeout). PUT now refreshes after saving; DELETE now
  purges the catalog rows; `handleReloadMCPServer` really reconnects and returns
  `{status, name, tools}` or HTTP 502 with the underlying error.
- `internal/llm/agent/agent.go`: `agent.tools` guarded by `toolsMu`; new
  `SetTools` + `ToolsSetter` interface (kept OUT of `agent.Service` so test
  doubles need no update).
- `internal/app/app.go`: `App.coderToolsBuilder` + `App.RefreshAgentTools()`
  rebuild and install the coder agent's tool set at runtime.
- `internal/tui/page/settings.go`: add/edit/delete of an MCP server now triggers
  a background `RefreshServer` + `RefreshAgentTools`.
- `web-ui/src/stores/mcpServersStore.ts`: `reloadServer` surfaces the discovered
  tool count as a toast, shows the real error on failure, and always refetches.

## Verification

- `go build ./...`, `go vet` on touched packages, `gofmt` clean.
- `go test ./internal/api ./internal/mcpgateway ./internal/config ./internal/llm/agent` — all pass.
- `npx tsc --noEmit` in `web-ui` — clean.
- Persistence of a Figma-like `streamable-http` server with a spaced header value
  and a bearer `Auth` block verified against a temp config: both land in
  `.pando.toml` AGE-encrypted.

## Note for the Figma case

The remote `https://mcp.figma.com/mcp` endpoint requires OAuth: set the auth type
to `oauth` and authorize with `pando mcp login figma` (or the WebUI Login button)
— a bare bearer header is not enough. The local Figma desktop server
(`http://127.0.0.1:3845/mcp`) needs no auth but requires the desktop app running
with Dev Mode MCP enabled.

Related: [[feature_mcp_client_authentication]], [[mcp_server_config_fields]],
[[pando-mcp-gateway-implementation]], [[mcp_client_auth_phase1_static]]
