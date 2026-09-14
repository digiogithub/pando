---
id: PANDO-US-0017
type: story
title: "Survive client disconnect: park the run with a grace period instead of cancelling it"
status: done
priority: high
parent: PANDO-EP-0003
milestone: PANDO-M-0001
author: claude
labels: [agui, api]
estimate: 5
created: 2026-09-13T21:14:53Z
updated: 2026-09-13T21:14:53Z
---

## Description

As a user on a flaky network, I want my agent's turn to keep running when the stream drops, so that closing a tab or crossing a proxy idle timeout does not throw away work already paid for.

`internal/agui/server.go:407-418` selects on the **request** context and calls `finishRun(run)` on `ctx.Done()`, which reaches `run.stop()` and cancels the run context (`internal/agui/run.go:86-99`, `:146-152`). Runs are already parented to `r.baseCtx`, not the request (`internal/agui/runtime.go:40-43`, `server.go:330`), so the change is contained: on request-context cancellation, park the run instead — the parking machinery already exists at `run.go:63-83` (`park`/`unpark`) — and start a grace timer. On expiry with no reattach, tear the run down through the existing cancel path. Add the grace period as a config key next to the other AG-UI timings, with a sane default, and keep the existing suspended-run reaper (`run.go:28`, `suspendGrace`) as a separate lifetime.

Also add the event buffer the reattach story will replay from, sized and bounded here so a parked run with nobody listening cannot grow without limit: when the buffer is full, drop oldest and mark the buffer lossy.

Do NOT keep the parked run alive forever, do NOT let the parked goroutine hold the session busy past teardown, and do NOT change what happens on an explicit client abort once the cancel endpoint exists — an abort must still cancel, a disconnect must not.

## Acceptance Criteria

- [ ] Closing the stream mid-run leaves the agent running; the turn completes and its messages are persisted and visible through `GET {path}/threads/{id}/messages` afterwards (`internal/agui/run_test.go`).
- [ ] A parked run with no reconnect inside the grace period is torn down, and a goroutine-leak assertion passes after teardown.
- [ ] The event buffer is bounded; a run parked past the buffer limit marks itself lossy rather than growing.
- [ ] The grace period is configurable and its default is documented; the suspended-run reaper's lifetime is unchanged.

## Notes

Depends on the thread-API story (PANDO-EP-0003) only for the acceptance check that messages are readable afterwards. Blocks the reattach story, which consumes the buffer this story introduces. Reference implementation for the buffered replay: the Web-UI stream at `internal/api/handlers_chat.go:112` (route `internal/api/routes.go:30`). Size **M**.
