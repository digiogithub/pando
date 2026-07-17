---
created_at: 2026-07-17T11:54:49.451213538Z
updated_at: 2026-07-17T12:53:47.402020647Z
tags:
    - feature
    - security
    - webui
    - api
    - config
---
# Feature: WebUI Access — Basic Auth for the HTTP API (2026-07-17)

## Motivation

`pando serve` / `pando app` expose the whole agent over HTTP (bash tool, PTY terminal,
file writes, config). Before this change, a non-localhost bind was effectively remote code
execution for the entire network:

- `internal/api/server.go` — `authMiddleware` let `/api/v1/token` through with **no auth**:
  anyone reachable to the port could ask for the token and get full access.
- `generateToken()` was `b[i] = chars[i%len(chars)]` — a **constant** string, identical on
  every install, so the "token" protected nothing.
- `config.APIServerConfig.RequireAuth` existed but was **never read** by the API server.

User request: a WebUI Access settings panel to enable/disable username+password protection,
with the passwords encrypted in the `.toml`.

## Decisions

- **Password storage**: reuse the AGE machinery in `internal/config/agecrypto.go`
  (`encryptSecretString`/`decryptSecretString`, `age1:` prefix, keys in `~/.config/pando/keys/<set>/`),
  same treatment as provider API keys. Reversible by design so the panel can reveal a password;
  the in-memory `Config` holds plaintext after `Load` (`decryptSensitiveConfigFields`), so the
  middleware compares plaintext with `subtle.ConstantTimeCompare`. (User chose AGE over bcrypt.)
- **Scope**: `/api/` only. Static assets and `index.html` stay public — the workbox precache
  (`web-ui/vite.config.ts`) fetches them without credentials and install is atomic, so a 401 on
  an asset would break the service worker permanently.
- **Activation — bind host only**: enforced when the server is started on a **non-loopback host**
  (`--host 0.0.0.0`). Bound to localhost the setting stays inert.
  **The request's origin IP is deliberately NOT considered.** The first implementation also
  exempted loopback *requests*, which looked like a bug in practice: `pando app --host 0.0.0.0`
  opens a local browser, so the user got no prompt at all despite having enabled the feature.
  It was also weaker — the port is open to the network either way, so anything able to reach
  127.0.0.1 walked past the gate. Do not re-add a remote-IP exemption.
- **Token is the session credential**: `basicAuthMiddleware` passes a request that already carries
  a valid API token. The gate protects *obtaining* the token (`/api/v1/token`). Without this the
  SPA would break after login (it only sends `X-Pando-Token`) and SSE streams would be impossible
  (EventSource cannot set an `Authorization` header — it only passes `?token=`).
- **No native browser prompt**: the SPA sends `X-Pando-Client: web`; the server then omits
  `WWW-Authenticate` and returns `{"error":"basic_auth_required"}` so the React login dialog
  renders instead. CLI clients (no header) still get the standard `Basic realm="Pando"` challenge.

## Changes

### Config
- `internal/config/config.go`: `APIServerConfig.BasicAuth BasicAuthConfig{Enabled, Users []BasicAuthUser{Username, Password}}`,
  with `toml` tags. Serialises as `[Server.BasicAuth]` + `[[Server.BasicAuth.Users]]` — the first
  array-of-tables in the config tree.
- `internal/config/agecrypto.go`: encrypt/decrypt `Server.BasicAuth.Users[i].Password` in
  `encryptSensitiveConfigFields` (clones the slice — never mutates the in-memory config the
  middleware authenticates against) and `decryptSensitiveConfigFields`.

### API
- **new** `internal/api/basicauth.go`: `isLoopbackHost`, `basicAuthEnforced`,
  `basicAuthCredentialsValid` (no early return — wrong user and wrong password cost the same),
  `basicAuthMiddleware`. Reads `config.Get()` per request, so the panel toggle applies live with
  no restart. NOTE: `isLoopbackHost` **replaced** a weaker duplicate that lived in
  `handlers_terminal_pty.go` (it only matched the three literals `localhost`/`127.0.0.1`/`::1`;
  the new one parses the IP and covers all of `127.0.0.0/8`).
