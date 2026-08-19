---
created_at: 2026-08-19T10:10:30.330521514Z
updated_at: 2026-08-19T11:06:39.431630096Z
tags:
    - fix
    - webui
    - ipc
    - instances
---
# Fix: WebUI Instances panel showed no sessions / no conversation (2026-08-19)

## Symptom
In the WebUI Instances panel both running instances were listed, but clicking one showed
"No sessions found" and no way to open a conversation or the live stream.

## Root cause
`GET /api/v1/instances/{id}/sessions` forwarded the raw IPC RPC result of `session.list`,
which is a **bare JSON array** of `protocol.SessionPayload`. The store
(`web-ui/src/stores/instancesStore.ts`, `selectInstance`) reads `data.sessions ?? []`, so it
always got `undefined` and set an empty list. Verified live against a running instance:
`curl .../sessions` returned `[{...}]` instead of `{"sessions":[...]}`.

Secondary gaps found in the same panel:
- No endpoint/UI for the existing conversation: only live PUB events were rendered.
- `message.send` / `session.interrupt` registered with `nil` runner/interrupter in
  `cmd/bridge_delegation.go`, so "Send Message" and "Cancel" always failed with
  "agent runner not available on this instance".
- Per-session SSE filter only checked `payload.session_id`, ignoring the envelope's
  top-level `sessionId`, leaking other sessions' events.

## Changes (first pass)
- `internal/api/handlers_instances.go`
  - `handleInstanceListSessions`: unmarshal into `[]protocol.SessionPayload` and respond
    `{"sessions": [...]}` (nil-safe). The RPC contract stays an array because
    `internal/remoteview/control.go` and `internal/ipc/bridge` tests depend on it.
  - New `handleInstanceListMessages`: `GET /api/v1/instances/{id}/sessions/{sid}/messages`,
    calls `protocol.MethodMessageList`, responds `{"messages": [...]}` (15s timeout).
  - `handleInstanceSessionStream`: filter also on the envelope `sessionId`.
- `internal/api/routes.go`: registered the new `/messages` route.
- `cmd/bridge_delegation.go`: new `agentMessageRunner` adapter (agent.Service ->
  `bridge.MessageRunner`, `context.WithoutCancel`, drains the event channel in a goroutine);
  `pandoApp.CoderAgent` wired as `bridge.SessionInterrupter`.
- `web-ui/src/stores/instancesStore.ts`: `RemoteMessage` type + `fetchRemoteMessages`;
  `selectInstance` logs the failure instead of silently emptying the list.
- `web-ui/src/components/instances/RemoteSessionView.tsx`: loads history on session select
  (loading/error states), renders it with `MessageRow`, then a "Live stream" separator and
  the stream events.

## Follow-up: forced scroll on old sessions (same day)
Reading an old session was impossible: `instance.heartbeat` fires every 5s, has no
`session_id` (so it passes the session filter), was appended as an event, and the
unconditional `scrollIntoView` on every `streamEvents` change yanked the viewport back to
the bottom.

Fixes in `RemoteSessionView.tsx`:
- `NOISE_TOPICS = {instance.heartbeat, instance.ping}` dropped in `es.onmessage` before
  reaching state (kept out of the list entirely, `streamConnected` still driven by
  `onopen`/`onerror`).
- Sticky auto-scroll: `scrollRef` + `onScroll` computes
  `scrollHeight - scrollTop - clientHeight < 40` and sets `autoScroll`. The scroll effect
  runs only when `autoScroll` is true, so scrolling up pins the view.
- "↓ Jump to latest" floating button appears while `autoScroll` is false; it re-enables it.
- `autoScroll` resets to true on session change.

## Verification
- `go build ./...`, `go test ./internal/api ./internal/ipc/...` — all pass.
- `npx tsc --noEmit` on web-ui — clean; `npm run build` — OK (both passes).
- Live: rebuilt binary via `pando serve --port 8799` (HTTPS, self-signed), queried the two
  running instances: `/sessions` -> `{"sessions":[…]}`, `/sessions/{sid}/messages` ->
  `{"messages":[…]}`. Test server stopped afterwards.

Related: [[project_inter_instance_plan]], [[feature_webui_projects_stop_instance]],
[[feature_tui_chat_copy_scroll]]
