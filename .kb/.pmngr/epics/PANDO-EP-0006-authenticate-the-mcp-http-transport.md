---
id: PANDO-EP-0006
type: epic
title: Authenticate the MCP HTTP transport
status: backlog
priority: high
milestone: PANDO-M-0002
labels: [mcp, security]
created: 2026-09-13T21:11:26Z
updated: 2026-09-13T21:11:26Z
---

## Description

`pando mcp-server` serves `/mcp` and `/mcp/sse` on port 9777 with no authentication of any kind (`internal/mesnada/server/server.go:97-127` has no token field), answers `Access-Control-Allow-Origin: *` (`server.go:148-162`) and runs under `SetGlobalAutoApprove(true)` (`cmd/mcp_server.go:125`). Any web page the user's browser visits can drive the full tool surface; with `--system-exec` that is unauthenticated remote code execution, and loopback binding is no defence against the user's own browser.

## Acceptance Criteria

- [ ] `MCPServerConfig` gains `HttpToken` and `HttpAllowedOrigins`; `/mcp` and `/mcp/sse` require `Authorization: Bearer` with a constant-time compare when a token is configured; `/health` does not.
- [ ] Binding a non-loopback host without a token is refused at startup; a generated token is printed to stderr when none is configured and never logged afterwards.
- [ ] CORS echoes only allow-listed origins and emits no CORS headers when the list is empty; `*` is gone.
- [ ] The MCP Go SDK client (`github.com/modelcontextprotocol/go-sdk` v1.4.1) still completes initialize, tools/list and tools/call with the token, and the standalone-SSE 405 and close-DELETE paths keep their current behaviour, under a regression test.

## Notes

Not on git-in-track's critical path (its client refuses non-loopback URLs and the risk is documented), but a latent foot-gun independent of any host. Precedent for the middleware in `internal/api/server.go`. Evidence in `report-search-and-fit.md` §A9 and `report-agui-server.md` §4.
