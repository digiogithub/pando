---
created_at: 2026-08-19T09:50:15.919816155Z
updated_at: 2026-08-19T10:03:37.436380025Z
tags:
    - fix
    - webui
    - api
    - agent
    - askuserquestion
    - models
    - sse
---
# Fix: WebUI model switch failed while an unrendered AskUserQuestion blocked the session

Date: 2026-08-19

## Symptom
In the WebUI/desktop, after starting a run with an Ollama model, the model selector
kept failing with a generic "Failed to switch model" toast. Closing and reopening the
desktop app made switching work again — and revealed a pending `AskUserQuestion`
dialog that had never been rendered during the run.

## Root cause
1. `question_request` / `permission_request` are only pushed over the chat SSE stream
   (`internal/api/handlers_chat.go`, `streamSessionEvents`). A client not attached to
   that stream (dropped connection, reload, or a run that continued in the background
   after the initial stream ended) never sees them. The agent stays blocked inside
   `UserInput.Ask`, so the session stays busy.
2. `agent.Update` refuses with `cannot change model while processing requests` when
   `IsBusy()` is true — which it was, because of the blocked tool.
3. The WebUI `ModelSwitcher` swallowed the server error (`catch { addToast('Failed to
   switch model') }`), hiding the actual reason.
4. Secondary: the client could not tell a real end-of-run from a dropped stream, so it
   marked the session as finished and never reattached (`reconnectedSessionRef` also
   allowed a single reconnection per session for the component's lifetime).

## Changes (slice 1 — make the prompt visible again)
- `internal/api/handlers_questions.go`: new `handleSessionPending`
  (`GET /api/v1/sessions/{id}/pending`) returning `{permissions, questions, running}`
  from `Permissions.PendingRequests` / `UserInput.PendingRequests` / `bgRunner.IsBusy`.
- `internal/api/routes.go`: route registration.
- `web-ui/src/stores/sessionStore.ts`: `fetchPendingRequests(sessionId)` merges pending
  prompts by id (idempotent, ignores responses for a session that is no longer active)
  and applies the server's `running` flag to the sessions list.
- `web-ui/src/components/chat/ChatView.tsx`: polls it on session change and every 4s.
- `web-ui/src/components/overlays/ModelSwitcher.tsx`: `serverErrorMessage` parses the
  `{"error": "..."}` body so the toast shows the real reason.
- `internal/llm/agent/agent.go` (`runInternal`): `activeRequests.Delete` /
  `clearSteering` / `cancel()` moved into a deferred cleanup registered after
  `RecoverPanic` (LIFO: runs first), so a panic can no longer leak a busy entry that
  keeps `IsBusy()` true forever and blocks every model switch until restart.

## Changes (slice 2 — reattach to a run whose stream dropped)
No new API: `bgRunner` already keeps a replay buffer (10 min TTL) and
`GET /api/v1/sessions/{id}/stream` already replays before going live.
- `web-ui/src/services/sse.ts`: `onDone` now takes `completed: boolean` — `true` only
  for a real `done` event, `false` when the body ends without one (dropped connection).
- `web-ui/src/hooks/useChat.ts`: `handleDone(sessionId, completed)` only calls
  `markSessionRunning(false)` when `completed`; `reconnectSession` no longer appends a
  second empty assistant bubble when the last message already is one.
- `web-ui/src/components/chat/ChatView.tsx`: reconnection is driven by server state
  (`sessions[].is_running`, refreshed by the pending poll) instead of by stream events;
  `reconnectedSessionRef` is re-armed whenever the session stops running, and
  `finishedSessionRef` blocks a reattach triggered by a poll response that was already
  in flight when the run completed (which would replay the whole buffer).

## Verification
- `go build ./...`, `go test -count=1 ./internal/api ./internal/llm/agent` — ok
- `npm --prefix web-ui run build` (tsc -b + vite) — ok; eslint clean on touched files

## Related
[[project_ask_user_question_tool_plan]] [[fix_fresh_machine_coder_model]]
