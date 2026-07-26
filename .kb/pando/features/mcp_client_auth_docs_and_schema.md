---
created_at: 2026-07-27T05:17:40.820546357Z
updated_at: 2026-07-27T05:17:40.820546357Z
tags:
    - docs
    - mcp
    - auth
    - schema
---
# Docs + JSON-schema for MCP client authentication

Status: COMPLETE. Documentation-only follow-up to phases 1-5 of
[[pando-mcp-client-authentication-oauth-plan]]
([[pando/features/mcp_client_auth_phase1_static.md]] .. phase5 docs). No runtime behaviour
was changed except the hand-written JSON Schema generator.

## What changed

1. **`cmd/schema/main.go`** — added the full `auth` object under
   `properties.mcpServers.additionalProperties.properties.auth`, matching the existing
   hand-written style (plain `map[string]any`, no reflection): `type` (enum
   none/bearer/basic/header/oauth/oauth_client_credentials, default "none"), `token`,
   `username`, `password`, `headerName`, nested `oauth` object (`clientID`, `clientSecret`,
   `scopes`, `redirectURI`, `callbackPort` default 19876, `authServerMetadataURL`),
   `clientCert`, `clientKey`, `clientKeyPassword`, `caCert`, `skipTLSVerify` (default
   false). Regenerated the checked-in `pando-schema.json` via
   `go run cmd/schema/main.go > pando-schema.json` and verified it with
   `python3 -m json.tool` and `jq .` (both pass). Note: there is also a stray *compiled
   binary* named `schema` (not `.json`) checked into the repo root from an earlier commit;
   `go build ./...` silently overwrites it as a build side effect (Go writes a binary named
   after the source dir for every main package built from repo root) — reverted with
   `git checkout -- schema` since it's unrelated build noise, not a generated artifact this
   task owns.

2. **`docs/mcp-authentication.md`** (new) — full reference: no-auth, bearer/basic/header,
   interactive `oauth` (zero-config, pre-registered client, scopes, custom callback
   port/redirect URI, AuthServerMetadataURL override), `oauth_client_credentials`, mTLS +
   SkipTLSVerify (with explicit insecurity warning), stdio servers (`Env`, `Auth` ignored),
   full `pando mcp list/status/login/logout` CLI reference incl. flags and
   headless/`--manual` workflow, an AGE encryption-at-rest table (which `MCPAuth` fields are
   encrypted vs plain), and a spec-conformance section crediting mcp-go v0.57.0 (PKCE
   discovery/DCR/refresh/RFC 8707 resource) vs Pando-specific work (RFC 9207 iss validation,
   403 step-up, DCR persistence across restarts, the loopback callback server, credential
   storage, mTLS, `oauth_client_credentials` end-to-end). Troubleshooting section covers:
   no DCR support, callback port in use, non-interactive/remote login, stale/invalid_client
   DCR registration, scope step-up loops, iss mismatch.

3. **`README.md`** — new "### MCP Server Authentication" subsection inserted between the
   existing "Data Directory and Legacy Database Migration" and "Language Servers (LSP)"
   subsections (README had no prior MCP-servers-configuration section to extend); short TOML
   snippet + the four `pando mcp` commands + link to docs/mcp-authentication.md.

4. AGENTS.md/CLAUDE.md have no docs/*.md index to update (checked; no such listing exists).

## Verification

- `go build ./...` — clean.
- `go test ./internal/config/... ./cmd/...` — all pass (cached config pkg + cmd pkg ok).
- `go run cmd/schema/main.go > pando-schema.json` then `python3 -m json.tool` and `jq .` —
  both parse without error; `auth` fields present at
  `properties.mcpServers.additionalProperties.properties.auth`.

## Feedback on the implementation (not fixed, reported only)

- `docs/acp-server.md`, linked from README.md line ~1018, does not exist under `docs/` — it
  actually lives at `.kb/acp/acp-server.md`. Pre-existing broken link, unrelated to this
  task; flagged for whoever owns the ACP docs.
- `MCPOAuthConfig.RedirectURI` and `CallbackPort` are both documented as configuring the
  loopback callback address but only apply to the interactive `oauth` flow; nothing in
  config-side godoc states they're a no-op for `oauth_client_credentials` — had to read
  `manager.go`/`clientcredentials.go` to confirm. A one-line doc comment on the struct fields
  would save the next reader the same trip.
- `pando mcp login <name> --manual`'s `ManualCode` path accepts an empty `gotState` (a bare
  pasted code has no state to compare) — this is deliberate and explained in `login.go`, but
  it means `--manual` mode gives up part of the CSRF state check that the HTTP callback path
  enforces. Worth a one-line caveat in end-user docs beyond what I already added, if the
  security posture of `--manual` is ever revisited.
- `internal/mcpauth/mcp-auth.json` credential store is NOT AGE-encrypted at rest (relies
  solely on `0600`/`0700` file permissions), which is a real asymmetry with how
  `MCPAuth.Token`/`Password`/`ClientKeyPassword`/`OAuth.ClientSecret` in `.pando.toml` *are*
  AGE-encrypted. Documented this clearly in the new docs page, but flagging it here too since
  it's a meaningful inconsistency in the security model an operator might not expect.