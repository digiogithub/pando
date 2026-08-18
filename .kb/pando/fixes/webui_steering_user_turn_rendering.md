---
created_at: 2026-08-19T09:30:00.866058265Z
updated_at: 2026-08-19T09:30:00.866058265Z
tags:
    - fix
    - webui
    - steering
    - chat
    - pando
---
# Fix: WebUI steering feedback overwrote the assistant bubble instead of appearing as its own user turn

Date: 2026-08-19
Follows [[desktop_embed_keep_and_webui_steer_blocked]] (the change that unblocked sending while streaming).

## Problem

After enabling mid-run feedback in the WebUI (`ChatInput.tsx`: Send button always rendered,
`streaming` removed from the `handleSend` guard), submitting while the agent was busy:

- did NOT show the feedback as a user turn in the transcript, and
- visually "replaced" the previous prompt/answer, and
- looked like the agent loop never continued.

### Root cause (frontend only — backend steering was correct)

`useChat.steer()` optimistically appended the feedback as a user message at the END of the
message list. But the live stream writes into the LAST message:

- `handleEvent` → `updateLastMessage(accumulatedRef.current)` (sessionStore replaces
  `messages[len-1].content`), and `handleDone` → `updateLastMessageParts(parts)`.
- `MessageList` only passes `streamingState` to the last message when `role === 'assistant'`,
  so with a user message last it also rendered a duplicate `LoadingBubble`.

Result: the assistant's streamed text was written INTO the user's feedback bubble (the prompt
appeared replaced), and no new assistant bubble was ever created, so the continuation after
injection was invisible.

Secondary bug: a `409 not_busy` from `POST /api/v1/sessions/{id}/steer` (run finished between
the UI's `streaming` check and the request) silently dropped the message.

## Fix

`web-ui/src/hooks/useChat.ts`:
- New `pendingSteerRef` + `pendingFeedback` state: `steer()` no longer inserts a message; it
  queues the text locally and POSTs to `/steer`. Returns `SteerResult` = `'queued' | 'not_busy' | 'error'`.
- New `buildStreamParts()` helper (extracted from `handleDone`) that materializes
  `itemsRef`/`toolCallsRef` into `ContentPart[]`.
- `handleEvent` now handles the `steering_injected` SSE event (already emitted by
  `internal/api/handlers_chat.go` from `AgentEventTypeSteeringInjected`): it finalizes the
  currently streaming assistant bubble with `updateLastMessageParts(buildStreamParts())`,
  appends the queued feedback as real user message(s), appends a FRESH assistant placeholder,
  and resets `accumulatedRef`/`itemsRef`/`toolCallsRef` + live `streamingState` items. This keeps
  the last message an assistant bubble, so `updateLastMessage` targets the right one.
- `sendMessage`: on `'not_busy'` it falls through to a normal run instead of dropping the text.
- `handleDone` clears any leftover pending feedback and reuses `buildStreamParts()`.

`web-ui/src/components/chat/MessageList.tsx`: new `QueuedFeedback` chips (dashed border,
"queued ⏳") rendered from the new optional `pendingFeedback` prop, so the user sees WHAT is
queued; the chip disappears and becomes a real user turn when `steering_injected` arrives.
Scroll effect also reacts to `pendingFeedback.length`.

`ChatView.tsx` / `SimpleChatView.tsx`: pass `pendingFeedback` from `useChat` to `MessageList`.

`ChatInput.tsx`: footer hint while streaming — "Agent running · Enter queues feedback, injected
at the next safe boundary".

## Backend notes (verified, unchanged)

- `agent.drainSteeringInto` injects at two safe boundaries (after tool results, and at end of
  turn before returning), publishes `AgentEventTypeSteeringInjected` and continues the loop.
- Events reach the WebUI through `BackgroundSessionManager.pump` reading the run channel
  (buffered 512), so the `select { default: }` drop in `drainSteeringInto` is not a practical risk.

## Verification

- `cd web-ui && npx tsc --noEmit` — clean.
- `go build ./...` — clean.
- Manual browser click-through still pending.
