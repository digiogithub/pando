---
created_at: 2026-09-14T16:30:14.453690709Z
updated_at: 2026-09-14T16:36:08.445201949Z
tags:
    - fix
    - security
    - mcp
---

# MCP HTTP transport: bearer token + CORS allow-list

Closes [[PANDO-EP-0006]] via two stories. See
`.kb/.pmngr/stories/PANDO-US-0025-bearer-token-on-the-mcp-http-transport.md` and
`.kb/.pmngr/stories/PANDO-US-0026-replace-the-wildcard-cors-policy-with-an-origin-allow-list.md`
for the authoritative specs.

## PANDO-US-0025 — Bearer token on the MCP HTTP transport (DONE)

### What changed

- `internal/config/config.go` — `MCPServerConfig` gains two fields, next to `HttpHost`:
  - `HttpToken string` (`json:"httpToken,omitempty" toml:"HttpToken"`). Field name ends in
    "Token" so `internal/redact.IsSecretKey` (suffix match on "token") automatically masks it
    from any generic config-dump redaction path (e.g. `internal/llm/tools/pando_setup.go`'s
    `redactSetupValue`) with no extra code — confirmed by reading that code path, not modified
    (out of scope: `internal/llm/`).
  - `HttpAllowedOrigins []string` (`json:"httpAllowedOrigins,omitempty" toml:"HttpAllowedOrigins"`).
  - Plain (non-pointer) types: unlike `AGUIConfig.Mesnada *bool`, absent-vs-empty does not need
    to be distinguishable here, since both "key absent" and "explicit empty" mean the same thing
    (generate a token; emit no CORS headers).

