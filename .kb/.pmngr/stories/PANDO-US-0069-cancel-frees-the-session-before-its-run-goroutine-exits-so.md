---
id: PANDO-US-0069
type: story
title: Cancel frees the session before its run goroutine exits, so a new run can overlap and lose its busy marker
status: backlog
priority: medium
author: mcp
labels: [agent, concurrency, race]
estimate: 3
created: 2026-09-29T18:52:06Z
updated: 2026-09-29T18:52:06Z
---

## Description

As a user who cancels a turn and immediately sends a new message, I want the new run to start only after the cancelled one has actually stopped, and to stay marked busy for as long as it runs, so that two runs never operate on the same session at once and model switches or steering never see a running session as idle.

`(*agent).Cancel` (`internal/llm/agent/agent.go:525`) does `activeRequests.LoadAndDelete(sessionID)` and cancels the context, but the run goroutine started in `runInternal` (`agent.go:958`) keeps executing until `processGeneration` notices the cancellation. During that window:

1. `IsSessionBusy(sessionID)` (`agent.go:782`) already reports false, so `Run`/`Resume` accept a new run for the same session while the cancelled one is still writing messages, tool results and snapshots (`session.RecordSnapshot`).
2. The new run stores its own cancel func under the same key. When the old goroutine finishes, its cleanup (`agent.go:979` deferred and `agent.go:1003`) calls `activeRequests.Delete(sessionID)` unconditionally and removes the **new** run's entry. The new run is then invisible: `IsSessionBusy` is false, `Cancel` cannot stop it, `Steer` rejects with `ErrSessionNotBusy`, and model switches are no longer blocked.
3. The same applies to `clearSteering(sessionID)` in the old goroutine's cleanup, which can drop steering messages queued for the new run.

The `-summarize` key (`agent.go:533`, `agent.go:2161`) has the same shape.

Found while fixing PANDO-US-0032 (see `.kb/pando/fixes/agent_run_goroutine_leak_race.md`), which added `agent.runs` (a WaitGroup over all run goroutines) but deliberately left per-session ownership alone.

## Acceptance Criteria

- [ ] A run's cleanup only removes the `activeRequests` entry it created (for example by storing a per-run token/pointer and using `CompareAndDelete`, since cancel funcs are not comparable), and only clears steering that belongs to it.
- [ ] Decide and document whether `Cancel` keeps the session busy until the goroutine exits, or whether a new `Run` waits for (or is rejected until) the cancelled run's completion; the chosen behaviour is stated in a comment on `Cancel`.
- [ ] Regression test: Cancel, then immediately Run on the same session while the first run is still blocked in a stub provider; after the first goroutine exits, `IsSessionBusy` is still true for the second run and `Cancel` still stops it.
- [ ] Same guarantee for the `-summarize` entry.
- [ ] `go test -race -count=20 ./internal/llm/agent` stays green.

## Notes

Follow-up of PANDO-US-0032. Check callers that rely on `IsSessionBusy` right after `Cancel` (ACP `session/cancel`, WebUI stop button, delegation supervisor `Resume`) so a stricter busy window does not surface as spurious `ErrSessionBusy`.
