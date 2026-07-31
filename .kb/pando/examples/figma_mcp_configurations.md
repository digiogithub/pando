---
created_at: 2026-08-03T07:30:12.794737685Z
updated_at: 2026-08-03T07:30:12.794737685Z
tags:
    - example
    - mcp
    - oauth
    - figma
    - config
---
# Example: Figma MCP configurations for Pando (`examples/figma-mcp/`)

Date: 2026-08-03.

## What was added

New folder `examples/figma-mcp/` with:

- `README.md` — analysis of how OpenAI Codex reaches `https://mcp.figma.com/mcp`,
  why Pando gets a 403 at dynamic client registration, and how to apply a config.
- `figma-remote-oauth.toml` — remote streamable-HTTP server with
  `Auth.Type = "oauth"` and a Figma-issued `Auth.OAuth.ClientID` (skips DCR).
- `figma-desktop-local.toml` — unauthenticated Dev Mode server on
  `http://127.0.0.1:3845/mcp` (macOS/Windows desktop app only).
- `figma-framelink-stdio.toml` — `npx -y figma-developer-mcp --stdio` with
  `FIGMA_API_KEY`.

## Findings on the Codex plugin

- Codex declares `[mcp_servers.figma] url = "https://mcp.figma.com/mcp"` in
  `~/.codex/config.toml`, needs `[features] rmcp_client = true` for OAuth over
  streamable-HTTP, and authorizes with `codex mcp login figma`.
- The flow is the standard MCP authorization flow Pando already implements in
  `internal/mcpauth`: 401 + `WWW-Authenticate`, RFC 9728 protected-resource
  metadata, RFC 8414 AS metadata, RFC 7591 DCR, authorization code + PKCE S256
  on a loopback redirect.
- **There is no static Codex `client_id` to copy**: it is minted per install by
  DCR (Figma error reports quote random opaque ids like `WPjiIBOlI6Snc0EeAjsit7`)
  and is bound to that registration's redirect URIs.
- Figma gates the registration endpoint on the `client_name` field against a
  beta allowlist (Claude Code, Cursor, VS Code, Codex, Xcode). Pando sends
  `client_name = "Pando"` (`internal/mcpauth/login.go:23`), hence
  `registration request failed with status 403: Forbidden`. Personal access
  tokens are not accepted on `mcp.figma.com` and custom OAuth apps cannot get
  the `mcp:connect` scope.

## Decision

Spoofing another product's `client_name`/`client_id` was requested but not
implemented: it circumvents Figma's access control and mislabels the OAuth
consent screen. No knob for it was added; the README documents the legitimate
paths (Figma's MCP-client access form, local Dev Mode server, Framelink stdio).

## Verification

Documentation/config only — no Go or TypeScript code touched, so no build or
test run was required. TOML snippets mirror the field names in
`internal/config/mcp_auth.go` (`MCPAuth`, `MCPOAuthConfig`) and
`config.MCPServer`. The stdio pitfalls (`--stdio`, `FIGMA_API_KEY`) were
reproduced manually in the previous session.

Related: [[feature_mcp_client_authentication]],
[[mcp_client_auth_phase3_oauth_flow]],
[[mcp_runtime_tool_refresh_and_header_spaces]],
[[mcp_stdio_startup_error_stderr]]
