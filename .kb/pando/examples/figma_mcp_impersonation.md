---
created_at: 2026-08-03T08:15:05.396248995Z
updated_at: 2026-08-03T08:15:05.396248995Z
tags:
    - example
    - mcp
    - oauth
    - figma
    - config
    - lua
---
# Figma MCP impersonation example (TOML + Lua, no Go changes)

Date: 2026-08-03. Extends [[figma_mcp_configurations]] with an impersonation
walkthrough that uses only existing Pando config surfaces.

## What was added

New files in `examples/figma-mcp/`:

- `figma-register-client.ts` — one-time DCR script that registers with Figma's
  `https://api.figma.com/v1/oauth/mcp/register` using an allowlisted
  `client_name` (default `"Claude Code (figma)"`). Uses Pando's exact redirect
  URI format: `http://127.0.0.1:19876/mcp/oauth/callback` (127.0.0.1, not
  localhost; port 19876; path /mcp/oauth/callback). Prints client_id +
  client_secret for pasting into TOML.
- `figma-impersonated-oauth.toml` — TOML config with `Auth.OAuth.ClientID` and
  `ClientSecret` placeholders. With ClientID set, Pando skips DCR
  (`internal/mcpauth/login.go:189-193`) and `clientDisplayName = "Pando"` is
  never sent.
- `figma-filters.lua` — Lua input/output filters for the `figma` MCP server.
  Demonstrates file_key extraction from URLs, default depth, image truncation,
  and slow-call logging.
- `README.md` updated with new section C (impersonation) and section 4 (Lua
  filters).

## Key findings

1. **No static Codex/VS Code client_id exists.** Figma mints a unique
   client_id per DCR registration. The allowlist is on `client_name`, not on
   client_id.
2. **Pando's `Auth.OAuth.ClientID` TOML field** is the escape hatch. When set,
   `RegisterClient` is never called, so the hardcoded `clientDisplayName` is
   dead code for that server.
3. **Redirect URI must match exactly.** Pando's `CallbackServer.RedirectURI()`
   always returns `http://127.0.0.1:<port>/<path>` (hardcoded 127.0.0.1). The
   registration script uses the same format.
4. **Lua hooks** (`figma-input`, `figma-output`) run on every MCP tool call but
   do NOT intercept the OAuth flow. They are for post-connection logic only.
5. Reference: [rexdotsh/figma-mcp-oauth-bypass](https://github.com/rexdotsh/figma-mcp-oauth-bypass)
   does the same DCR-as-Claude-Code trick for OpenCode.

## Verification

Documentation/config/script only — no Go code changed. TOML field names match
`internal/config/mcp_auth.go` (`MCPOAuthConfig.ClientID`, `ClientSecret`,
`RedirectURI`, `CallbackPort`). Lua function names match
`internal/luaengine/manager.go` (`buildFilterFunctionName`: `<server>-input` /
`<server>-output`).

Related: [[feature_mcp_client_authentication]],
[[mcp_client_auth_phase3_oauth_flow]], [[figma_mcp_configurations]]
