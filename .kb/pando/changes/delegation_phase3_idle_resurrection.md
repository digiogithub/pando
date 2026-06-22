---
created_at: 2026-06-21T12:38:57.486847184Z
updated_at: 2026-06-21T12:38:57.486847184Z
tags:
    - change
    - mesnada
    - delegation
    - phase3
    - resurrection
    - agent
    - supervisor
    - orchestrator
---
# Change: Delegation Phase 3 — Case B idle-loop resurrection

Implemented 2026-06-21 (delegated subagent + parent review/cleanup). Status: DONE,
verified. Part of plan `pando/plans/delegated_conclusion_resurrection_plan.md`
(Phase 3). Builds on Phases 0-2. **Default-OFF.** With this phase, ALL delegation
config flags are consumed at runtime; only the settings UI (Phase 5) remains.

## What & why
Case B: when a delegated task completes while its parent agent session is IDLE (the
loop already ended), an event-driven supervisor resurrects the parent with a new
system-initiated turn carrying the conclusion(s). Near-simultaneous sibling
completions are batched into ONE resurrection.

## Agent — `Resume` entrypoint + resurrection budget (`internal/llm/agent/agent.go`)
- New event type `AgentEventTypeResurrected` ("resurrected").
- `Run` refactored: extracted `runInternal` (shared machinery). `Run` (user) now
  just `clearResurrectionCount` then `runInternal` — user behavior byte-for-byte
  unchanged.
- New `Resume(ctx, sessionID, content) error` (Service interface + impl): rejects
  empty/busy (`ErrSessionBusy`), publishes the Resurrected system event so UIs can
  frame the turn, calls `runInternal`, increments the per-session resurrection
  counter, and DRAINS the returned channel in a goroutine (events still flow to UIs
  via the pubsub broker; draining stops the buffered(512) run channel from blocking
  the run goroutine when unread).
- Per-session resurrection counter `resurrectCount map[string]int` (+`resurrectMu`):
  incremented by `Resume`, reset by user `Run`, cleared by `Cancel`. Exposed via
  `ResurrectionCount(sessionID) int` (Service interface) so the supervisor enforces
  `MaxResurrections` with automatic reset on the next user message.
- Mocks updated: `steerMockAgent` (handlers_steer_test.go), `stubGoalService`
  (goal_runner_test.go) gained `Resume`+`ResurrectionCount`; `modelUpdateMockAgent`
  inherits via embedding.

## Supervisor — Case B batching (`internal/app/delegation_supervisor.go`)
- `delegationAgent` interface broadened with `Resume`+`ResurrectionCount`; new
  `parentLister` interface (`ListByParentSession`) so the outstanding-sibling check
  is stubbable. Options broadened: `ResurrectIdleLoop`, `MaxResurrections`,
  `MaxDepth`, `ResurrectionTimeout time.Duration`.
- `handleCompletion`: gate → require ParentSessionID+Conclusion → dedupe by
  CorrelationID → if `IsSessionBusy` → `injectLive` (Case A, unchanged) → else
  `enqueueResurrection` (Case B).
- `enqueueResurrection`: depth cap (`task.Depth >= MaxDepth` → skip) → budget cap
  (`ResurrectionCount >= MaxResurrections` → skip) → mark seen + append to the
  session's `sessionBatch`. Then `hasOutstandingSiblings` (via
  `ListByParentSession`, counts pending/running/paused): if any outstanding → arm/
  reset a `ResurrectionTimeout` debounce timer; if none → `flush` now.
- `flush(session)`: takes+clears the batch under mutex, re-checks the budget cap,
  builds combined content (`buildResurrectionContent`: system preamble + one
  `conclusion.FormatForParent` per task), calls `agent.Resume`. On `ErrSessionBusy`
  race → falls back to `InjectConclusion` (Case A) so results aren't lost.
- `Start` gate now `Enabled && (InjectIntoLiveLoop || ResurrectIdleLoop)`; `Stop`
  stops pending timers and drops un-flushed batches cleanly (no resurrection across
  shutdown). Mutex guards `seen`+`batches`; I/O (`ListByParentSession`) runs outside
  the lock.

