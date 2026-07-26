---
created_at: 2026-07-27T04:41:09.294534152Z
updated_at: 2026-07-27T04:41:09.294534152Z
tags:
    - feature
    - mcp
    - auth
    - oauth
    - cli
---
# Feature: MCP client interactive OAuth flow + CLI (Phase 3)

Status: COMPLETE (2026-07-27). Implements Phase 3 of [[pando-mcp-client-authentication-oauth-plan]]:
the local callback server, the flow driver, `pando mcp` CLI commands, and "needs authorization"
surfacing in the agent layer. Builds directly on [[pando/features/mcp_client_auth_phase2_oauth_storage.md]]
and [[pando/features/mcp_client_auth_phase1_static.md]].

## What changed

### `internal/mcpauth/callback.go` (new)

`CallbackServer` — a short-lived local HTTP server for the OAuth redirect leg:

- `StartCallbackServer(redirectURI string) (*CallbackServer, error)` parses the configured
  redirect URI (falling back to `DefaultCallbackPort`/`DefaultCallbackPath` when empty/unparseable)
  and binds **only** to `127.0.0.1` (loopback host required; non-loopback redirect URIs are
  rejected). Passing port `0` in the redirect URI lets the OS pick an ephemeral port — this is
  the test hook used throughout `callback_test.go`.
- `(*CallbackServer) RedirectURI() string` returns the actual bound `redirect_uri` (useful when
  port 0 was requested).
- `(*CallbackServer) Wait(ctx, state, timeout) (code string, err error)` blocks for the redirect.
  Default timeout is `DefaultCallbackTimeout` (5 min) when `timeout <= 0`.
- `(*CallbackServer) Close() error` — idempotent shutdown.
- Security invariants enforced in `handleCallback`, all covered by tests: missing/mismatched
  `state` → HTTP 400 "possible CSRF" and the pending `Wait` is **not** resolved (an attacker
  probing the endpoint must not be able to poison or prematurely end a legitimate wait);
  `error`/`error_description` query params reject `Wait` with that message; missing `code` is an
  error; any path other than the configured one 404s (plain `http.ServeMux` semantics); loopback
  binding only. A minimal self-contained HTML success/error page is served (no external assets).

### `internal/mcpauth/login.go` (new)

- `LoginPrompt` struct: `OnAuthorizationURL func(string)`, `OpenBrowser bool`,
  `OnBrowserError func(error)`, `ManualCode func(ctx) (code, state string, err error)`,
  `Timeout time.Duration`. UI-agnostic — CLI today, TUI/WebUI can reuse it later.
- `(*Manager) Login(ctx, serverName string, srv config.MCPServer, p LoginPrompt) error` drives the
  full authorization-code + PKCE flow: rejects stdio servers and non-oauth `Auth.Type`; resolves
  secrets; builds `transport.OAuthConfig` via the existing `Manager.OAuthConfig`; starts the
  callback server and rewrites `RedirectURI` to whatever port it actually bound; builds the
  `*transport.OAuthHandler` via `transport.NewOAuthHandler` + `SetBaseURL`; calls `RegisterClient`
  only when no client_id is available (static config always wins), returning an actionable error
  naming `Auth.OAuth.ClientID` when the server doesn't support DCR; generates state/PKCE verifier,
  calls `SetExpectedState`/`GetAuthorizationURL`, invokes `OnAuthorizationURL`, and opens the
  browser via `internal/auth.OpenBrowser` when requested — a browser-open failure is reported via
  `OnBrowserError` but never aborts the flow, since the URL was already surfaced. Waits for the
  callback (or drives `ManualCode` for headless/paste flows — a bare pasted code with no visible
  state is accepted since the user themselves navigated the Pando-generated URL locally, so there
  is no network attacker to defend against there, unlike the HTTP callback path). Exchanges the
  code via `ProcessAuthorizationResponse`, persists the client registration via the existing
  `PersistClientRegistration`, and verifies with `HasTokens` before returning success.
- `(*Manager) Status(serverName string, srv config.MCPServer) StatusInfo` — `{ServerName,
  ServerURL, AuthType, HasTokens, ExpiresAt, Expired, ClientID, DynamicallyRegistered}`.
  `DynamicallyRegistered` is inferred: a persisted `ClientID` is flagged as DCR-obtained only when
  static config has no `ClientID` of its own (config always wins in `OAuthConfig`, so a persisted
  ID that differs from an *absent* static one could only have come from `RegisterClient`).
- `IsInteractive() bool` (via `golang.org/x/term`, already a transitive dependency) and
  `NonInteractiveLoginError(serverName string) error` — "MCP server %q requires authorization and
  no interactive session is available. Run: pando mcp login %s". Not yet wired into a
  non-interactive call site (no such call site exists in this phase — the agent layer only warns,
  see below); available for Phase 4/TUI/WebUI to call before attempting an interactive login.

### `cmd/mcp.go` (new)

`pando mcp` command group, registered on `rootCmd` the same way `cmd/kb.go`'s `kb relink` is:
config loaded via the lightweight `config.Load(cwd, false, "")` + `config.Get()` path (no
`app.New`/DB bootstrap — matches `cmd/secret.go`/`cmd/kb.go`, not the heavier `cmd/mcp_server.go`
path).

- `pando mcp list` — table: name, type, auth type, status (`ok` / `expired (auto-refreshes on
  use)` / `needs login` / `n/a` for non-oauth servers).
