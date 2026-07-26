---
created_at: 2026-07-27T05:11:35.589391794Z
updated_at: 2026-07-27T05:11:35.589391794Z
---
# Feature: MCP client authentication — SURFACES phase (Phase 5)

Status: COMPLETE (2026-07-27). Exposes the existing config/storage/OAuth-flow/CLI layer
(Phases 1-4: `config.MCPAuth`, `internal/mcpauth`, `pando mcp login/logout/status`) through
the REST API, TUI settings, and WebUI settings. Config/auth model files
(`internal/config/mcp_auth.go`, `internal/config/agecrypto.go`, `internal/mcpauth/*`,
`internal/mcpclient/*`, `cmd/mcp.go`) were being edited concurrently by another agent adding
mTLS + `oauth_client_credentials`; none of those files were touched here.

## REST API (internal/api)

- Extended `MCPServerConfigItem` (handlers_config.go) with:
  - `Auth *MCPServerAuthItem` — mirrors `config.MCPAuth`; `Token`/`Password`/`OAuth.ClientSecret`
    are write-only (never populated on GET, only `HasToken`/`HasPassword`/`HasClientSecret`
    booleans are).
  - `AuthStatus *MCPServerAuthStatusItem` — computed live from `mcpauth.Default().Status(name, srv)`,
    only for `Auth.Type == "oauth"`.
