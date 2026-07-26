# MCP Client Authentication

Pando can authenticate to remote MCP servers (`sse` and `streamable-http` transports) using
static credentials, mutual TLS, or OAuth 2.1 — including the full authorization-code +
PKCE flow with auto-discovery, dynamic client registration, and step-up re-authorization.

All of this is configured under `[MCPServers.<name>.Auth]` in `.pando.toml`. A server with
no `Auth` block behaves exactly as before authentication existed: no credentials are sent.

- [No auth (default)](#no-auth-default)
- [Static auth: bearer, basic, custom header](#static-auth-bearer-basic-custom-header)
- [OAuth 2.1 interactive flow](#oauth-21-interactive-flow-typeoauth)
- [OAuth 2.1 client_credentials (headless/CI)](#oauth-21-client_credentials-headlessci)
- [Mutual TLS and TLS options](#mutual-tls-and-tls-options)
- [Stdio servers](#stdio-servers)
- [CLI workflow](#cli-workflow-pando-mcp)
- [Encryption at rest](#encryption-at-rest)
- [Spec conformance](#spec-conformance)
- [Troubleshooting](#troubleshooting)

## No auth (default)

Unchanged from before authentication existed: omit `Auth` entirely, or leave `Type` unset /
`"none"`.

```toml
[MCPServers.example]
Type = "sse"
URL = "https://mcp.example.com/sse"
```

## Static auth: bearer, basic, custom header

### Bearer token

Sends `Authorization: Bearer <Token>`.

```toml
[MCPServers.example]
Type = "streamable-http"
URL = "https://mcp.example.com/mcp"

[MCPServers.example.Auth]
Type = "bearer"
Token = "sk-my-static-token"
```

### HTTP Basic

Sends `Authorization: Basic <base64(Username:Password)>`.

```toml
[MCPServers.example.Auth]
Type = "basic"
Username = "svc-account"
Password = "s3cret"
```

### Custom header

Sends the raw value of `Token` under `HeaderName` (no `Bearer ` prefix is added — supply the
full header value yourself). `HeaderName` defaults to `Authorization` when left empty.

```toml
[MCPServers.example.Auth]
Type = "header"
HeaderName = "X-Api-Key"
Token = "my-api-key-value"
```

An explicit entry already present in `[MCPServers.example.Headers]` always wins over the
header `Auth` would derive — use `Headers` directly if you need to override the exact
casing some picky proxy expects.

## OAuth 2.1 interactive flow (`Type = "oauth"`)

Drives the full authorization-code + PKCE flow: opens your browser, waits on a short-lived
local callback server, exchanges the code for tokens, and persists everything so future runs
need no further interaction until the refresh token itself stops working.

### Zero-config (auto-discovery + dynamic client registration)

No `OAuth` block needed at all if the server supports RFC 9728 protected-resource metadata
and RFC 7591 dynamic client registration:

```toml
[MCPServers.example]
Type = "streamable-http"
URL = "https://mcp.example.com/mcp"

[MCPServers.example.Auth]
Type = "oauth"
```

Then run `pando mcp login example` (see [CLI workflow](#cli-workflow-pando-mcp)).

### Pre-registered client

If the authorization server requires a client registered out-of-band (no DCR support, or DCR
disabled), set `ClientID` (and `ClientSecret`, if the server issued one — many public/native
clients only get a `ClientID`):

```toml
[MCPServers.example.Auth]
Type = "oauth"

[MCPServers.example.Auth.OAuth]
ClientID     = "abc123"
ClientSecret = "shh-client-secret"   # omit for a public client
```

### Scopes

```toml
[MCPServers.example.Auth.OAuth]
Scopes = ["mcp.read", "mcp.write"]
```

Scopes accumulate across logins: a 403 `insufficient_scope` step-up (see
[Spec conformance](#spec-conformance)) or a later `pando mcp login` never silently narrows a
scope you already granted — it always requests the union of what was previously granted plus
whatever is newly required.

### Custom callback port / redirect URI

The local callback server listens on `127.0.0.1:19876` at `/mcp/oauth/callback` by default.
Override the port if that's already taken, or pin an explicit redirect URI (must point at a
loopback host):

```toml
[MCPServers.example.Auth.OAuth]
CallbackPort = 54321
# or, equivalently and more explicitly:
RedirectURI  = "http://127.0.0.1:54321/mcp/oauth/callback"
```

### Overriding authorization-server metadata discovery

By default Pando discovers the authorization server from the MCP server's own RFC 9728
protected-resource metadata, then fetches RFC 8414 (or OIDC) authorization-server metadata.
If a server's discovery is broken or you need to point at a different issuer, override it
directly:

```toml
[MCPServers.example.Auth.OAuth]
AuthServerMetadataURL = "https://auth.example.com/.well-known/oauth-authorization-server"
```

## OAuth 2.1 client_credentials (headless/CI)

For service-to-service use where no user/browser is involved (RFC 6749 §4.4). Requires a
pre-registered confidential client — there is no dynamic registration or refresh token for
this grant; Pando re-mints a fresh access token from the same `ClientID`/`ClientSecret`
whenever the cached one is close to expiry.

```toml
[MCPServers.example.Auth]
Type = "oauth_client_credentials"

[MCPServers.example.Auth.OAuth]
ClientID     = "ci-service-account"
ClientSecret = "ci-service-secret"
Scopes       = ["mcp.read"]
```

Run `pando mcp login example` once to mint the initial token (it detects the grant type and
mints non-interactively — no `--no-browser`/`--manual` flags apply). After that, Pando mints
a fresh token on demand whenever the transport needs one; no further `pando mcp login` calls
are required unless you rotate the client secret.

## Mutual TLS and TLS options

TLS/mTLS options are independent of `Type` — they apply to the transport connection itself,
so a server can require both a client certificate *and*, say, a bearer token. Stdio servers
ignore all of these fields (no HTTP transport).

```toml
[MCPServers.example.Auth]
ClientCert        = "~/.pando/certs/client.pem"
ClientKey         = "~/.pando/certs/client-key.pem"
ClientKeyPassword = "key-passphrase"          # only if the key is encrypted
CACert            = "~/.pando/certs/ca.pem"    # trust an additional CA
```

`ClientCert`/`ClientKey`/`CACert` accept `~` and `$VAR`/`${VAR}` expansion. `ClientCert` and
`ClientKey` must both be set or both left empty — setting only one fails config validation.
An encrypted key must be the legacy RFC 1423 PEM format (`DEK-Info` header, e.g. produced by
older OpenSSL `-des3`/`-aes256` flags on `BEGIN RSA PRIVATE KEY`); a PKCS#8
`ENCRYPTED PRIVATE KEY` block is **not** supported by Go's standard library and Pando returns
an actionable error telling you to decrypt it out-of-band first (e.g.
`openssl pkcs8 -in key.pem -out key-decrypted.pem`).

```toml
[MCPServers.example.Auth]
SkipTLSVerify = true
```

**Warning:** `SkipTLSVerify = true` disables TLS certificate validation entirely for that
server's connection. This is insecure — it accepts any certificate, including one from an
attacker performing a man-in-the-middle — and should only be used against a known-trusted
server during local development or testing. Pando logs a warning every time it is used.

## Stdio servers

Stdio servers have no HTTP transport, so the `Auth` block does not apply to them at all —
`Type = "oauth"`/`oauth_client_credentials` on a stdio server errors out (`pando mcp login`
refuses with "stdio servers take credentials from the environment, not OAuth"), and the TLS
fields are silently ignored. Per the MCP spec, a stdio server's credentials go directly in
its process environment via `Env`:

```toml
[MCPServers.example]
Type    = "stdio"
Command = "my-mcp-server"
Env     = ["API_KEY=sk-my-static-token"]
```

`Env` values are eligible for the same AGE-at-rest encryption as everything else Pando
encrypts (see [Encryption at rest](#encryption-at-rest)).

## CLI workflow (`pando mcp …`)

```
pando mcp list                 # every configured server: type, auth type, and status
pando mcp status [name]        # detailed OAuth status; exits non-zero if [name] needs login
pando mcp login <name>         # start (or complete) authorization for one server
pando mcp logout <name>        # remove stored tokens + client registration for one server
```

`pando mcp login` flags (interactive `oauth` only — ignored for `oauth_client_credentials`,
which never opens a browser):

- `--no-browser` — don't try to open a browser automatically; just print the URL to open by
  hand.
- `--manual` — skip the local callback server entirely: paste the full redirect URL (or just
  the bare authorization code) back into the terminal after authorizing. Use this on a
  headless/remote host where the browser that completes the login can't reach
  `127.0.0.1:<callback-port>` on the machine running Pando.
- `--timeout <duration>` — how long to wait for the callback (or the manual paste) before
  giving up. Default 5 minutes.
- `--force` — with `--manual`, skip the confirmation prompt that appears when you paste a bare
  authorization code instead of the full redirect URL (see below). Has no effect otherwise.

### Pasting the full redirect URL vs. a bare code (`--manual`)

`pando mcp login <name> --manual` prefers the **full redirect URL** — the address the
authorization server sent your browser to after you approved the request (it looks like
`http://127.0.0.1:.../mcp/oauth/callback?code=...&state=...`, even if that page fails to load).
Pasting the full URL lets Pando validate the `state` it embedded in the original authorization
request (CSRF protection) and, when present, the RFC 9207 `iss` parameter — exactly the same
checks the local HTTP callback path performs. A pasted URL missing its `state` parameter is
rejected outright with an actionable error; it is not silently downgraded to the weaker path
below.

Pasting just the **bare authorization code** (no URL, e.g. because the page that showed it
didn't display the full address) skips both of those checks — there is nothing to validate
against. Pando makes that trade-off explicit: it prints an unmistakable warning and requires
you to type `y`/`yes` at a confirmation prompt before proceeding. Pass `--force` to skip that
prompt (e.g. for a scripted/expect-driven login) — you are still asserting the same trade-off,
just without the interactive pause.

`pando mcp logout` flags:

- `--yes` — skip the "Remove stored OAuth credentials for `<name>`? [y/N]" confirmation
  prompt.

### What a successful login stores, and where

On success, Pando persists (per server name):

- the access token, token type, refresh token (if any), granted scope, and expiry;
- the client ID/secret actually used — whether from static config or freshly obtained via
  RFC 7591 dynamic client registration (mcp-go performs DCR but does not persist its result
  across restarts; Pando fills that gap);
- the discovered authorization-server issuer and whether it declares the RFC 9207 `iss`
  parameter, so a later login doesn't need to re-discover it;
- the accumulated set of granted scopes, for step-up re-authorization.

This lives in a single JSON file: `mcp-auth.json` inside Pando's global config directory
(`$XDG_CONFIG_HOME/pando`, or `~/.config/pando` when `XDG_CONFIG_HOME` is unset), created
with directory mode `0700` and file mode `0600`. Set `PANDO_MCP_AUTH_FILE` to point at a
different path (tests and headless setups use this to isolate the store from a real user
config directory). Credentials are invalidated automatically if a server's configured `URL`
later changes — a stored token/registration is scoped to the resource server it was issued
for.

### Authorizing on a headless box

Two options, depending on the server's `Auth.Type`:

- **Interactive `oauth`, no local browser reachable**: run `pando mcp login <name> --manual`.
  Open the printed authorization URL on any machine with a browser, complete the login, then
  paste the resulting redirect URL back into the headless terminal (preferred — it carries
  `state` and, if present, `iss` for full validation) or, if that page never showed you the full
  address, the bare code (weaker — requires confirming the CSRF-state trade-off, or `--force`).
- **`oauth_client_credentials`**: just run `pando mcp login <name>` — it is already
  non-interactive by design; there's nothing else to configure for headless use beyond
  `ClientID`/`ClientSecret` in the config.

## Encryption at rest

Pando encrypts secret-shaped config values with [age](https://age-encryption.org) (an
`age1:`-prefixed ciphertext, transparently decrypted on load) whenever the config file is
saved. For `MCPAuth`, that covers:

| Field                       | Encrypted at rest? |
|-----------------------------|--------------------|
| `Auth.Token`                | Yes |
| `Auth.Password`              | Yes |
| `Auth.ClientKeyPassword`     | Yes |
| `Auth.OAuth.ClientSecret`    | Yes |
| `Auth.Username`               | No (not a secret) |
| `Auth.HeaderName`             | No |
| `Auth.OAuth.ClientID`         | No (not a secret) |
| `Auth.OAuth.Scopes`, `RedirectURI`, `CallbackPort`, `AuthServerMetadataURL` | No |
| `Auth.ClientCert`, `ClientKey`, `CACert` (file paths) | No — these are paths, not secrets; the referenced key file's own contents are not read/re-encrypted by Pando |
| `Auth.SkipTLSVerify`          | No (boolean flag) |

The `mcp-auth.json` store also has its sensitive values encrypted at rest, the same way as
`.pando.toml`: each server entry's `tokens.accessToken`, `tokens.refreshToken` and
`clientInfo.clientSecret` are individually wrapped in the same `age1:`-prefixed envelope (via
`config.EncryptSecretString`/`DecryptSecretString`, thin exported wrappers around the same key
management `.pando.toml` uses — no separate key or crypto scheme). Everything else in the file
— server URL, expiry, scopes, client id, issuer, timestamps — is left in clear, so the file
stays readable and hand-editable for debugging, matching the per-value (not whole-file)
encryption convention used elsewhere.

Notes on this:

- **Backwards compatible.** An existing plaintext `mcp-auth.json` from before this change still
  loads normally; its values are transparently encrypted the next time the store is written
  (login, refresh, logout, step-up, etc.) — no manual migration needed.
- **Fails open, never hard.** If no AGE key is available (fresh machine, misconfigured/read-only
  config home), Pando logs a single warning and falls back to storing that entry's values in
  clear rather than refusing to save — losing access to a configured MCP server because a key
  couldn't be loaded would be worse than the pre-existing filesystem-permissions-only posture.
- **Isolated failure.** If one entry's stored ciphertext fails to decrypt (e.g. the AGE key
  changed), only that entry's tokens/client info are dropped (logged, treated as absent — you
  simply re-run `pando mcp login <name>`); every other server's credentials in the same file are
  unaffected.
- The `0600`/`0700` filesystem permissions and atomic write are unchanged and still apply
  regardless of whether encryption succeeded for a given entry — defense in depth, not a
  replacement for it.

## Spec conformance

Pando's MCP OAuth support layers Pando-specific code (`internal/mcpauth`) on top of
[mcp-go](https://github.com/mark3labs/mcp-go) v0.57.0's OAuth transport:

**From mcp-go v0.57.0:**
- OAuth 2.1 authorization-code grant with PKCE (S256)
- RFC 9728 OAuth 2.0 Protected Resource Metadata discovery
- RFC 8414 OAuth 2.0 Authorization Server Metadata (and OIDC Discovery 1.0 fallback)
- RFC 7591 Dynamic Client Registration (the exchange itself)
- Token refresh
- RFC 8707 Resource Indicators (the `resource` parameter on token requests)

**Pando-specific (`internal/mcpauth`):**
- RFC 9207 §3 `iss` (authorization-server issuer) validation on the callback, including the
  extra metadata fetch needed because mcp-go's parsed metadata struct doesn't expose
  `authorization_response_iss_parameter_supported`
- HTTP 403 `insufficient_scope` step-up re-authorization (`WWW-Authenticate` challenge
  parsing, scope accumulation, a retry cap so a misbehaving server can't loop a caller
  forever)
- Persistence of dynamic client registration results across process restarts (mcp-go
  performs DCR but never persists it)
- The local loopback callback server (mcp-go doesn't ship one)
- Credential storage (`mcp-auth.json`) with cross-process locking
- mTLS / custom CA / `SkipTLSVerify` transport options
- The `oauth_client_credentials` grant end-to-end (mcp-go's OAuth transport only implements
  the authorization-code grant)

## Troubleshooting

**"has no client_id configured and dynamic client registration failed"** — the authorization
server doesn't support RFC 7591 DCR (or has it disabled). Register a client with the server
out-of-band and set `Auth.OAuth.ClientID` (and `ClientSecret`, if issued) in the config.

**"listen on 127.0.0.1:19876: ... address already in use"** — another process (or a stale
Pando instance) already holds the default callback port. Set a different
`Auth.OAuth.CallbackPort`, or free the port and retry.

**Login hangs / times out in a non-interactive/remote environment** — the browser that
completes the login can't reach the callback server on the machine running Pando (common
over SSH, in a container, or on a headless server). Use `pando mcp login <name> --manual`
(paste the redirect URL/code back by hand) or, for service accounts, switch that server to
`Type = "oauth_client_credentials"` instead.

**"the persisted dynamic client registration ... looks stale or was rejected"** — the
authorization server returned `invalid_client` for a dynamically-registered client (expired
secret, revoked registration, or the server reset its client store). Pando clears the stale
registration automatically and tells you to re-run `pando mcp login <name>`, which triggers
a fresh RFC 7591 registration. If `Auth.OAuth.ClientID` is set explicitly in config, this
auto-clear does not apply — you must update the config yourself.

**"MCP server ... needs additional permissions (scope: ...)"** (`ErrInsufficientScope`) — the
server rejected a request with 403 `insufficient_scope`. Run `pando mcp login <name>` again;
it requests the union of previously-granted scopes plus whatever the server just challenged
for. If this keeps happening for the same accumulated scope set more than twice, Pando gives
up with a hard error instead of looping forever — check the server's own scope requirements.

**Authorization response `iss` does not match / is missing** — Pando rejects the callback
rather than silently accepting it, since this can indicate an authorization-server
mix-up/confusion attack (RFC 9207 §3). This should not happen against a correctly configured
server; if it does, verify `Auth.OAuth.AuthServerMetadataURL` (if set) actually points at the
issuer the server itself uses.
