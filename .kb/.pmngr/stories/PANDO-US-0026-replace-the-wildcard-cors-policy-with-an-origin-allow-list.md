---
id: PANDO-US-0026
type: story
title: Replace the wildcard CORS policy with an origin allow-list
status: done
priority: high
parent: PANDO-EP-0006
milestone: PANDO-M-0002
author: claude
labels: [mcp, security]
estimate: 3
created: 2026-09-13T21:16:10Z
updated: 2026-09-13T21:16:21Z
---

## Description

As a user with a browser, I want the MCP port to refuse cross-origin calls from pages I merely visit, so that a random web page cannot preflight its way into Pando's tool surface on localhost.

`corsMiddleware` (`internal/mesnada/server/server.go:148-162`) sets `Access-Control-Allow-Origin: *` with `Allow-Methods: GET, POST, DELETE, OPTIONS` and `Allow-Headers: Content-Type, Mcp-Session-Id, ACP-Session-Id`, and answers `OPTIONS` with `204`. A JSON POST with a custom header is a non-simple request, so the browser preflights — and the server approves the preflight for every origin. Combined with `SetGlobalAutoApprove(true)` (`cmd/mcp_server.go:125`), any page the user opens can drive the tool surface.

Implementation: `corsMiddleware` reads `HttpAllowedOrigins` (added by the bearer-token story in this epic). When the list is empty, emit no CORS headers at all and answer `OPTIONS` with `403` — loopback Go clients are not browsers and need none, so the empty list is the correct default. When the list is non-empty, echo the request's `Origin` only when it matches an entry exactly, add `Vary: Origin`, and keep `Allow-Credentials` unset. Never echo an unmatched origin, and never combine `*` with credentials.

Then add the regression test the epic requires, in the `internal/mesnada/server` package: drive a real `github.com/modelcontextprotocol/go-sdk` v1.4.1 streamable client against a test server over one session, with the token set, and assert initialize → tools/list → tools/call all succeed. The same test must pin the two behaviours the SDK client depends on and that this epic's middleware must not disturb: a `GET /mcp` returns `405` (`handleMCP:293-297`), which the client treats as "no standalone SSE stream" and not as an error, and `Close()` sends `DELETE /mcp` whose status the client ignores. Also assert that `Mcp-Session-Id` is minted on the first request and echoed unchanged on every subsequent one (`server.go:299-312`), since the SDK errors on a mismatch.

Do NOT make the allow-list a substitute for the token — an allow-listed origin still has to authenticate. Do NOT relax the `405` on `GET /mcp` into a real SSE stream as part of this story.

## Acceptance Criteria

- [ ] With `HttpAllowedOrigins` empty, no `Access-Control-*` header is emitted on any response and a preflight `OPTIONS` is refused.
- [ ] With a non-empty list, only an exactly matching `Origin` is echoed, `Vary: Origin` is set, and a non-matching origin gets no CORS headers; `*` appears nowhere in the package.
- [ ] A regression test runs the MCP Go SDK v1.4.1 client through initialize, tools/list and tools/call against the server with a token and passes.
- [ ] The same test asserts `GET /mcp` returns `405` and that the client's `Close()` (a `DELETE /mcp`) does not surface an error.
- [ ] The session id is minted once and echoed identically on every later response in the session.

## Notes

Size S. Depends on PANDO-US-0025 for the `HttpAllowedOrigins` config key and the token the regression test uses. Known wrinkles to note but not fix here: `ReadTimeout: 30s` with `WriteTimeout: 0` (`server.go:141-144`) cuts off a slow request body but not a slow embedding call, and the session map (`server.go:114`) has no eviction, so a client that opens a session per call leaks one struct per call. Evidence in `report-search-and-fit.md` §A10-§A11.
