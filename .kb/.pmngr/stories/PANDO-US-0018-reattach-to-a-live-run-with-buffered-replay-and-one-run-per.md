---
id: PANDO-US-0018
type: story
title: Reattach to a live run with buffered replay, and one run per thread
status: done
priority: high
parent: PANDO-EP-0003
milestone: PANDO-M-0001
author: claude
labels: [agui, api]
estimate: 8
created: 2026-09-13T21:14:53Z
updated: 2026-09-13T21:14:53Z
---

## Description

As a browser client that reconnected, I want to re-join the run that is still in flight and see what I missed, so that a reload or a second tab does not kill or duplicate the turn.

Add `GET {path}/threads/{id}/stream` (and accept a `POST` carrying no new user message as the same reattach) that looks up the thread's `activeRun` in `runStore` (`internal/agui/run.go:102-122`), unparks it, replays the bounded buffer added by the disconnect story, then continues live — the Web-UI's buffered replay at `internal/api/handlers_chat.go:112` is the reference implementation to port. Replay must be consistent: use the run's `endedCalls` bookkeeping (`run.go:49-62`) so a `TOOL_CALL_END` already delivered is not re-emitted and a `TOOL_CALL_ARGS` whose start was replayed is never orphaned. A second attach follows read-only; only one attach may submit tool results.

Fix the concurrency at the same time. `handleRun` (`internal/agui/server.go:263-271`) today calls `abandonRun` (`run.go:157-174`) when a POST arrives on a thread with a live run, and two requests can both pass that check and race into `svc.Run`, where the loser gets `agent.ErrSessionBusy` surfaced as `RUN_ERROR{session_busy}` (`server.go:333-338`, `:542-552`). Close the window: take the per-thread decision under one lock so exactly one run exists per thread, and give the loser a single documented error rather than a race. `runStore.put` is last-write-wins (`run.go:118-122`) while `remove` is identity-checked (`run.go:126-132`) — keep the identity check and make `put` conditional.

Do NOT abandon a live run just because a new POST arrived; a POST with a new user message on a busy thread is the loser case, not a reason to cancel. Do NOT replay from an unbounded buffer, and do NOT let a read-only follower resume a run suspended on a frontend tool.

## Acceptance Criteria

- [ ] Reconnecting mid-run yields a consistent event sequence: no duplicate `TOOL_CALL_END`, no orphaned `TOOL_CALL_ARGS`, and the run continues live after replay (`internal/agui/server_test.go`).
- [ ] A second tab attaches read-only to the same run, receives the same live events, and cannot submit tool results.
- [ ] Two concurrent `POST`s on one `threadId` produce exactly one run; the loser receives one documented error, and a repeated race test shows no `RUN_ERROR{session_busy}` from the TOCTOU path.
- [ ] Attaching to a thread with no live run answers a documented 404/409 rather than starting one.
- [ ] A run parked by a disconnect is unparked by a reattach and its grace timer is cleared.

## Notes

Depends on the disconnect/park story (PANDO-EP-0003) for the parking behaviour and the event buffer. The trickiest part is replay semantics — budget for it. Until this ships, the documented client-side workaround is one thread per tab plus per-thread serialization in the embedding proxy. Size **L**.
