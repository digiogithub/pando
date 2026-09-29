---
created_at: 2026-09-29T19:24:18.711292905Z
updated_at: 2026-09-29T19:24:18.711292905Z
tags:
    - fix
    - agent
    - concurrency
---
# Fix: Cancel freed a session before its run exited (PANDO-US-0069)

Date: 2026-09-29. Follow-up of [[pando/fixes/agent_run_goroutine_leak_race.md]] (PANDO-US-0032). Implemented by a Sonnet subagent, reviewed and re-verified in the main session.

## Defect
`(*agent).Cancel` did `activeRequests.LoadAndDelete(sessionID)` while the cancelled run goroutine kept running. In that window `IsSessionBusy` was false so a second run could start on the same session; the old goroutine's cleanup then did an unconditional `activeRequests.Delete(sessionID)` + `clearSteering`, removing the NEW run's entry (invisible to IsSessionBusy/Cancel/Steer/model-switch guard) and its steering. Same shape for the `-summarize` key.

## Final behaviour (`internal/llm/agent/agent.go`)
- `activeRun` value in `activeRequests`: cancel func, `cancelled atomic.Bool`, `done chan` (closed by `markDone`, first defer of the run/summarize goroutine, so it fires after the entry is removed and `runs.Done`).
- `Cancel` marks the run(s) cancelled and calls cancel; it no longer deletes the entry.
- `IsSessionBusy` = registered AND not cancelled. So after Cancel, `Steer`/`InjectConclusion` return `ErrSessionNotBusy` and TUI (`internal/tui/page/chat.go`) / ACP (`internal/mesnada/acp/agent.go`) fall through to `Run` instead of queueing the new message as steering on the dying run (which would be lost — regression caught in review of the first iteration).
- `IsBusy` (model-switch guard) still counts cancelled-but-unwinding runs.
- `acquireRun(ctx, key, cancel)`: atomic registration via `LoadOrStore` loop; live run -> `ErrSessionBusy`; cancelled run -> wait on its `done` bounded by `cancelledRunWait` (10s, package var) and ctx, then retry; timeout -> `ErrSessionBusy`, ctx -> `ctx.Err()`. Replaces the racy IsSessionBusy-then-Store in `runInternal`.
- Cleanup: `finishRun` uses `CompareAndDelete` with the run's own pointer and clears steering only if it still owned the entry.
- `SummarizeStream` waits for a cancelled main run (`waitCancelledRun`), then `acquireRun` on `-summarize` (a second concurrent summarize now gets ErrSessionBusy instead of silently overwriting).

Callers of Cancel reviewed (ACP session/cancel+close, delegation CancelDelegation, TUI chat, context enricher timeout, IPC interrupt, appACPAgentAdapter, API goal cancel): none needs changes.

## Tests
`internal/llm/agent/cancel_registration_test.go`: cancelled run not steerable but counted by IsBusy; Run waits for cancelled run to unwind with no overlap and the new run is visible/cancellable; wait timeout -> ErrSessionBusy and ctx deadline -> DeadlineExceeded; finishing run keeps newer entry and its steering; summarize equivalents. `markBusy` helper in `steering_test.go` uses `acquireRun`.

Verified: `go test -race -count=20 ./internal/llm/agent` ok; `go test ./internal/api ./internal/mesnada/... ./internal/tui/... ./internal/app` ok; `go build ./...`, `go vet` clean.

## Remaining risks
- A cancelled run that never exits makes a follow-up Run block up to 10s, then ErrSessionBusy.
- `IsSessionBusy` ignores the `-summarize` key (pre-existing).
- After Cancel, a model switch is rejected until the old run has actually exited (intended).
