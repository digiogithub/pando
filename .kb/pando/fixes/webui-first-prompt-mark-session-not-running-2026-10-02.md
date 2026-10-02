---
created_at: 2026-10-02T10:52:07.936052874Z
updated_at: 2026-10-02T10:52:07.936052874Z
tags:
    - fix
    - webui
    - chat
    - sse
    - pando
---
# Fix: first prompt of a new session left it marked as running after `done`

Date: 2026-10-02. Follow-up to [[webui-first-prompt-duplicated-response-2026-10-02]].

## What changed
`web-ui/packages/pando-client/src/hooks/useChat.ts`, `handleDone`: `markSessionRunning(…, false)` now uses `sessionId ?? streamSessionRef.current` instead of `sessionId` alone.

## Why
`sendMessage` captures `sessionId = activeSessionId`, which is null for the first prompt of a session the stream itself creates. `handleDone(null, true)` therefore never cleared `is_running` in the store; the session stayed marked as running until the next `fetchSessions`/pending poll. On v1.2.6 and earlier this stale flag was part of the duplicated-response bug; on main (run_seq reattach, ed1d59077) it is only cosmetic. `streamSessionRef` is filled from the stream's own events, so it knows the created session id.

## Verification
`bun run typecheck` and `bun run test` (vitest) in `web-ui/`. No live E2E for the first-prompt case.
