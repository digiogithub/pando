---
id: PANDO-US-0025
type: story
title: Bearer token on the MCP HTTP transport
status: backlog
priority: high
parent: PANDO-EP-0006
milestone: PANDO-M-0002
author: claude
labels: [mcp, security]
estimate: 5
created: 2026-09-13T21:16:10Z
updated: 2026-09-13T21:16:10Z
---

## Description

As an operator running `pando mcp-server`, I want the HTTP transport to require a token, so that a process that merely reached the port cannot drive the whole tool surface.

`mesnadaServer.Config` (`internal/mesnada/server/server.go:97-107`) has no token, no auth and no TLS field, and there is no `Authorization` check anywhere in the package. `New` mounts `/mcp`, `/mcp/sse`, `/health` and a Gin engine on `/` and wraps them only in `corsMiddleware` (`server.go:127-146`). `cmd/mcp_server.go:77-78` defaults the port to 9777, and `cmd/mcp_server.go:125` calls `pandoApp.Permissions.SetGlobalAutoApprove(true)`, so every call that reaches a handler is auto-approved — with the system-execution tool group enabled that is unauthenticated command execution.

Implementation, following the precedent already in the repository (`internal/api/server.go:448-453` for the compare, `:480-517` for the prefix-based gate):

- Add `HttpToken string` and `HttpAllowedOrigins []string` to `MCPServerConfig` (`internal/config/config.go:594-620`), beside `HttpPort` and `HttpHost`, with `json`/`toml` tags in the existing style.
- Carry the token into `mesnadaServer.Config` at the construction site (`cmd/mcp_server.go:161-175`) and add a `bearerMiddleware` wrapping the mux at `server.go:140`, inside the CORS wrapper, using `subtle.ConstantTimeCompare` on `Authorization: Bearer <token>`. Exempt `/health` — it is a liveness probe with no data.
- When no token is configured, generate one at startup, print it to stderr the way `cmd/agui_serve.go` does for the AG-UI server, and never log it again. Redact it from any config dump.
- Refuse to start when `HttpHost` resolves to a non-loopback interface and no token is configured, with an error naming the config key.

Do NOT treat loopback binding as the security boundary — it is not one against the user's own browser, which is what the CORS story in this epic addresses. Do NOT leave the token in a query parameter; header only. Do NOT put the check inside `handleMCP` (`server.go:293`) — a middleware covers `/mcp/sse` and the Gin engine as well.

## Acceptance Criteria

- [ ] `MCPServerConfig` carries `HttpToken` and `HttpAllowedOrigins`, both settable from the TOML config.
- [ ] With a token configured, `/mcp` and `/mcp/sse` answer `401` without a matching `Authorization: Bearer`, and the comparison is constant-time.
- [ ] `/health` answers without a token.
- [ ] Starting with a non-loopback `HttpHost` and no token fails with an error that names the config key; starting on loopback with no token generates a token, prints it once to stderr and does not log it again.
- [ ] Unit tests cover: correct token, wrong token, missing header, malformed header, `/health` exemption.

## Notes

Size S-M. Must land before the CORS allow-list story in this epic (that one narrows the origin policy and needs the token to exist to be meaningful), and the regression test it carries also exercises this middleware. Not on git-in-track's critical path: GIT-US-0077's client refuses non-loopback URLs and the residual risk is documented, but it is a latent foot-gun for every host. Evidence in `report-search-and-fit.md` §A10.