- `internal/mesnada/server/server.go`:
  - `Config.Token string` / `Server.token string` — bearer token carried into the server.
  - `bearerMiddleware(next http.Handler) http.Handler` (new method) — requires
    `Authorization: Bearer <token>` via `subtle.ConstantTimeCompare` (length-checked first,
    which leaks nothing since token length isn't secret). Exempts `/health`. When `s.token == ""`
    it is a no-op passthrough — preserves current behavior for the *other* caller of
    `mesnadaServer.New` (`internal/app/app.go:688`, the embedded Mesnada orchestrator server),
    which this story does not touch and which never sets `Config.Token`.
  - Wiring: `Handler: s.corsMiddleware(s.bearerMiddleware(mux))` — bearer sits inside CORS, so
    a preflight `OPTIONS` (which `corsMiddleware` answers itself, never calling `next`) is never
    subject to the token check.
  - `writeUnauthorized` helper writes a `401` with a small JSON body.

- `cmd/mcp_server.go` (construction site, `runMCPServerMode`):
  - `ensureMCPHTTPToken(host, configuredToken string) (token string, generated bool, err error)`
    (new, pure, unit-tested): configured token passes through; empty token on a loopback host
    generates a random 32-byte/64-hex-char token; empty token on a non-loopback host is refused
    with an error naming `MCPServer.HttpToken` explicitly.
  - `isLoopbackMCPHost(host string) bool` — local duplicate of the loopback check in
    `internal/api/basicauth.go` (not shared: `internal/api` was out of scope for this spec, and
    `internal/mesnada/server` cannot depend on `internal/api`).
  - `randomMCPHTTPToken()` — 32 random bytes via `crypto/rand`, hex-encoded, mirroring
    `cmd/agui_serve.go`'s `randomToken()`. Printed once to stderr via `fmt.Fprintf`, never through
    `logging.*`.
  - `mesnadaServer.Config{..., Token: httpToken}` passed at the HTTP-transport construction site.
    The `stdioSrv` construction (`UseStdio: true`) is untouched.

### Tests

`internal/mesnada/server/auth_test.go` (new): correct token passes, wrong token / missing header
/ malformed header (no scheme, bare "Bearer", wrong scheme, wrong case) → 401, `/health` exempt,
empty `Config.Token` stays fully unauthenticated (back-compat pin), query-string `?token=`
explicitly rejected (header-only), full-chain test for `/mcp` and `/mcp/sse`.

`cmd/mcp_server_test.go` (new): `ensureMCPHTTPToken` for configured/loopback-generate/
non-loopback-refuse (error names `MCPServer.HttpToken`)/non-loopback-with-configured-token, two
generated tokens differ, `isLoopbackMCPHost` table test.

### BREAKING CHANGE — called out per instructions

Before this change, `pando mcp-server`'s HTTP transport was **fully unauthenticated on
loopback**. After this change, even a loopback bind with no `MCPServer.HttpToken` configured
generates and enforces a fresh random token on every start (printed once to stderr) — so **any
existing MCP HTTP client not already sending `Authorization: Bearer <token>` gets 401s** after
upgrading. This is the deliberate intent of PANDO-EP-0006, implemented literally per spec, not an
accident. Operators must either capture the printed token and configure their client, or pin
`MCPServer.HttpToken` in `.pando.toml`.

## PANDO-US-0026 — Origin allow-list for MCP HTTP CORS (DONE)

### What changed

- `internal/mesnada/server/server.go`:
  - `Config.AllowedOrigins []string` / `Server.allowedOrigins []string` — new fields.
  - `corsMiddleware` rewritten: no more `Access-Control-Allow-Origin: *`. Reads
    `s.allowedOrigins`; `isAllowedOrigin(origin)` (new helper) matches the request's `Origin`
    header verbatim against the list (empty `Origin` or empty list never matches). When
    allowed: sets `Vary: Origin`, echoes `Access-Control-Allow-Origin: <origin>` (never `*`),
    `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers` (now includes `Authorization`,
    needed since US-0025 requires it on every call — otherwise an allow-listed browser origin
    could never actually send the bearer token past a real preflight), and
    `Access-Control-Expose-Headers`. `Access-Control-Allow-Credentials` is never set (was never
    set before either). A preflight `OPTIONS` is answered `204` only when allowed, else `403`.
    Non-preflight requests always reach `next` regardless of CORS outcome (CORS is a browser-side
    read gate, not a server-side block on GET/POST) — only the preflight is refused outright.
  - No literal `"*"` remains anywhere in the package's CORS logic (verified by grep and by a
    dedicated test).
  - `cmd/mcp_server.go` passes `cfg.MCPServer.HttpAllowedOrigins` into
    `mesnadaServer.Config.AllowedOrigins` at the HTTP construction site, and warns to stderr
    (mirroring `cmd/agui_serve.go`'s equivalent origin warning) when the list is empty on
    startup — this is the *safe* default, not a misconfiguration, but operators exposing a
    browser client need to know they must configure it.
  - Added `github.com/modelcontextprotocol/go-sdk v1.4.1` as a direct module dependency (`go get`
    + `go mod tidy`); pulled in `segmentio/asm`/`segmentio/encoding` (indirect) and bumped
    `golang-jwt/jwt/v5` v5.2.2 → v5.3.0 (indirect, required transitively by go-sdk; confirmed
    `internal/llm/provider`, the only in-repo user of that jwt package, still builds and its
    tests still pass unchanged).

### Tests

- `internal/mesnada/server/cors_test.go` (new): empty allow-list emits no CORS headers on a
  normal request and refuses preflight with 403 (with and without an `Origin` header); matching
  origin is echoed with `Vary: Origin` and no `Access-Control-Allow-Credentials`, preflight
  succeeds `204` and `Allow-Headers` includes `Authorization`; non-matching origin (non-empty
  list) gets no headers and preflight is refused `403`; a dedicated test asserts none of the four
  CORS response headers is ever a bare `"*"`.
- `internal/mesnada/server/mcp_sdk_regression_test.go` (new) — the spec-mandated regression test:
  drives a real `github.com/modelcontextprotocol/go-sdk` v1.4.1 `mcp.StreamableClientTransport` +
  `mcp.NewClient` against an `httptest.Server` wrapping `srv.httpServer.Handler`, with the bearer
  token attached via a custom `http.RoundTripper` (the SDK transport has no built-in static-header
  option). Exercises `Connect` (initialize) → `ListTools` → `CallTool` against a registered
  `echo_regression` native tool, then `Close()` (asserted to return no error — pins that a `DELETE
  /mcp` answered `405` by `handleMCP` does not surface as a client error, since the SDK's
  `streamableClientConn.Close` only checks the transport-level `err` from `client.Do`, never the
  HTTP status). A `sessionIDCapturingRoundTripper` records `Mcp-Session-Id` on every response and
  asserts it is identical throughout. `TestMCPGoSDKClient_GetMCPReturns405` separately pins the
  authenticated `GET /mcp` → `405` behavior `handleMCP` already had (unchanged, not relaxed into
  a real SSE stream, per the spec's explicit "do NOT" constraint).

### Verification

- `go build ./...` — clean (confirmed with a clean, up-to-date `internal/agui` after that
  package's own concurrent in-flight edits from another agent settled; my changes never touched
  `internal/agui`).
- `go test ./cmd/... ./internal/mesnada/server/... ./internal/config/...` — all pass.
- `go vet` on the same packages — clean; `gofmt -l` clean.
- `go test ./internal/app/...` and `./internal/api/...` (unaffected by this work, sanity-checked
  since `internal/app/app.go` has the other `mesnada.New` call site) — pass.
- Scope check via `git status`: only `cmd/mcp_server.go`, `cmd/mcp_server_test.go`, `go.mod`,
  `go.sum`, `internal/config/config.go`, `internal/mesnada/server/{server.go,auth_test.go,
  cors_test.go,mcp_sdk_regression_test.go}` touched by this work.

### Notes / residual risk carried forward (not fixed here, per spec's explicit scope)

- `internal/app/app.go`'s embedded Mesnada orchestrator server (`cfg.Mesnada.Server`, a
  *different* HTTP server from `pando mcp-server`) shares this same `corsMiddleware` /
  `bearerMiddleware` code since they're both built via `mesnadaServer.New`. It never sets
  `Config.Token` or `Config.AllowedOrigins`, so its behavior is: still fully unauthenticated
  (unchanged), and CORS goes from wildcard `*` to "no CORS headers, preflight refused" (a
  *hardening*, not a break, for any same-origin/non-browser caller — flagged here since
  `internal/app/` was out of scope to modify and the spec's file/method names are package-wide).
- `ReadTimeout: 30s` / `WriteTimeout: 0` and the unbounded session map (`server.go:114`, no
  eviction) are known wrinkles called out in US-0026's notes as explicitly not fixed by this
  story.