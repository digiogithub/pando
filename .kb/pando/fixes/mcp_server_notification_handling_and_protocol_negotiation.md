---
created_at: 2026-06-30T07:00:56.651264606Z
updated_at: 2026-06-30T07:00:56.651264606Z
tags:
    - fix
    - mcp
    - server
    - antigravity
---
# Fix: MCP server breaks strict clients (Antigravity) — notification reply + fixed protocol version

## Symptom
- Antigravity CLI failed to connect to Pando's MCP server.
  - HTTP/streamable mode: `error: calling "initialize": sending "initialize": failed to connect (session ID: ): session not found`.
  - stdio mode: error listing tools.
- opencode and claude code connected fine over stdio (they are lenient clients).

## Root cause
In `internal/mesnada/server/server.go`, `handleRequest` always returned a `*JSONRPCResponse` and the
callers always wrote it to the wire. Bugs:

1. **Replying to notifications.** A JSON-RPC 2.0 notification has no `id` and MUST NOT receive a
   reply. The MCP client sends `notifications/initialized` right after the initialize handshake.
   Pando replied to it, which strict clients (Antigravity) treat as a fatal protocol violation —
   in HTTP the client loses the session (`session not found`), in stdio it desyncs the stream so the
   subsequent `tools/list` fails. Lenient clients (opencode, claude code) ignore the stray `id:null`
   message, which is why they worked.
2. **Wrong method match.** The switch matched `"initialized"` but the real method name is
   `notifications/initialized`, so the notification fell through to `default` and produced a
   `-32601 Method not found` error response.
3. **Fixed protocol version.** `handleInitialize` always returned `protocolVersion: "2024-11-05"`
   regardless of what the client requested; strict clients can reject a mismatched negotiated version.

## Changes (`internal/mesnada/server/server.go`)
- `handleRequest`: if `req.ID == nil` (notification), dispatch to new `handleNotification` and return
  `nil`. Removed the dead `case "initialized"`.
- New `handleNotification`: no-ops `notifications/initialized` / `initialized` /
  `notifications/cancelled`, silently ignores any other notification.
- `runStdio`: skip encoding when `handleRequest` returns `nil` (nothing on the wire).
- `handleMCP` (HTTP): when response is `nil`, reply `202 Accepted` with empty body per Streamable
  HTTP spec; session header still set.
- New `negotiateProtocolVersion` + `supportedProtocolVersions` (`2024-11-05`, `2025-03-26`,
  `2025-06-18`): `handleInitialize` echoes the client's requested version when supported, else
  falls back to `mcpVersion`.

## Verification
- Rebuilt `pando`, `go vet ./internal/mesnada/server/` clean.
- HTTP curl: `initialize` negotiates `2025-06-18`; `notifications/initialized` and unknown
  notifications return `202` empty; `tools/list` works on the same session.
- stdio: exactly 2 response lines (ids 1 and 2), no `Method not found`, no reply to the notification,
  version negotiated.
- New tests `TestHandleRequestNotificationsGetNoResponse`, `TestNegotiateProtocolVersion`; full
  `go test ./internal/mesnada/server/` passes.

## Non-blocking follow-ups (spec-allowed today)
- `GET /mcp` returns `405` (no server->client SSE GET stream); allowed by spec. SSE is exposed
  separately at `/mcp/sse`.
- `DELETE /mcp` (explicit session termination) not handled; optional in the spec.
