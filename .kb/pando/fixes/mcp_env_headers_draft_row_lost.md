---
created_at: 2026-07-31T12:39:19.519921962Z
updated_at: 2026-07-31T12:39:19.519921962Z
tags:
    - fix
    - mcp
    - webui
    - tui
    - config
---
# Fix: MCP Env/Headers typed in the UI were silently dropped (2026-07-31)

## Symptom

Entering `FIGMA_API_KEY=...` in the WebUI MCP server modal (and headers likewise)
appeared to work, but `.pando.toml` kept `Env = []` / an empty `Headers` table.
Same complaint for the TUI.

## Root causes

1. **WebUI — uncommitted draft row.** `web-ui/src/components/shared/KeyValueEditor.tsx`
   keeps the "new pair" row in LOCAL state (`newKey`/`newValue`) and only pushes
   it into `pairs` via the "Add" button or Enter. Typing a key/value and clicking
   "Save" straight away discarded it, so `formToServer` serialized `env: []`.
   This affected every consumer of the component (MCP env AND headers).
2. **TUI — whitespace splitting.** `mcpServers.<name>.env` was parsed with
   `strings.Fields`, so any value containing a space produced several bogus
   entries (same class of bug already fixed for headers).

The config/API layers were verified NOT to be at fault: a direct
`config.UpdateMCPServer` and a `handlePutConfigMCPServer` round trip both
persist `Env`/`Headers` correctly (AGE-encrypted at rest, plaintext on reload).

## Changes

- `web-ui/src/components/shared/KeyValueEditor.tsx`: the draft row is wrapped in
  a ref'd container with `onBlur={commitDraftOnLeave}`; focus leaving the row
  (e.g. clicking Save) commits the pending pair. Moving between the row's own
  key/value inputs does not commit (checked via `relatedTarget`).
- `internal/config/mcp_headers.go`: added `ParseEnvPairs` / `FormatEnvPairs`,
  mirroring `ParseHeaderPairs` — comma/newline separated `KEY=value`, values may
  contain spaces, legacy space-separated form still accepted when every token
  carries its own `=`.
- `internal/tui/page/settings.go`: `env` field now parsed/rendered with those
  helpers, with a hint; parse errors surface instead of silently mangling.
- `internal/tui/components/dialog/add_mcp_server.go`: same for the Env field of
  the add-server dialog; placeholders/hints updated to the comma syntax.

## Verification

- `go build ./...`, `go test ./internal/config ./internal/api ./internal/mcpgateway ./internal/llm/agent` pass.
- New table tests `TestParseEnvPairs` (+ existing `TestParseHeaderPairs`).
- `npx tsc --noEmit` clean in `web-ui`.
- Throwaway end-to-end tests (removed afterwards) confirmed both
  `config.UpdateMCPServer` and the API PUT handler persist `Env` to
  `.pando.toml`.

## Figma context

Remote `https://mcp.figma.com/mcp` cannot be used from Pando: Figma's DCR
endpoint (`https://api.figma.com/v1/oauth/mcp/register`) returns 403 for any
client outside their MCP catalog allowlist. Working alternative on Linux is the
Framelink stdio server, which **requires `--stdio`** in Args plus
`FIGMA_API_KEY` in Env:
`Command = 'npx'`, `Args = ['-y','figma-developer-mcp','--stdio']`.

Related: [[mcp_runtime_tool_refresh_and_header_spaces]], [[mcp_server_config_fields]]
