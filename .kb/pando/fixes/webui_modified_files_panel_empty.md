---
created_at: 2026-08-19T15:31:37.562174192Z
updated_at: 2026-08-19T15:31:37.562174192Z
tags:
    - fix
    - webui
    - agentvcs
    - sse
    - tools
---
# Fix: WebUI "modified files" panel stayed empty

Date: 2026-08-19

## Symptom
In the WebUI the user saw edit/write tool calls rendered in the chat, but the
FileChangesBar and the ChatInfoSidebar "Modified files" section stayed empty.

## Root causes
1. **No tool_call event for non-streaming providers.** `internal/llm/agent/agent.go`
   `processEvent` only publishes `AgentEventTypeToolCall` on
   `EventToolUseStart/Delta/Stop`. A provider that reports its tool calls in the
   final `EventComplete` never published them, so the SSE handler
   (`internal/api/handlers_chat.go`) had an empty `pendingInputs[toolCallID]`.
   The tool result then synthesized a `tool_call` start event (so the call was
   visible) but `diffMeta` was skipped because it is built from the *stored
   input* (`file_path`/`old_string`/`new_string`/`content`). No `diff` in the
   `tool_result` payload = `useChat` never called `fileChangesStore.addChange`.
2. **Store was live-only and reset per turn.** `useChat.resetAccum` called
   `clearChanges()` on every `sendMessage`, and nothing rebuilt the panel from
   session history, so a page reload or a session switch always showed nothing.
3. **Multi-file tools invisible.** `patch` has no `file_path` in its input; its
   paths live in `PatchResponseMetadata.FilesChanged` and were never read.

## Changes
- `internal/llm/agent/agent.go` (`processEvent`, `EventComplete`): capture the
  streamed tool calls before `SetToolCalls`, then publish
  `AgentEventTypeToolCall` (with `Finished=true`) for every resolved call the
  stream never announced. Fixes WebUI SSE and AG-UI alike.
- `web-ui/src/stores/fileChangesStore.ts`: new `hydrateSession(sessionId, messages)` —
  replays `edit/write/patch/multiedit` tool_call parts from history, then merges
  `GET /api/v1/agentvcs/sessions/{id}/diff` for paths history does not name
  (entries flagged `fromVcs`, additions/removals 0). Endpoint returns an empty
  diff when agent-vcs is disabled, so it degrades silently.
- `web-ui/src/hooks/useChat.ts`: `resetAccum` no longer clears the store (the
  panel now accumulates across turns of the same session); tool results also
  register `metadata.files_changed` paths (patch).
- `web-ui/src/components/chat/ChatView.tsx` and `SimpleChatView.tsx`: hydrate on
  session load / session switch; SimpleChatView clears when no session.

## Verification
- `go build ./...`
- `go test ./internal/llm/agent ./internal/api` — ok
- `npx tsc --noEmit` and `npx eslint` on the touched web-ui files — clean
