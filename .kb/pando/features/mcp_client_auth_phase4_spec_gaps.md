---
created_at: 2026-07-27T04:57:35.913590003Z
updated_at: 2026-07-27T04:57:35.913590003Z
---
# Feature: MCP client OAuth spec-gap closure (Phase 4)

Status: COMPLETE (2026-07-27). Implements Phase 4 of [[pando-mcp-client-authentication-oauth-plan]]:
closes three gaps mark3labs/mcp-go v0.57.0 does not cover on its own. Builds on
[[pando/features/mcp_client_auth_phase3_oauth_flow.md]], [[pando/features/mcp_client_auth_phase2_oauth_storage.md]],
[[pando/features/mcp_client_auth_phase1_static.md]].

## 1. RFC 9207 `iss` validation

- New `internal/mcpauth/issuer.go`: `validateIssuer(expected, got string, supported bool) error` — pure,
  exhaustively table-tested function implementing the 4-case truth table (supported×present). Comparison is
  RFC 3986 §6.2.1 exact string equality: no case folding, no port elision, no trailing-slash/percent-encoding
  normalization.
- mcp-go's `transport.AuthServerMetadata` (RFC 8414 struct) does NOT expose
  `authorization_response_iss_parameter_supported` (verified against the vendored source). Workaround:
  `fetchIssSupport` re-fetches the AS metadata document itself (second network round trip) and decodes just
  that one field, using `issDiscoveryCandidateURLs` — a duplicate of mcp-go's unexported
  `authorizationServerMetadataURLs` (same RFC 8414 path-insertion + OIDC fallback order) since mcp-go doesn't
  export it.
- `Manager.Login` now: calls `handler.GetServerMetadata` before redirecting to record `Metadata.Issuer` +
  `Metadata.AuthorizationResponseIssParameterSupported` via new `Store.UpdateMetadata`; validates `iss`
  (captured from the callback) against it BEFORE `ProcessAuthorizationResponse`, aborting with a "possible
  mix-up attack" error on mismatch.
- `CallbackServer.Wait` signature changed: `(code, iss string, err error)` (was `(code string, err error)`).
  All callers (login.go, callback_test.go) updated.
- Related fix in `handleCallback`: the `error`/`error_description` branch used to run BEFORE the state check,
  so a state-less/wrong-state request carrying `?error=...` could resolve (with an error) a legitimate
  pending `Wait` without knowing the real state — reordered so state is validated first, matching the intent
  that a response failing basic origin/CSRF binding must never be acted on at all.
- Known non-conformant path: the `LoginPrompt.ManualCode` fallback (headless/remote paste-a-code flow) has no
  channel to surface `iss` — its signature is unchanged (`(code, state string, err error)`) per the
  "keep every existing exported signature working" constraint. `gotIss` is empty for that path; if the AS
  declares `authorization_response_iss_parameter_supported=true` this correctly rejects (per the table), so a
  server with that flag on cannot be logged into via ManualCode today.

## 2. 403 `insufficient_scope` step-up

- `internal/mcpauth/challenge.go`: `ParseChallenge(header string) Challenge` / `ParseChallenges(headers []string) Challenge`
  parse a `WWW-Authenticate` value into `{Scheme, Error, ErrorDescription, Scope []string, ResourceMetadata}`,
  handling multiple challenges per line and across lines (prefers the Bearer challenge), quoted values with
  escapes, and comma-inside-quoted-value.
- `internal/mcpauth/stepup.go`: `ErrInsufficientScope{ServerName, Scopes}` (+ `IsInsufficientScope`),
  `Manager.RecordGrantedScopes`, `Manager.StepUpScopes` (order-preserving, deduped union of previously
  requested ∪ challenged — spec requires accumulation, not replacement), `Manager.HandleInsufficientScope`
  (accumulates, caps at `maxStepUpAttempts=2` per server+scope-set key, then returns a terminal error instead
  of looping forever). New `Entry.RequestedScopes []string` field + `Store.UpdateRequestedScopes`.
  `Manager.Login` now seeds `oauthCfg.Scopes` from `StepUpScopes` and calls `RecordGrantedScopes` on success.
- **mcp-go limitation worked around**: mcp-go v0.57.0's generic non-2xx request-error path (verified in
  `client/transport/streamable_http.go` `SendRequest`) only preserves response headers for HTTP 401 — a 403 (or
  any other non-401 4xx/5xx) just returns `fmt.Errorf("request failed with status %d: %s", ...)`, discarding
  `resp.Header` (and thus any `WWW-Authenticate` challenge) entirely, with no exported hook to observe the raw
  response. Also, `OAuthConfig.HTTPClient` is a *separate* client instance used only by `OAuthHandler`'s own
  metadata/token/refresh requests — it does NOT back the actual tool-call HTTP requests (those go through
  `sc.httpClient` / the SSE client's own client, configurable only via `transport.WithHTTPBasicClient` /
  `transport.WithHTTPClient`). Fix: new `internal/mcpauth/scopeinterceptor.go` `ScopeCapturingTransport`
  (an `http.RoundTripper`) is installed as that actual tool-call HTTP client in `internal/mcpclient/client.go`'s
  two OAuth branches (SSE + StreamableHTTP), recording the last 403 challenge so it can be recovered after the
  call errors out.
