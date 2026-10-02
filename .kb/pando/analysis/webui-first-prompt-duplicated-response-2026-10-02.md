---
created_at: 2026-10-02T10:49:24.984075956Z
updated_at: 2026-10-02T10:49:24.984075956Z
tags:
    - analysis
    - webui
    - chat
    - sse
    - pando
---
# Analysis: first prompt of a new session renders the assistant response twice (v1.2.6 and earlier)

Date: 2026-10-02. Related: [[resumed-run-live-updates-webui-e2e-run-seq-2026-10-01]], [[webui_pending_askuserquestion_blocks_model_switch]].

## Report
Two users (macOS desktop, v1.2.5/v1.2.6, iOS developers) see the answer to the FIRST prompt of a new session duplicated. Later prompts in the same session are fine.

## Remote logs (Better Stack source `pando`, id 2751484)
- No `os=ios` clients; the two reporters are `darwin/desktop` (debug_id 2136-7892-4842-3089 and 5197-1123-6710-9997).
- Nothing logged for the duplication itself: it is purely a frontend bug, the backend emits each event once.
- Unrelated findings on those installs: `sqlite3: database is locked` in kb watcher delete, code reindex and `remembrances session index failed` (v1.2.6); one user has an embedding model (`vuongnguyen2212/CodeRankEmbed`) configured as a chat/title model on Ollama (400 "does not support chat").

## Root cause (released code, `web-ui/src/components/chat/ChatView.tsx` + `pando-client/src/hooks/useChat.ts` at tag v1.2.6)
1. `sendMessage` captures `sessionId = activeSessionId`, which is `null` for the first prompt of a new session.
2. During the run the 4 s `/sessions/{id}/pending` poll (and `fetchSessions` from `onNewSession`) sets `is_running = true` on the new session.
3. On `done`, `handleDone(null, true)` skips `markSessionRunning(sessionId, false)` because `sessionId` is null, and ChatView's `onDone` closure (captured when the prompt was sent) also sees `activeSessionId === null`, so `finishedSessionRef` is never set.
4. `streaming` turns false while the store still says `is_running = true`: the reattach effect calls `reconnectSession`, which adds a new assistant bubble and replays the whole server event buffer from `GET /sessions/{id}/stream`. Result: the response appears twice.
5. From the second prompt on `sessionId` is non-null, both guards work, no duplicate.

The guards date from 0a044769c (2026-08-19); present in every tag up to v1.2.6.

## Status on main
Not reproducible by code reading after ed1d59077 (2026-10-01, unreleased): reattach is driven by `run_seq` (`shouldReattach`), `streamSessionEvents` emits a `run` event carrying `sessionId` + `runSeq` on the POST stream too, and `useChat.handleEvent` records it via `setSeenRunSeq`, so the client never replays the run it streamed. `finishedSessionRef` was removed.
Residual on main (cosmetic): `handleDone` still skips `markSessionRunning` when `sessionId` is null; could use `sessionId ?? streamSessionRef.current`.

## Action
Ship a release containing ed1d59077 (v1.2.7). No code changed in this analysis; not verified live (no E2E run for the first-prompt case).