## Depth propagation (so MaxDepth works)
- `spawner_pando_cli.go` (and `spawner_acp.go`): append
  `PANDO_DELEGATION_DEPTH=<task.Depth>` to the child process env.
- `internal/llm/tools/mesnada.go` `spawnCorrelation`: reads
  `PANDO_DELEGATION_DEPTH` (default 0) → sets `SpawnRequest.Depth = parent+1`
  (threaded onto the Task). Top-level spawn = depth 1, its children = depth 2, etc.

## MaxConcurrent soft guard (spawn tool)
- In `mesnada.go` spawn, gated on `Delegation.Enabled && MaxConcurrent>0 &&
  ParentSessionID!=""`: counts non-terminal correlated siblings via
  `ListByParentSession`; at/over the cap returns an informative `NewTextErrorResponse`
  nudging the model to wait instead of spawning more. New public
  `Orchestrator.ListByParentSession` pass-through added.

## Wiring (`app.go`)
- Supervisor options populated from `config.Get().Mesnada.Delegation`;
  `ResurrectionTimeout` parsed via `time.ParseDuration` (10m fallback). Start gate
  updated to include ResurrectIdleLoop.

## Deliberate decision
- `processGeneration`'s hot path / end-of-turn `Done` return was NOT modified. The
  event-driven supervisor (resurrect on completion when idle) fully covers the
  "loop ended but work pending" case with far less risk than gating the loop-end
  return. (Stated as a deliberate simplification of the plan's Phase 3.)

## Tests
- `internal/llm/agent/resume_test.go`: Resume starts an idle run + drains; busy →
  ErrSessionBusy; ResurrectionCount increments on Resume, resets on Run, cleared by
  Cancel.
- `internal/app/delegation_supervisor_test.go`: idle + no siblings → one Resume with
  combined content; depth cap blocks; budget cap blocks after N; batching coalesces
  two completions into one Resume when a sibling is outstanding then terminal;
  ErrSessionBusy → InjectConclusion fallback. (`fakeLister` stubs ListByParentSession.)
- `mesnada_correlation_test.go`: depth read from env → Depth=parent+1.

## Verification (parent-run)
- `go build ./...` → clean. (Editor diagnostic re `modelUpdateMockAgent` missing
  Resume was STALE — inherits via embedding.)
- `go vet` → only the 2 pre-existing spawner_template.go warnings (untouched).
- `gofmt`: parent applied `gofmt -w` to `orchestrator.go` (a Phase-2 struct-field
  alignment leftover from adding `completionSubs`/`completionMu`); all our files now
  gofmt-clean.
- `go test -count=1 ./internal/llm/agent ./internal/llm/tools ./internal/mesnada/...
  ./internal/app ./internal/api ./internal/config` → all pass. `go test -race
  ./internal/app` → pass.

## Residual limitations / notes for Phase 4/5
- **UI**: `AgentEventTypeResurrected` is published but TUI/WebUI don't render it
  distinctly yet — Phase 5 should frame "resuming because a delegated task finished".
- **Config**: every delegation flag is now live; Phase 5 only needs to expose
  toggles (TUI/WebUI/API) — no new threading.
- **MaxResurrections** auto-resets on user `Run`, so it bounds runaway per user
  turn-chain, not over a session's whole lifetime (a session repeatedly poked by the
  user could resurrect again each turn). MaxDepth is the cross-chain guard.
- **Relaunch** preserves `Depth` (no increment) — depth caps won't trip on relaunch.
- **Narrow timer race** (theoretical): if the debounce timer fires in the exact
  microsecond window between a concurrent enqueue's append and its flush-decision, a
  single conclusion could miss its batch. Practically irrelevant at the 10-min
  default timeout, and degrades gracefully (the conclusion stays persisted on the
  task, retrievable via get_task). Not fixed (a fix would hold the mutex over store
  I/O). Phase 4's await_task gives the model an explicit alternative path.