- `internal/api/server.go`: middleware wired as `cors(ui(basicAuth(auth(mux))))`; `generateToken()`
  → `crypto/rand` + hex (returns an error now); new `hasValidToken(r)` helper shared by both
  middlewares, using `subtle.ConstantTimeCompare`; `Authorization` + `X-Pando-Client` added to
  `Access-Control-Allow-Headers`.
- **new** `internal/api/handlers_basicauth.go` + routes: `GET|PUT /api/v1/config/api-server/basic-auth`
  (PUT rejects enabling with zero users — lockout guard), `POST .../users` (upsert),
  `DELETE .../users/{username}` (auto-disables on removing the last user), `POST .../users/{username}/reveal`.
  `maskBasicAuthPasswords` for GET responses and `preserveBasicAuth` for the services PUT:
  **the generic services payload never writes credentials** — it carries masked passwords and older
  clients omit the block entirely, so writing it back would wipe every user.

### WebUI
- **new** `web-ui/src/components/settings/WebUIAccessSettings.tsx` — status banner (enforced vs inert
  because bound to localhost), enable toggle, user table with add/delete/reveal. Registered as the
  `webui-access` category in `SettingsView.tsx` under Services.
- **new** `web-ui/src/components/auth/LoginDialog.tsx` + `authenticateWithCredentials` in
  `services/auth.ts` (UTF-8 safe base64 — plain `btoa` throws on non-Latin-1 passwords).
- `services/api.ts`: always sends `X-Pando-Client: web`; new `BasicAuthRequiredError` thrown on a
  401 whose body says `basic_auth_required`, instead of the token-wipe + `window.location.reload()`
  path (which would loop forever, since no token is obtainable without credentials).
- `App.tsx`: catches `BasicAuthRequiredError` during init and renders `LoginDialog`.
- i18n: `settings.categories.webuiAccess`, `settings.webuiAccess.*`, `login.*` in all 7 locales.

### TUI / templates / docs
- `internal/tui/page/settings.go`: `server.basicAuth.enabled` toggle + read-only username summary
  (`basicAuthUsersSummary`); `saveServer` refuses to enable with no users. There is no list field
  type in the TUI, so user management stays in the Web UI.
- `internal/config/init.go` + `cmd/init.go`: commented `[Server.BasicAuth]` block in both templates.
- `README.md`: "WebUI Access (protecting a remotely exposed server)" section under Usage.

## Verification

- `go build ./...`, `go vet`, `npx tsc --noEmit`, `npx eslint` — all clean.
- `go test ./internal/api ./internal/config` — pass. New tests:
  - `internal/api/basicauth_test.go`: remote 401 / valid creds 200 / wrong password 401; challenge
    suppressed for `X-Pando-Client: web`; **local requests on an exposed server are challenged**
    (`TestBasicAuthChallengesLocalRequestsOnExposedServer`); loopback bind exempt for
    `localhost`/`127.0.0.1`/`::1`; `/health` and static paths public; inert when disabled or
    user-less; `generateToken` randomness; `preserveBasicAuth` / `maskBasicAuthPasswords`; plus
    `TestTokenEndpointIsGatedEndToEnd` driving the real middleware stack.
  - `internal/config/basicauth_secrets_test.go`: AGE round-trip (incl. non-ASCII password) and
    `TestUpdateServerPersistsBasicAuthUsers` asserting the real write path emits
    `[[Server.BasicAuth.Users]]` with `age1:` and never plaintext.
- **Manual, real binary** (`pando serve --host 0.0.0.0`, curl over the LAN IP): remote no-creds 401
  with `www-authenticate: Basic realm="Pando"`; wrong creds 401; `-u admin:pass` returns the token;
  token alone then 200; bogus token 401; `X-Pando-Client: web` returns 401
  `{"error":"basic_auth_required"}` with no challenge header; `/health` 200. Panel endpoints
  exercised: status/add/reveal/delete, deleted user immediately 401, and the on-disk `.toml` showed
  `Password = 'age1:...'` for every user (the plaintext seed password was auto-encrypted on first
  start).
- **Re-verified after the activation-rule fix**: on `--host 0.0.0.0`, `https://127.0.0.1/api/v1/token`
  now returns 401 `basic_auth_required` and 200 with `-u admin:pass`; on `--host localhost` the same
  call returns 200 with no credentials.
