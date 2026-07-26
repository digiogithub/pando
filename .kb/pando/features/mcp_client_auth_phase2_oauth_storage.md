---
created_at: 2026-07-27T04:31:34.245070968Z
updated_at: 2026-07-27T04:31:34.245070968Z
tags:
    - feature
    - mcp
    - auth
    - oauth
---
# Feature: MCP client OAuth credential storage + manager (Phase 2)

Status: COMPLETE (2026-07-27). Implements Phase 2 of [[pando-mcp-client-authentication-oauth-plan]] — persistent OAuth 2.1 credential storage and a process-wide manager, wired into the MCP client transport. No interactive browser/callback flow yet (Phase 3).

## What changed

New package `internal/mcpauth`:

- `store.go` — `Store`: file-backed (`config.GlobalConfigDir()/mcp-auth.json`, override via `PANDO_MCP_AUTH_FILE`), 0700 dir / 0600 file, atomic write (temp file + `os.Rename`), degrades to an empty store on missing/corrupt file. Cross-process locking via a simple `<file>.lock` `O_CREATE|O_EXCL` retry loop with a 2s stale-lock timeout (the repo's existing `internal/ipc` flock helper is tied to its own `LockInfo` JSON schema, so a fresh, simpler mechanism was used instead, as the phase spec allowed). API: `New`, `DefaultStore`, `All`, `Get`, `GetForURL` (invalidates credentials when the server's URL changed, opencode's `getForUrl` pattern), `Set`, `UpdateTokens`, `UpdateClientInfo`, `Remove`, `ModTime`.
- `tokenstore.go` — `ServerTokenStore` implements `transport.TokenStore` for one server name, converting `transport.Token` <-> the persisted `Tokens` type, computing `ExpiresAt` from `ExpiresIn` when absent, returning `transport.ErrNoToken` when no token or URL mismatch.
- `manager.go` — `Manager` (singleton via `mcpauth.Default()`, or `NewManager(store)` for tests): `OAuthConfig(serverName, srv)` builds `transport.OAuthConfig` from the already-decrypted config plus persisted `ClientInfo` (config values win over persisted DCR results), `PKCEEnabled` always true, redirect URI defaults to `http://127.0.0.1:19876/mcp/oauth/callback` (`DefaultCallbackPort`/`DefaultCallbackPath` consts, overridable via `Auth.OAuth.CallbackPort`/`RedirectURI`); `InvalidateIfDiskChanged` drops cached state when the store's mtime moved (multi-process pickup); `PersistClientRegistration` closes mcp-go's DCR-persistence gap by saving `h.GetClientID()`/`GetClientSecret()`; `Logout`, `HasTokens`; `Do401` dedupes concurrent 401-recovery attempts keyed by `(serverName, staleToken)` (hermes' `pending_401` pattern). Exported `ErrAuthorizationRequired` + `IsAuthorizationRequired` (also recognises `client.IsOAuthAuthorizationRequiredError` and `transport.ErrOAuthAuthorizationRequired`).

`internal/mcpclient/client.go`: both `// TODO(phase2)` markers resolved — `MCPSse`/`MCPStreamableHTTP` now call `client.NewOAuthSSEClient`/`client.NewOAuthStreamableHttpClient` with a `transport.OAuthConfig` from `mcpauth.Default().OAuthConfig(...)` when `resolved.Auth.IsOAuth()`, still passing static headers through `client.WithHeaders`/`transport.WithHTTPHeaders`. Added exported `mcpclient.IsAuthorizationRequired(err)` delegating to `mcpauth`. stdio + oauth remains unsupported (warns, unchanged from phase 1). `internal/config/mcp_auth.go`'s stale phase-1 TODO comment on `AuthHeaders` was updated to point at the now-implemented OAuth transport path.

## Files touched

- `internal/mcpauth/store.go`, `tokenstore.go`, `manager.go` (new)
- `internal/mcpauth/store_test.go`, `tokenstore_test.go`, `manager_test.go` (new)
- `internal/mcpclient/client.go` (OAuth wiring + `IsAuthorizationRequired` helper)
- `internal/config/mcp_auth.go` (comment only)

## Why

mcp-go v0.57.0 provides the OAuth transport (PKCE, discovery, refresh, DCR) but has no persistence for dynamically-registered client credentials, and Pando needs a process-wide cache keyed by server name plus 401-dedup so the eventual interactive login flow (Phase 3) and background tool calls don't race each other.

## Verification

- `go build ./...` — clean
- `go vet ./internal/mcpauth/... ./internal/mcpclient/...` — clean
- `go test ./internal/mcpauth/... ./internal/mcpclient/... ./internal/config/...` — all pass
- `go test -race ./internal/mcpauth/...` — pass (covers `Store` concurrent writers and `Manager.Do401` dedup)
- `gofmt -l` on all touched packages — no output

## Deviations from spec

- Cross-process lock: used a bespoke `<file>.lock` sentinel-file retry loop rather than reusing `internal/ipc`'s flock helper, since that helper is coupled to its own `LockInfo` JSON schema and not meant for reuse; this matches the spec's explicit fallback instruction.
- `Metadata.Issuer` is defined now (per spec) but not yet populated anywhere — Phase 4 (iss validation, RFC 9207) is expected to fill it in.

## Phase 3 driving notes

Phase 3 (interactive browser flow + `pando mcp login/logout/status` + callback server) should:
1. Call `mcpauth.Default().OAuthConfig(serverName, resolvedServer)` to get the `transport.OAuthConfig`, then build a `*transport.OAuthHandler` via `transport.NewOAuthHandler(cfg)` (or obtain one from a failed call via `client.GetOAuthHandler(err)`).
2. Detect the need to log in via `mcpauth.IsAuthorizationRequired(err)` / `mcpclient.IsAuthorizationRequired(err)`.
3. Drive `GetAuthorizationURL` -> open browser -> local callback server on `Auth.OAuth.CallbackPort` (default `mcpauth.DefaultCallbackPort` at path `mcpauth.DefaultCallbackPath`) -> `ProcessAuthorizationResponse`.
4. After a successful flow (whether or not DCR happened), call `mcpauth.Default().PersistClientRegistration(serverName, serverURL, handler)` to persist the client_id/secret mcp-go ended up using.
5. `pando mcp logout` -> `mcpauth.Default().Logout(serverName)`. `pando mcp status` -> `mcpauth.Default().HasTokens(serverName, serverURL)` plus `Store().Get(serverName)` for details.

Related: [[pando-mcp-client-authentication-oauth-plan]], [[pando-mcp-client-auth-phase1-static]]