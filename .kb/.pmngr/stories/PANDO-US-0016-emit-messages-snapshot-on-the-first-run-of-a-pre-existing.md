---
id: PANDO-US-0016
type: story
title: Emit MESSAGES_SNAPSHOT on the first run of a pre-existing thread
status: backlog
priority: high
parent: PANDO-EP-0003
milestone: PANDO-M-0001
author: claude
labels: [agui, api]
estimate: 3
created: 2026-09-13T21:14:53Z
updated: 2026-09-13T21:14:53Z
---

## Description

As a browser client that lost its local transcript, I want the server to resynchronise me in-band on my next run, so that I do not need a second round trip to the thread API.

`NewMessagesSnapshot` exists (`internal/agui/events.go:429-436`) but has zero call sites. Emit it in the run path right after `STATE_SNAPSHOT` (`internal/agui/state.go:80-82`), and only when `sessionForThread` (`internal/agui/runtime.go:126-144`) found a session that already existed — never on a thread created by this very run, where it would be an empty snapshot. Reuse the AG-UI `Message[]` conversion written for the thread-API story rather than writing a second converter. Cap the snapshot: a configurable maximum number of messages and total bytes, taking the most recent messages; when truncated, say so in the emitted payload so the client knows the transcript is partial.

Do NOT emit it on every run of a thread (once per stream attach, on the first run of that attach), do NOT emit it before `STATE_SNAPSHOT`, and do NOT let a large history block the stream — build the snapshot before the run starts, not inside the event loop.

## Acceptance Criteria

- [ ] Event ordering on a pre-existing thread is `RUN_STARTED` -> `STATE_SNAPSHOT` -> `MESSAGES_SNAPSHOT`, asserted in `internal/agui/server_test.go`.
- [ ] A brand-new thread emits no `MESSAGES_SNAPSHOT`.
- [ ] A history above the configured cap yields a truncated, most-recent-first-complete snapshot flagged as truncated, and the run still starts within the same latency budget.
- [ ] Tool calls and their results survive the conversion with matching `toolCallId`s.

## Notes

Depends on the thread-API story (PANDO-EP-0003) for the AG-UI message converter. Size **S**.
