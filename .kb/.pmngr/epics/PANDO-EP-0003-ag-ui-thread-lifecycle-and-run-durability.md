---
id: PANDO-EP-0003
type: epic
title: AG-UI thread lifecycle and run durability
status: done
priority: high
milestone: PANDO-M-0001
labels: [agui, api]
created: 2026-09-13T21:11:26Z
updated: 2026-09-13T21:11:26Z
---

## Description

The AG-UI adapter exposes exactly two routes, `GET {path}/info` and `POST {path}/{agent}` (`internal/agui/server.go:26-33`). There is no way to list threads, read a thread's messages, delete a thread or re-attach to a run in progress: `agui_threads` offers `get`, `put` and `forget` but no `list`, and `NewMessagesSnapshot` has zero call sites. Worse for a browser client, cancelling the request context calls `finishRun` (`server.go:412-417`), so a dropped stream kills the agent's turn, and a second `POST` on the same `threadId` abandons the first (`server.go:263-271`), with a TOCTOU window that answers `RUN_ERROR{session_busy}`. A suspended run (parked on a permission prompt) cannot be cancelled at all except by the eleven-minute reaper.

This epic gives the adapter the lifecycle a web product needs: thread API, in-band transcript resync, a run that survives its client, reattach with replay, and explicit cancellation. Runs are already parented to the adapter's base context rather than the HTTP request, which keeps the disconnect work contained.

## Acceptance Criteria

- [ ] `GET {path}/threads` (paginated, scoped to this adapter), `GET {path}/threads/{id}/messages` (AG-UI `Message[]` shape with `toolCalls`/`toolCallId`) and `DELETE {path}/threads/{id}` (removes messages, session and the `agui_threads` row) exist, go through `authorize()` and work on the dedicated `agui-serve` listener with no REST API co-mounted.
- [ ] The first run on a thread that already has a session emits `MESSAGES_SNAPSHOT` right after `STATE_SNAPSHOT`, size-capped, and only when the thread pre-existed.
- [ ] Closing the stream mid-run parks the run instead of cancelling it; the turn completes and its messages are persisted; a parked run with no reconnect within a configurable grace period is torn down and leaks no goroutine.
- [ ] `GET {path}/threads/{id}/stream` (or a `POST` with no new user message) re-joins a live run, replays buffered events then continues live, with no duplicated `TOOL_CALL_END` and no orphaned `TOOL_CALL_ARGS`; a second tab may follow read-only.
- [ ] Two concurrent `POST`s on one `threadId` produce exactly one run and a documented error for the loser, with no race.
- [ ] `POST {path}/runs/{threadId}/cancel` cancels live and suspended runs, emits `RUN_ERROR{code:"cancelled"}` to any attached stream and is idempotent.

## Notes

Decision of 2026-09-13: fund this in Pando rather than have the embedding host own replay state. The host will still persist its own `threadId` per conversation and may reuse a Pando session id as `threadId`, which `runtime.go:138-144` honours. The Web-UI's buffered replay in `internal/api/handlers_chat.go:112` is the reference implementation to port. Evidence in `report-agui-server.md` §2 and `report-sdk-agui-client.md` §3.
