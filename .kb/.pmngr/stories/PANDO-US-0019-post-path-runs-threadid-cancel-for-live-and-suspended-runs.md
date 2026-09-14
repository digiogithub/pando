---
id: PANDO-US-0019
type: story
title: POST {path}/runs/{threadId}/cancel for live and suspended runs
status: in_progress
priority: medium
parent: PANDO-EP-0003
milestone: PANDO-M-0001
author: claude
labels: [agui, api]
estimate: 3
created: 2026-09-13T21:14:53Z
updated: 2026-09-13T21:14:53Z
---

## Description

As a user, I want a Stop button that works even while the agent is waiting on a permission prompt, so that I am not stuck until a reaper fires eleven minutes later.

There is no cancel route today (`internal/agui/server.go:26-33`); cancelling means aborting the HTTP request, which only works while a stream is attached. A **suspended** run has no open request at all and dies only after `suspendGrace` (`internal/agui/run.go:28`), and cancelling from another tab is impossible. Add `POST {path}/runs/{threadId}/cancel` behind `authorize()`: look the run up in `runStore` (`run.go:111-117`), call the existing `stop()` (`run.go:86-99`) for a live or parked run, and for a suspended one release whatever it waits on (the pending frontend-tool/permission wait) before cancelling, so no goroutine is left blocked. Emit `RUN_ERROR{code:"cancelled"}` to every attached stream, including read-only followers, and close them cleanly.

Do NOT reuse the `session_busy` code, do NOT leave the `agui_threads` binding or the session behind (cancel ends the run, not the thread), and do NOT make cancellation depend on a stream being attached.

## Acceptance Criteria

- [ ] Cancelling a run parked on a permission prompt returns promptly and the run is gone from `runStore`; no goroutine remains blocked (`internal/agui/run_test.go`).
- [ ] Every attached stream, live attach and read-only follower alike, receives `RUN_ERROR{code:"cancelled"}` and is closed.
- [ ] The endpoint is idempotent: a second cancel, and a cancel on an unknown or already-finished thread, answer success without error.
- [ ] Cancelling from a request other than the one streaming the run works.
- [ ] The thread remains listable and its messages readable after cancellation.

## Notes

Depends on the disconnect/park story for parked-run handling and on the reattach story for read-only followers (both PANDO-EP-0003); implement last in the epic. Size **S**.
