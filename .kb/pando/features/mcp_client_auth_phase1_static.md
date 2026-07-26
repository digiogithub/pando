---
created_at: 2026-07-27T04:24:40.816092312Z
updated_at: 2026-07-27T04:24:40.816092312Z
tags:
    - feature
    - mcp
    - auth
    - config
---
# Feature: MCP client static authentication (Phase 1)

Status: COMPLETE (2026-07-27). Implements Phase 1 of [[pando-mcp-client-authentication-oauth-plan]] (declarative, non-OAuth auth). Phase 2 (OAuth 2.1 flow) is a separate follow-up.

## What changed

New declarative `Auth` block on `config.MCPServer`, consumed by `internal/mcpclient/client.go` for SSE and streamable-HTTP transports.

### New types — `internal/config/mcp_auth.go` (new file)

```go
type MCPAuthType string
const (
    MCPAuthNone   MCPAuthType = "none"
    MCPAuthBearer MCPAuthType = "bearer"
    MCPAuthBasic  MCPAuthType = "basic"
    MCPAuthHeader MCPAuthType = "header"
    MCPAuthOAuth  MCPAuthType = "oauth" // declared now, phase 2 implements it
)

type MCPOAuthConfig struct {
    ClientID, ClientSecret, RedirectURI, AuthServerMetadataURL string
    Scopes       []string
    CallbackPort int
}

type MCPAuth struct {
    Type       MCPAuthType
    Token      string          // bearer token / api key value
    Username   string          // basic
    Password   string          // basic
    HeaderName string          // for type=header, default "Authorization"
    OAuth      *MCPOAuthConfig
}

func (a *MCPAuth) ResolvedType() MCPAuthType // nil-safe, empty Type -> MCPAuthNone
func (a *MCPAuth) IsOAuth() bool
func (a *MCPAuth) Validate() error
func (s MCPServer) AuthHeaders() (map[string]string, error) // merges s.Headers + derived auth header; explicit Headers entries win
func cloneMCPAuth(a *MCPAuth) *MCPAuth // deep copy incl. nested OAuth, used by UpdateMCPServer
```

`config.MCPServer` gained: `Auth *MCPAuth` (json/toml/yaml `auth`/`Auth`/`auth`).

`AuthHeaders()` header derivation (operates on an already-decrypted server):
- `bearer` -> `Authorization: Bearer <Token>` (errors if Token empty)
- `basic` -> `Authorization: Basic base64(Username:Password)` (errors if Username empty)
- `header` -> `<HeaderName or "Authorization">: <Token>`, no prefix added (errors if Token empty)
- `none`/`oauth` -> just `s.Headers` unchanged (OAuth token injection deferred to phase 2)
- Explicit entries already in `s.Headers` always win over the derived header (documented in code so an operator can override without touching Auth).

### Secrets — `internal/config/agecrypto.go`

- `ResolveMCPServerSecrets`: now deep-clones `*MCPAuth`/`*MCPOAuthConfig` and decrypts `Auth.Token`, `Auth.Username`, `Auth.Password`, `Auth.OAuth.ClientID`, `Auth.OAuth.ClientSecret`. Never mutates the input server's nested pointers (verified by `TestResolveMCPServerSecretsWithAuth`).
- `encryptSensitiveConfigFields`: encrypts `Auth.Token`, `Auth.Password`, `Auth.OAuth.ClientSecret`. `Username`, `ClientID`, `HeaderName` are left in clear (not secrets).

### Client wiring — `internal/mcpclient/client.go::New()`

- SSE / streamable-HTTP branches now call `resolved.AuthHeaders()` instead of using `resolved.Headers` directly; error is wrapped as `fmt.Errorf("MCP server %s auth: %w", serverName, err)`.
- stdio branch ignores `Auth` (per MCP spec, stdio takes credentials from env) but logs `logging.Warn` if a non-`none` Auth type is configured on a stdio server.
- Both HTTP branches carry a `// TODO(phase2)` comment marking where OAuth-aware transports should be wired in.

### Config API — `internal/config/config.go`

- `UpdateMCPServer` now also clones/persists `Auth` via `cloneMCPAuth` (previously it would have silently dropped any Auth block on every server update through this path — fixed as part of keeping the new field first-class, not just decorative).
- No MCP-server validation loop existed in `Validate()` prior to this change, so `MCPAuth.Validate()` is exposed but intentionally NOT wired into `Validate()` per the phase-1 task scope — a future validate pass can call it per-server if desired.

## Backward compatibility

Every existing config without an `Auth` block behaves exactly as before: `Auth == nil` -> `ResolvedType() == MCPAuthNone` -> `AuthHeaders()` returns `s.Headers` unchanged.

## Verification

- `go build ./...` — OK
- `go vet ./internal/config/... ./internal/mcpclient/...` — OK (repo-wide `go vet ./...` shows one pre-existing, unrelated finding in `internal/mesnada/agent/spawner_template.go` about an unused cancel func — not touched by this change)
- `go test ./internal/config/... ./internal/mcpclient/...` — all pass. New tests: `internal/config/mcp_auth_test.go` (ResolvedType, IsOAuth, Validate table test, AuthHeaders table test incl. precedence/error cases, no-mutation-of-source-map test); extended `internal/config/agecrypto_test.go` (Auth+OAuth round-trip in `TestEncryptDecryptSensitiveConfigFields`, new `TestResolveMCPServerSecretsWithAuth` proving no in-place mutation of the original server).

## Notes for a Phase 2 implementer

- `MCPAuthOAuth` is a real enum value today but is a no-op in `AuthHeaders()` (falls into the `none` case) and in `client.go` (falls through to plain `NewSSEMCPClient`/`NewStreamableHttpClient` with only `s.Headers`). Both TODO(phase2) comments mark the exact lines to change.
- `MCPOAuthConfig` fields already exist and are already encrypted/decrypted end-to-end (`ClientSecret`), so phase 2 can read `resolved.Auth.OAuth` directly without further config-layer plumbing.
- `mesnada`/TUI/WebUI/CLI were intentionally NOT touched in this phase (out of scope) — server-side config editing UIs will need new form fields for `Auth.Type`/`Token`/`Username`/`Password`/`HeaderName` in a later phase.

Related: [[pando-mcp-client-authentication-oauth-plan]], [[pando-mcp-gateway-implementation]]