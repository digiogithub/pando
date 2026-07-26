---
created_at: 2026-07-27T05:11:34.230729877Z
updated_at: 2026-07-27T05:11:34.230729877Z
---
# MCP client auth — Phase 5: mTLS / TLS options + OAuth client_credentials grant

Builds on [[pando/plans/mcp_client_authentication_oauth_plan.md]] and phases 1-4
([[pando/features/mcp_client_auth_phase1_static.md]],
[[pando/features/mcp_client_auth_phase2_oauth_storage.md]],
[[pando/features/mcp_client_auth_phase3_oauth_flow.md]],
[[pando/features/mcp_client_auth_phase4_spec_gaps.md]]). Implemented by a
subagent; internal/api, internal/tui and web/ were off-limits (another agent
was editing them concurrently) and were not touched.

## What changed

### 1. TLS / mutual-TLS config (internal/config/mcp_auth.go)
New fields on `MCPAuth` (all optional, independent of `Type`):
- `ClientCert string` (toml `ClientCert`)
- `ClientKey string` (toml `ClientKey`)
- `ClientKeyPassword string` (toml `ClientKeyPassword`) — AGE-encrypted at
  rest like `Token`/`Password` (internal/config/agecrypto.go:
  `ResolveMCPServerSecrets` decrypts it, `encryptSensitiveConfigFields`
  encrypts it).
- `CACert string` (toml `CACert`)
- `SkipTLSVerify bool` (toml `SkipTLSVerify`)

`MCPAuth.Validate()` now also runs `validateTLS()`: `ClientCert` and
`ClientKey` must both be set or both empty. Paths support `~` and env-var
expansion via `internal/mcpauth.expandPath` (unexported, lives next to
`BuildTLSConfig` since that's its only consumer).

### 2. TLS config builder (internal/mcpauth/tls.go) — new file
- `BuildTLSConfig(srv config.MCPServer) (*tls.Config, error)` — returns
  `(nil, nil)` when no TLS fields are set (the common case). Loads the
  client keypair (`tls.X509KeyPair`), decrypting the key first via
  `decryptPEMKey` when `ClientKeyPassword` is set. Loads `CACert` into a
  `RootCAs` pool. Sets `InsecureSkipVerify` + `logging.Warn` for
  `SkipTLSVerify`.
- Deliberately placed in **internal/mcpauth, not internal/mcpclient**: this
  lets `internal/mcpauth/login.go` (interactive OAuth) use it too without an
  import cycle, since `internal/mcpclient` already imports
  `internal/mcpauth`.
- Encrypted key support: legacy RFC 1423 PEM (`x509.IsEncryptedPEMBlock` /
  `DecryptPEMBlock`, deprecated but still present in go1.26) is decrypted
  transparently. PKCS#8 `ENCRYPTED PRIVATE KEY` (PBES2, what
  `openssl genpkey`/`pkcs8` produce by default) is **not** supported by Go's
  stdlib — `decryptPEMKey` detects this block type and returns an actionable
  error telling the operator to `openssl pkcs8 -in key.pem -out
  key-decrypted.pem` first. This is a deliberate spec/stdlib gap, not a bug.
- `TransportForTLS(tlsCfg *tls.Config) http.RoundTripper` — clones
  `http.DefaultTransport` and sets `TLSClientConfig`; returns `nil` for a
  nil `tlsCfg` (callers treat nil as "use the library default").
- `HTTPClientForTLS(tlsCfg *tls.Config, timeout time.Duration) *http.Client`.

