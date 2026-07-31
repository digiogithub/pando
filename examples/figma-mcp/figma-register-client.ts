#!/usr/bin/env node
//
// Registers an OAuth client with Figma's MCP server using an allowlisted
// client_name so the registration is accepted. Prints the client_id and
// client_secret that you then paste into Pando's TOML config.
//
// This does NOT exchange tokens — Pando does that itself via
// `pando mcp login figma` once the client_id is in the config.
//
// Adapted from https://github.com/rexdotsh/figma-mcp-oauth-bypass
// Changes:
//   - redirect_uri uses 127.0.0.1 (Pando always sends 127.0.0.1, not localhost)
//   - redirect_uri path is /mcp/oauth/callback (Pando's DefaultCallbackPath)
//   - port is 19876 (Pando's DefaultCallbackPort)
//   - only registration is performed; no token exchange
//
// Usage:
//   npx tsx figma-register-client.ts
//   bun run figma-register-client.ts

// ── Config ───────────────────────────────────────────────────────────────

// The client_name Figma allowlists. Pick one:
//   "Claude Code (figma)"  — used by Claude Code
//   "Cursor"               — used by Cursor
//   "VS Code"              — used by VS Code
//   "Codex"                — used by OpenAI Codex
const CLIENT_NAME = "Claude Code (figma)";

// Pando's default callback. Must match what Pando sends in the authorization
// request. Pando always uses 127.0.0.1 (not localhost) and its default path
// is /mcp/oauth/callback on port 19876.
// If you set a different RedirectURI/CallbackPort in TOML, change these to match.
const REDIRECT_URI = "http://127.0.0.1:19876/mcp/oauth/callback";

// Figma's OAuth endpoints (discovered from RFC 8414 metadata).
const FIGMA_REGISTER = "https://api.figma.com/v1/oauth/mcp/register";

// ── Registration ─────────────────────────────────────────────────────────

async function register() {
  const res = await fetch(FIGMA_REGISTER, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      client_name: CLIENT_NAME,
      redirect_uris: [REDIRECT_URI],
      grant_types: ["authorization_code", "refresh_token"],
      response_types: ["code"],
      token_endpoint_auth_method: "none", // public client, PKCE S256
    }),
  });

  if (!res.ok) {
    throw new Error(`Registration failed: ${res.status} ${await res.text()}`);
  }

  const data = await res.json() as {
    client_id: string;
    client_secret?: string;
  };

  if (!data.client_id) {
    throw new Error("Registration response missing client_id");
  }

  return data;
}

// ── Main ─────────────────────────────────────────────────────────────────

console.log(`Registering OAuth client with Figma as "${CLIENT_NAME}"...`);
console.log(`Redirect URI: ${REDIRECT_URI}\n`);

const creds = await register();

console.log("=== Registration successful ===");
console.log(`Client ID:     ${creds.client_id}`);
console.log(`Client Secret: ${creds.client_secret ?? "(none — public client)"}`);
console.log("");
console.log("Paste these into your Pando TOML config:");
console.log("");
console.log("  [MCPServers.figma.Auth.OAuth]");
console.log(`  ClientID = "${creds.client_id}"`);
if (creds.client_secret) {
  console.log(`  ClientSecret = "${creds.client_secret}"`);
}
console.log("");
console.log("Then run:  pando mcp login figma");