- `internal/mcpclient/client.go`: new `authAwareClient` wraps the OAuth client's `Initialize/ListTools/CallTool`,
  checks `interceptor.LastChallenge()` after any error, and on `error=="insufficient_scope"` replaces/joins the
  error with `mcpauth.Manager.HandleInsufficientScope(...)` (via `errors.Join`, so `errors.As` still finds
  `*ErrInsufficientScope`).
- `internal/llm/agent/mcp-tools.go`: `mcpOperationError` and `getTools`'s Initialize/ListTools error paths now
  check `mcpauth.IsInsufficientScope` first and surface `"MCP server %q needs additional permissions
  (scope: …). Run: pando mcp login %s"` instead of an opaque failure.

## 3. Dynamic client registration robustness

- `ClientInfo.SecretExpiresAt` (unix seconds, 0 = never) is now checked: new `clientInfoExpired` helper;
  `Manager.OAuthConfig` treats an entry whose secret has expired as wholly absent (both ClientID and
  ClientSecret), so a fresh `RegisterClient` runs instead of a confidential-client token exchange failing
  opaquely.
  - Caveat: mcp-go's own `OAuthHandler.RegisterClient` does not parse `client_secret_expires_at` from the
    registration response at all (verified in `client/transport/oauth.go`), so `PersistClientRegistration`
    still never populates this field today — the check is now correctly wired for whenever the field *does*
    get set (future capture path, manual store edits), but nothing currently sets it via the normal DCR flow.
- New `Manager.InvalidateClientRegistration(serverName string) error`: drops persisted `ClientInfo` (keeps
  tokens/requested-scopes) and the cached in-memory state.
- Detection wired in two places: `Manager.Login`'s `ProcessAuthorizationResponse` error path, and
  `mcpclient.authAwareClient.recoverAuthError` (covers `invalid_client` surfacing later, e.g. during a
  transparent refresh inside an ongoing tool call) — both check `errors.As(err, &transport.OAuthError{})` for
  `ErrorCode=="invalid_client"` and only invalidate when the server's config does NOT pin a static
  `Auth.OAuth.ClientID` (a config-pinned value is never "stale" in this sense).

## Files touched

- `internal/mcpauth/store.go` — `Metadata.AuthorizationResponseIssParameterSupported`, `Entry.RequestedScopes`,
  `Store.UpdateMetadata`, `Store.UpdateRequestedScopes`.
- `internal/mcpauth/manager.go` — expired-secret check in `OAuthConfig`, `InvalidateClientRegistration`,
  `stepUp stepUpState` field.
- `internal/mcpauth/callback.go` — `Wait` returns `(code, iss, err)`; `handleCallback` reordered + captures `iss`.
- `internal/mcpauth/login.go` — metadata fetch/store before redirect, iss validation, invalid_client handling,
  scope accumulation.
- New: `internal/mcpauth/issuer.go`, `challenge.go`, `stepup.go`, `scopeinterceptor.go`.
- `internal/mcpclient/client.go` — `authAwareClient`, `hasStaticClientID`, interceptor wiring for both OAuth
  transports.
- `internal/llm/agent/mcp-tools.go` — surfaces `ErrInsufficientScope`.
- New tests: `issuer_test.go`, `challenge_test.go`, `stepup_test.go`, `dcr_test.go`, `login_iss_test.go`; updated
  `callback_test.go` (new `Wait` signature + iss-capture + error-ordering tests).

## Verification

`go build ./...` clean. `go vet ./internal/mcpauth/... ./internal/mcpclient/... ./internal/llm/agent/... ./cmd/...`
clean (one pre-existing, unrelated vet warning in `internal/mesnada/agent/spawner_template.go`, not touched by
this phase). `go test ./internal/mcpauth/... ./internal/mcpclient/... ./internal/llm/agent/... ./internal/config/... ./cmd/...`
all pass. `go test -race ./internal/mcpauth/...` passes. All new tests use `New(t.TempDir()/...)` stores
(same isolation pattern the existing Phase 2/3 tests already use, never touching the real global config dir).

Related: [[pando-mcp-client-authentication-oauth-plan]], [[pando/features/mcp_client_auth_phase3_oauth_flow.md]],
[[pando/features/mcp_client_auth_phase2_oauth_storage.md]], [[pando/features/mcp_client_auth_phase1_static.md]]