- `pando mcp login <name>` — flags `--no-browser`, `--manual` (paste back the full redirect URL,
  parsed by `parseManualCodeInput`, or a bare code), `--timeout` (default 5m). Prints the
  authorization URL unconditionally (browser-open is a convenience, not the only path).
- `pando mcp logout <name>` — confirmation prompt unless `--yes`.
- `pando mcp status [name]` — with a name, **exits non-zero** when that server still needs
  authorization (`needs login`), so it's usable as a script precondition; without a name, prints
  every configured server's status and always exits 0.
- Every command resolves `--age-keys` the same way `cmd/secret.go` does and gives a clear
  "no MCP server named %q is configured" error for typos.

### `internal/llm/agent/mcp-tools.go`

Three call sites that create/initialize an MCP client now special-case
`mcpclient.IsAuthorizationRequired(err)` and publish an actionable warning instead of the generic
connection error, via the existing `mcpclient.PublishWarn`:

- `mcpTool.Run` (per-call client creation) — returns the actionable message as the tool's error
  response instead of the generic `mcpToolErrorResponse`.
- `GetMcpTools` (discovery-time client creation per configured server).
- `getTools` (discovery-time `Initialize` failure) — added for symmetry even though the phase spec
  named only the first two call sites, since the same 401→authorization-required condition can
  surface here too.

Message format in all three: `MCP server %q requires authorization. Run: pando mcp login %s`.

## Why

Phase 2 built the credential store and process-wide `Manager` but deliberately stopped short of
driving the interactive browser/callback flow. Without Phase 3, an OAuth-configured server would
only ever fail with "authorization required" and no way for the user to actually authorize it
short of hand-editing `mcp-auth.json`.

## How verified

- `go build ./...` — clean.
- `go vet ./internal/mcpauth/... ./internal/mcpclient/... ./internal/llm/agent/... ./cmd/...` — clean.
- `go test ./internal/mcpauth/... ./internal/mcpclient/... ./internal/llm/agent/... ./internal/config/... ./cmd/...` — all pass.
- `go test -race ./internal/mcpauth/...` — pass, no races.
- New tests: `internal/mcpauth/callback_test.go` (state mismatch/missing-state rejection incl. the
  "pending Wait must not resolve" check, error-param propagation, missing-code error, happy path,
  wrong-path 404, timeout, context cancellation, idempotent Close, loopback-only binding,
  `parseRedirectURI` table test) and `internal/mcpauth/login_test.go` (full happy-path login
  against a stub authorization server via `httptest.Server` + `AuthServerMetadataURL` pointed
  directly at it — token endpoint issues a real access token that ends up persisted in the Store;
  "no client_id and DCR unsupported" error path; stdio/non-oauth rejection; `Status` computation
  including expired-token and dynamically-registered-clientID detection; `NonInteractiveLoginError`
  message shape). No `cmd`-level unit test was added beyond the manual CLI smoke test below, since
  `cmd/root_test.go`'s existing style tests pure helper functions and argument-parsing edge cases
  rather than full command execution against a live config — the manual smoke test below covers
  that gap for this phase.
- Manual smoke test: built a temp binary, ran it from a temp working directory with a local
  `.pando.toml` declaring one `oauth` and one `bearer` MCP server. Confirmed `pando mcp list`
  shows correct type/auth/status columns, `pando mcp status <name>` exits 1 for a
  needs-authorization server and prints details, `pando mcp login <name> --no-browser --timeout 2s`
  fails cleanly (and correctly) against an unreachable dummy host during metadata discovery
  (`dial tcp: lookup example.test: no such host` — the expected failure mode with no real AS to
  talk to), and `pando mcp logout <name> --yes` removes the entry without prompting. Also ran
  `go run . mcp --help` / `mcp login --help` to confirm command registration and flags.

## What Phase 4 must know

- RFC 9207 `iss` validation is not implemented anywhere in this phase — mcp-go's `OAuthHandler`
  itself does not validate an `iss` parameter on the callback either, so this remains open exactly
  as the plan doc scoped it.
- 403 `insufficient_scope` step-up re-authorization has no handling; `Manager`/`Login` only cover
  the initial "no token" or "refresh failed" paths surfaced via
  `mcpclient.IsAuthorizationRequired`. A 403 with `insufficient_scope` currently surfaces as a
  generic tool/operation error, not a re-triggerable login prompt.
- DCR persistence edge cases: `PersistClientRegistration` (Phase 2) persists whatever
  `client_id`/`client_secret` the handler ends up holding after `Login` returns, but there is no
  handling yet for a registered client's `client_secret_expires_at` (captured in `ClientInfo` but
  never checked) — a client whose registration has expired at the authorization server will
  currently just fail opaquely on next use rather than triggering re-registration.
- `Manager.Login`'s `ManualCode` bare-code trust argument (no external network path can inject a
  value there) should be re-examined if a future phase adds a *remote* manual-code relay (e.g. a
  WebUI dialog that receives the code over the network rather than the user typing it into the
  same local process) — that would reintroduce a real CSRF surface the current code path
  deliberately doesn't have.
- `IsInteractive`/`NonInteractiveLoginError` exist but have no call site yet: the agent layer
  currently only *warns* (`PublishWarn`) rather than blocking with this specific error. A future
  phase (or the TUI/WebUI login surfaces) should decide where a hard non-interactive guard belongs.

Related: [[pando-mcp-client-authentication-oauth-plan]], [[pando/features/mcp_client_auth_phase2_oauth_storage.md]], [[pando/features/mcp_client_auth_phase1_static.md]]