- `buildMCPServerAuthItem`, `buildMCPServerAuthStatusItem`, `mergeMCPAuthRequest` (new helpers
  in handlers_config.go): `mergeMCPAuthRequest` folds an incoming PUT's auth block onto the
  previously-stored one so an empty Token/Password/OAuth.ClientSecret keeps what's on disk
  instead of wiping it (same convention as `handlePutConfigProviders`'s masked-APIKey check).
  `handlePutConfigMCPServer` now also validates the resulting `Auth` via `MCPAuth.Validate()`.
- New file `internal/api/handlers_mcp_auth.go` + routes in routes.go:
  - `POST /api/v1/mcp/{name}/login` — starts the OAuth 2.1 authorization-code+PKCE flow.
    Mirrors `handleCopilotLoginStart`'s start-then-poll pattern: runs `mcpauth.Default().Login`
    in a background goroutine (detached context, `mcpauth.DefaultCallbackTimeout+30s`), waits
    up to `mcpLoginWaitTimeout` (20s) on a channel fed by `LoginPrompt.OnAuthorizationURL`, then
    returns `{authorizationUrl, message}` immediately so the caller can open it while the local
    callback server keeps waiting in the background. 400 if the server's auth type isn't oauth,
    404 if unknown, 502/504 on early failure/timeout.
  - `POST /api/v1/mcp/{name}/logout` — `mcpauth.Default().Logout(name)`, `{message}`.
  - `GET /api/v1/mcp/{name}/status` — `mcpauth.Default().Status(name, srv)` as
    `MCPServerAuthStatusItem` (non-oauth servers get a minimal `{type}` body rather than 404/empty).
- Tests: `internal/api/handlers_mcp_auth_test.go` — DTO masking (`buildMCPServerAuthItem` never
  leaks Token/Password/ClientSecret), `mergeMCPAuthRequest` keep/replace/clear semantics, and
  handler-level GET/login/logout/status tests. Per the mandatory convention, tests that touch
  the auth store call `t.Setenv("PANDO_MCP_AUTH_FILE", <t.TempDir()>/mcp-auth.json)`; note
  `mcpauth.Default()`/`DefaultStore()` are process-wide `sync.Once` singletons (not editable —
  owned by the other agent's files), so only the first test in the binary to reach them actually
  picks the store path — tests were written with per-test unique server names so this doesn't
  cause cross-test interference.

## TUI (internal/tui/page/settings.go)

- `buildMCPServerAuthFields(name, server)` (new): adds an Auth Type selector + type-specific
  credential fields (token/username+password/header name/OAuth client id+secret+scopes+
  callback port) per MCP server, all keyed `mcpServers.<name>.auth*`, gated `Disabled` by the
  selected auth type. Secret fields show a masked placeholder (`••••••••`) when a value is
  already stored and empty otherwise; saving leaves them unchanged unless a real (non-masked,
  non-empty) value is typed (`secretFieldValue` helper, same convention as `providerAccounts`'s
  masked-APIKey handling).
- For oauth-type servers: a read-only Auth Status line (`mcpAuthStatusLabel`, same wording as
  `cmd/mcp.go`'s `authStatusLabel`: ok / expired / needs login) plus **actionable** Login/Logout
  `FieldAction` entries (`action:mcp_login:<name>` / `action:mcp_logout:<name>`), fully wired in
  `Update()` — this is the "prefer actionable" path from the task, not a fallback hint.
- `saveMCPServer` gained cases for `authType`/`authToken`/`authUsername`/`authPassword`/
  `authHeaderName`/`authOAuthClientID`/`authOAuthClientSecret`/`authOAuthScopes`/
  `authOAuthCallbackPort`, plus `ensureMCPAuth`/`ensureMCPOAuth` allocators and a final
  `server.Auth.Validate()` check.
- `loginMCPServer`/`logoutMCPServer` (new methods): `loginMCPServer` follows the exact shape of
  the pre-existing `antigravityLoginCommand` (`tea.Batch` of an immediate "opening browser..."
  info message + a blocking command) but delegates the whole authorization-code+PKCE+callback
  flow to `mcpauth.Default().Login` instead of reimplementing it (unlike Antigravity, which has
  its own hand-rolled callback server) — `OnAuthorizationURL` logs the URL via `internal/logging`
  as a fallback if the auto-opened browser fails. Both report through the existing
  `providerAccountActionMsg` type, which already rebuilds sections + shows a toast, so the status
  line refreshes automatically after login/logout.

## WebUI (web-ui/)

- `src/types/index.ts`: added `MCPAuthType`, `MCPServerOAuthConfig`, `MCPServerAuthConfig`,
  `MCPServerAuthStatus`, and `auth?`/`authStatus?` on `MCPServerConfig`.
- `src/stores/mcpServersStore.ts`: added `authBusy`, `loginServer`, `logoutServer`,
  `fetchAuthStatus`. `loginServer` POSTs `/api/v1/mcp/{name}/login`, opens
  `authorizationUrl` in a new tab (`window.open`), toasts the message, and re-fetches servers
  after a 5s delay to pick up the (background) completed status.
  `logoutServer` POSTs `/api/v1/mcp/{name}/logout` and refetches immediately.
- `src/components/settings/MCPServersSettings.tsx`: added an `AuthStatusBadge` (ok/expired/needs
  login/— for non-oauth), a new "Auth" status column in the servers table, Login/Re-authorize
  (+ Logout when tokens exist) buttons per oauth row, and — inside the add/edit modal, only for
  non-stdio servers (`isRemoteServer`, matching the spec's stdio-takes-env-creds rule) — an
  Authentication section: auth-type `SelectInput`, `MaskedInput` for token/password/client
  secret (using the existing `FormInput.tsx` `MaskedInput`/`SelectInput` shared components), and
  the OAuth client id/secret/scopes/callback-port fields.
- **i18n**: NOT added. `MCPServersSettings.tsx` (unlike some other settings screens) has no
  `useTranslation` usage at all — every existing string in that file, including the table
  headers, buttons, and modal labels, is hardcoded English. Only `settings.categories.mcpServers`
  (the sidebar label) is translated, which was untouched. Introducing i18n for just the new auth
  strings while the rest of the same file stays hardcoded would be an inconsistent, partial
  migration; following the file's actual existing convention exactly meant keeping the new
  strings hardcoded English too. Flagging this explicitly in case a later pass wants to migrate
  the whole file to i18n at once.
- Build verified: `bun run typecheck` (tsc -b, no errors), `bun run build` (vite build succeeds,
  only a pre-existing "chunk >500kB" advisory warning, no error), `bun run lint` (0 errors, the
  same 4 pre-existing `react-refresh/only-export-components` warnings in unrelated files).

## Verification

- `go build ./...` — clean (both before and after; no compile errors from the concurrently-edited
  mTLS/oauth_client_credentials files were observed at any point during this session).
- `go vet ./internal/api/... ./internal/tui/...` — clean. (Repo-wide `go vet ./...` reports one
  pre-existing, unrelated issue in `internal/mesnada/agent/spawner_template.go:82/91` — a cancel-
  var-not-used-on-all-paths — not touched by this change.)
- `go test ./internal/api/... ./internal/tui/... ./internal/config/... ./internal/mcpauth/...` — all pass.
- New Go tests: `internal/api/handlers_mcp_auth_test.go`.

## Left undone / follow-ups

- No SSE/streaming progress channel for the login flow beyond the existing
  poll-`GET .../status` pattern (matches the Copilot device-flow precedent already in this
  codebase; there was no existing SSE-per-login-flow pattern to reuse).
- WebUI i18n migration for `MCPServersSettings.tsx` as a whole (see i18n note above) — out of
  scope for a surfaces-only change that must "follow existing conventions exactly".
