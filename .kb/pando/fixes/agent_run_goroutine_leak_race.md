---
created_at: 2026-09-29T18:45:03.589375386Z
updated_at: 2026-09-29T18:45:03.589375386Z
tags:
    - fix
    - agent
    - race
    - testing
---
# Fix: agent run goroutine leak and `-race` failure in internal/llm/agent (PANDO-US-0032)

Date: 2026-09-29. Story: PANDO-US-0032 (supersedes cancelled PANDO-T-0005). Related: [[agui_mcp_tool_permission_binding]], [[plan_delegated_conclusion_resurrection]].

## Symptom
`go test -race ./internal/llm/agent` failed intermittently with `race detected` in unrelated tests (`TestSessionModelIDFollowsOverride`, `TestAttachSessionMCPServersExposesToolsToThatSessionOnly`): `config.SetForTests` in one test raced with `effectiveContextWindow` -> `config.Get()` read by a run goroutine (`runInternal`) started by an earlier test in `resume_test.go`.

## Root causes
1. Tests (`resume_test.go`) called `a.Cancel(...)` and returned without joining the run goroutine. `Cancel` only signals; the goroutine keeps running and reading global config.
2. **Production leak**: on the panic path of the `runInternal` goroutine, `logging.RecoverPanic`'s handler sent the error to `events` but `close(events)` was only on the normal path. Any consumer ranging over `events` — `Resume`'s internal drain goroutine included — blocked forever. With the stub test agent the run panics, so each `Resume` test leaked a drain goroutine permanently.

## Changes
- `internal/llm/agent/agent.go`
  - `agent.runs sync.WaitGroup`: `runInternal` does `a.runs.Add(1)` before starting the goroutine; `defer a.runs.Done()` is the first defer (runs last).
  - `defer close(events)` registered before `RecoverPanic` so the channel is closed on both normal and panic paths (after the recovered error is sent). Removed the trailing `close(events)`.
  - `(*agent).waitForRuns()` unexported helper that waits on `runs` (does not cancel).
- `internal/llm/agent/resume_test.go`
  - `newResumeTestAgent(t)` registers `t.Cleanup(waitForRunsOrFail)`.
  - `waitForRunsOrFail` joins runs with a 5s timeout, then scans `runtime.Stack(all)` for live `(*agent).runInternal.func*` / `(*agent).Resume.func*` goroutines (goleak-style check, no new dependency; package tests are not parallel).
  - New `TestCancelledRunGoroutineTerminates`.

Not done (by design per story): no `-p 1`, no dropping `-race`.

## Verification
- `go test -race -count=20 ./internal/llm/agent` green (before: races in 1-6 of 5 runs).
- `go test -race -count=1 ./internal/llm/agent ./internal/api` green; `go build ./...` OK.
- Mutation checks: removing the deferred `close(events)` makes the leak check fail in 3 tests ("agent run goroutine leaked past the test").

## Follow-up worth noting
`Cancel` does `activeRequests.LoadAndDelete` immediately, so the session reads as not busy while the cancelled goroutine is still running; a new `Run` can start and the old goroutine's deferred `activeRequests.Delete(sessionID)` may then remove the new run's entry. Not addressed here.