Wired into `internal/mcpclient/client.go` New(): for both `MCPSse` and
`MCPStreamableHTTP`, `tlsCfg`/`baseTransport` are built once per server and
applied to all three sub-branches (static/none, interactive oauth,
client_credentials) — including the OAuth handler's own metadata/token
requests via `oauthCfg.HTTPClient`. `ScopeCapturingTransport` continues to
wrap (not replace) the TLS transport (`NewScopeCapturingTransport(nil)` was
already nil-safe, matching `TransportForTLS`'s nil-for-no-TLS contract).
Also wired into `internal/mcpauth/login.go` (interactive flow) so mTLS +
interactive OAuth combine correctly. Stdio servers are unaffected (no HTTP
transport).

### 3. OAuth `client_credentials` grant (internal/mcpauth/clientcredentials.go — new file)
- New `config.MCPAuthOAuthClientCredentials MCPAuthType =
  "oauth_client_credentials"`. Requires `Auth.OAuth.ClientID` +
  `ClientSecret` (checked in `Validate()`).
- `MCPAuth.IsOAuth()` now returns true for **both** `MCPAuthOAuth` and
  `MCPAuthOAuthClientCredentials`. A new `MCPAuth.IsInteractiveOAuth()`
  returns true only for `MCPAuthOAuth`. Call-site audit:
  - `internal/mcpauth/login.go` `Login()` (interactive flow entry guard) →
    switched to `IsInteractiveOAuth()`, with a pointed error message for
    client_credentials servers directing them to the non-interactive path.
  - `internal/mcpauth/manager.go` `OAuthConfig()` (builds the
    interactive-flow `transport.OAuthConfig`) → switched to
    `IsInteractiveOAuth()`; client_credentials never reaches this function.
  - `internal/mcpauth/login.go` `Status()`, `internal/mcpclient/client.go`,
    `cmd/mcp.go` (`authStatusLabel`, `mcpStatusCmd`) → left on `IsOAuth()`
    since both variants need the same "has this server been authorized"
    treatment.
- **Design decision on how the token reaches the transport**: mcp-go
  v0.57.0's `OAuthHandler.getValidToken` only ever refreshes via a stored
  `refresh_token`, otherwise returns `ErrOAuthAuthorizationRequired` — and
  `client_credentials` has no refresh token (RFC 6749 §4.4), so reusing
  `client.NewOAuthSSEClient`/`NewOAuthStreamableHttpClient` would make a
  token expiring mid-session unrecoverable without a spurious
  "please re-authorize" error. Instead: a `clientCredentialsTokenSource`
  (mints/re-mints on demand, cached through the existing `Store`) plus a
  `bearerRoundTripper` (attaches `Authorization: Bearer …`, wraps the
  TLS-configured base transport) are used with the **plain**
  `client.NewSSEMCPClient`/`NewStreamableHttpClient` constructors — not
  their OAuth-aware counterparts. `Manager.ClientCredentialsTransport(ctx,
  serverName, resolvedSrv, base http.RoundTripper) (http.RoundTripper,
  error)` is the single entry point `internal/mcpclient` calls.
- `Manager.LoginClientCredentials(ctx, serverName, srv) error` — the
  non-interactive equivalent of `Login()`: resolves secrets, discovers the
  token endpoint via `transport.OAuthHandler.GetServerMetadata` (RFC 8414,
  reusing mcp-go's discovery rather than reimplementing it), mints a token
  via `mintClientCredentialsToken` (client_secret_post: `grant_type`,
  `client_id`, `client_secret`, space-joined `scope`, RFC 8707 `resource`),
  and persists it via the existing `Store.UpdateTokens`.
- `CanonicalResourceURI(raw string) (string, error)` — RFC 8707 resource
  identifier: lowercase scheme+host, default ports (80/443) stripped,
  fragment and query dropped, trailing slash dropped except on a bare root
  path. Table-tested in `clientcredentials_test.go`.
- `cmd/mcp.go`: `pando mcp login <name>` now checks
  `srv.Auth.ResolvedType() == config.MCPAuthOAuthClientCredentials` first
  and dispatches to `LoginClientCredentials` (no browser flags used);
  `printStatus` now shows the token block for both OAuth variants.

## Files touched
- internal/config/mcp_auth.go, internal/config/agecrypto.go (+ existing
  internal/config/mcp_auth_test.go extended)
- internal/mcpauth/tls.go (new), internal/mcpauth/tls_test.go (new)
- internal/mcpauth/clientcredentials.go (new),
  internal/mcpauth/clientcredentials_test.go (new)
- internal/mcpauth/login.go (IsInteractiveOAuth guard + TLS wiring into
  `oauthCfg.HTTPClient`)
- internal/mcpauth/manager.go (`OAuthConfig` guard tightened to
  `IsInteractiveOAuth`)
- internal/mcpclient/client.go (TLS + client_credentials branches for SSE
  and streamable-http)
- cmd/mcp.go (login dispatch, status label)

Not touched (owned by a concurrent agent): internal/api, internal/tui,
web-ui/.

## Verification
- `go build ./...` — clean.
- `go vet ./internal/config/... ./internal/mcpauth/... ./internal/mcpclient/... ./cmd/...` — clean.
- `go test ./internal/config/... ./internal/mcpauth/... ./internal/mcpclient/... ./cmd/...` — all pass (mcpclient has no test files, unchanged from before).
- `go test -race ./internal/mcpauth/...` — pass.
- New tests: TLS load/missing-file/CA-pool/SkipTLSVerify/encrypted-key
  (legacy RFC1423 decrypt + wrong-password + PKCS#8-actionable-error) +
  an httptest mutual-TLS end-to-end (`TestBuildTLSConfig_EndToEndMutualTLS`);
  client_credentials mint/persist/re-mint-on-expiry/401-invalid_client +
  `CanonicalResourceURI` table test + a full RoundTripper-chain end-to-end
  (`TestClientCredentialsTransport_EndToEnd`); config-level `Validate`/
  `IsOAuth`/`IsInteractiveOAuth`/`AuthHeaders` cases for the new type and TLS
  fields.

## Known MCP-spec deviations / notes
- PKCS#8 `ENCRYPTED PRIVATE KEY` (PBES2) client keys are not decryptable by
  Go's stdlib; documented workaround is decrypting out-of-band with openssl.
- `client_credentials` token re-mint has no server-side signal to
  distinguish "expired, mint a new one" from "revoked" — a re-mint is always
  attempted; a real `invalid_client`/`invalid_grant` from the AS surfaces as
  a wrapped `transport.OAuthError`-shaped message from
  `extractClientCredentialsError`.
