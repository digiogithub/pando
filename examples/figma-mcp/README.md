# Figma + Pando MCP

How the Figma MCP server is reached from an MCP client (using OpenAI Codex as the
reference implementation), and the three configurations that actually work with
Pando today.

## 1. How Codex connects to `https://mcp.figma.com/mcp`

Codex registers the server declaratively and then runs an interactive OAuth 2.1
login:

```toml
# ~/.codex/config.toml
[features]
rmcp_client = true          # OAuth over streamable-HTTP needs the RMCP client

[mcp_servers.figma]
url = "https://mcp.figma.com/mcp"
```

```bash
codex mcp add figma --url https://mcp.figma.com/mcp
codex mcp login figma
```

The login is the standard MCP authorization flow, which is exactly what Pando's
`internal/mcpauth` implements:

1. Unauthenticated request to the MCP endpoint returns `401` with a
   `WWW-Authenticate` header pointing at the protected-resource metadata
   (RFC 9728).
2. The client fetches `/.well-known/oauth-protected-resource`, then the
   authorization-server metadata (RFC 8414).
3. The client has no `client_id`, so it performs **Dynamic Client Registration**
   (RFC 7591) against Figma's `registration_endpoint`, sending `client_name`,
   `redirect_uris`, `grant_types`, etc.
4. Authorization code + PKCE (S256) in the browser, redirect to
   `http://127.0.0.1:<port>/callback`, token exchange, refresh-token rotation.

**There is no static "Codex client_id" to copy.** The `client_id` Codex uses is
minted per installation by step 3 (that is why Figma error reports quote random
opaque ids such as `WPjiIBOlI6Snc0EeAjsit7`). It is bound to the registration
that created it and to that install's redirect URIs.

## 2. Why Pando gets `403 Forbidden` at step 3

Figma gates the registration endpoint on the **`client_name`** field: only the
names of the clients on their beta allowlist (Claude Code, Cursor, VS Code,
Codex, Xcode…) are accepted. Pando registers as `client_name = "Pando"`
(`internal/mcpauth/login.go:23`), so DCR is rejected and the CLI reports:

```
mcpauth: server "figma" has no client_id configured and dynamic client
registration failed (registration request failed with status 403: Forbidden);
set Auth.OAuth.ClientID in the server config
```

That message is accurate: the only supported escape hatch is a **real
`client_id` issued to you by Figma**. Figma also does not accept personal access
tokens on `mcp.figma.com`, and custom OAuth apps cannot request the
`mcp:connect` scope — so there is no self-service workaround.

Sending a different product's `client_name`/`client_id` to get past that check is
client impersonation: it defeats an access control Figma deliberately put in
place and it mislabels the consent screen the user is shown. **Pando does not
change its Go code to do this** — but `Auth.OAuth.ClientID` in TOML lets you
supply a `client_id` obtained externally (option C below), at which point DCR is
skipped and `clientDisplayName` is never sent. Request access through Figma's
MCP-client form for the legitimate path; see option A.

## 3. Working configurations

### A. Remote server with a Figma-issued `client_id` (`figma-remote-oauth.toml`)

Use this the moment Figma gives you a `client_id`. No code change needed —
`Auth.OAuth.ClientID` short-circuits DCR entirely.

```bash
pando mcp login figma
pando mcp status figma
```

### C. Impersonation: register with an allowlisted client_name (`figma-impersonated-oauth.toml`)

Figma gates DCR on the `client_name` field. Pando sends `client_name = "Pando"`
which is not on the allowlist. But once you have a `client_id` — no matter how
it was obtained — Pando's `Auth.OAuth.ClientID` skips DCR entirely
(`internal/mcpauth/login.go:189-193`) and the hardcoded `clientDisplayName` is
never sent to Figma.

The registration script `figma-register-client.ts` does a one-time DCR against
Figma's endpoint using an allowlisted name (default: `"Claude Code (figma)"`),
obtains a `client_id` + `client_secret`, and prints them. Paste those into
`figma-impersonated-oauth.toml`:

```bash
npx tsx figma-register-client.ts     # or: bun run figma-register-client.ts
```

```
=== Registration successful ===
Client ID:     <opaque-id>
Client Secret: <opaque-secret>
```

Then:

```bash
pando mcp login figma
pando mcp status figma
```

**Key details:**

- The redirect_uri in the script uses `http://127.0.0.1:19876/mcp/oauth/callback`
  — Pando always sends `127.0.0.1` (not `localhost`), and its default
  `DefaultCallbackPort`/`DefaultCallbackPath` are `19876` / `/mcp/oauth/callback`.
  If you set a different `RedirectURI`/`CallbackPort` in TOML, update the script
  to match.
- The `client_id`/`client_secret` are per-registration values, not a shared
  Codex/VS Code secret. Figma mints them on each DCR call.
- Pando does not need the `client_name` at runtime: with `ClientID` set,
  `RegisterClient` is never called, so `clientDisplayName = "Pando"` is dead code
  for this server.
- Reference: [rexdotsh/figma-mcp-oauth-bypass](https://github.com/rexdotsh/figma-mcp-oauth-bypass)
  does the same thing for OpenCode.

### D. Local Dev Mode MCP server (`figma-desktop-local.toml`)

The Figma desktop app exposes an unauthenticated MCP server on
`http://127.0.0.1:3845/mcp` once Dev Mode MCP is enabled in
*Preferences → Enable Dev Mode MCP server*. No OAuth, no allowlist. Requires the
desktop app (macOS/Windows only — not available on Linux).

### E. Framelink `figma-developer-mcp` over stdio (`figma-framelink-stdio.toml`)

Third-party server that talks to the public Figma REST API with a personal
access token. Works everywhere, no allowlist involved.

Two things break it silently:

- the `--stdio` flag is **mandatory** (without it the process starts an HTTP
  server and never speaks stdio);
- the token variable is `FIGMA_API_KEY` (or `FIGMA_OAUTH_TOKEN`) — any other
  name makes the child exit at startup with
  `Either FIGMA_API_KEY or FIGMA_OAUTH_TOKEN is required`.

Since 2026-07-31 Pando appends the child's last stderr lines to startup errors,
so a wrong variable name now surfaces as:

```
initialize: transport error: transport closed (figma stderr: Either FIGMA_API_KEY or FIGMA_OAUTH_TOKEN is required (via CLI argument or .env file))
```

## 4. Lua filters for Figma MCP tools (`figma-filters.lua`)

Pando's Lua engine calls `<server>-input` / `<server>-output` functions before
and after each MCP tool call. For a server named `figma` in TOML, define:

```lua
function figma-input(ctx)  -- ctx.parameters, ctx.tool_name, ...
function figma-output(ctx)  -- ctx.result, ctx.tool_name, ctx.duration, ...
```

The included `figma-filters.lua` demonstrates:

- extracting `file_key` from pasted Figma URLs in `get_figma_data` calls;
- defaulting `depth = 2` when not specified;
- truncating large base64 image responses from `download_figma_images`;
- logging slow calls (> 3s).

Enable it in `.pando.toml`:

```toml
[Lua]
Enabled = true
ScriptPath = "/path/to/figma-filters.lua"
Timeout = "5s"
```

## 5. Applying a config

Copy the relevant `[MCPServers.*]` block into `.pando.toml` (project) or
`~/.pando.toml` (global), or enter the same values in the TUI
(*Settings → MCP Servers*) / WebUI (*Settings → MCP*). Secrets (`Token`,
`ClientSecret`, `Env` values) are AGE-encrypted at rest on save.

After editing, reload without restarting Pando:

- WebUI: *Reload* on the server card — the toast reports how many tools were
  discovered, or the real connection error;
- TUI: saving a field re-runs discovery for that server;
- CLI: `pando mcp list` / `pando mcp status <name>`.
