---
created_at: 2026-06-21T11:48:35.253703555Z
updated_at: 2026-06-21T11:48:35.253703555Z
tags:
    - change
    - mesnada
    - delegation
    - phase2
    - steering
    - agent
    - supervisor
    - orchestrator
---
# Change: Delegation Phase 2 — Case A live-loop conclusion injection

Implemented 2026-06-21 (delegated subagent + parent review). Status: DONE,
verified. Part of plan `pando/plans/delegated_conclusion_resurrection_plan.md`
(Phase 2). Builds on Phases 0+1. **Default-OFF**: with
`Mesnada.Delegation.Enabled=false` OR `InjectIntoLiveLoop=false` nothing
subscribes and runtime behavior is unchanged.

## What & why
Case A of the delegation protocol: when a delegated task completes while its parent
agent session is STILL running, inject the task's enriched `Conclusion` into that
live loop via the existing steering inbox (generalized to a second event class), so
the parent continues with the subagent's findings without the user resending.

## Agent loop — steering inbox generalized (`internal/llm/agent/agent.go`)
- `steeringMessage` gained a `kind steeringKind` field (`steeringFeedback` zero
  value | `steeringConclusion`). The existing user-feedback `Steer` path is
  byte-for-byte unchanged (kind defaults to feedback).
- New event types `AgentEventTypeConclusionQueued` ("conclusion_queued") /
  `AgentEventTypeConclusionInjected` ("conclusion_injected").
- New `Service` method `InjectConclusion(sessionID, content string) error`: mirrors
  `Steer` but enqueues kind=conclusion, no attachments, content used verbatim (the
  supervisor pre-formats it), emits ConclusionQueued; returns `ErrSessionNotBusy`
  when idle (caller decides — Phase 3 resurrection).
- `drainSteeringInto` now counts per kind and emits `AgentEventTypeSteeringInjected`
  for feedback and/or `AgentEventTypeConclusionInjected` for conclusions. Same two
  safe boundaries (agent.go ~865 after tool results, ~893 end of turn); the
  "never between tool_call/tool_result" guarantee is preserved.
- Interface broadened: `InjectConclusion` added to `agent.Service` (only `*agent`
  implements it). Three test mocks updated: `handlers_steer_test.go` (steerMockAgent),
  `goal_runner_test.go`; `modelUpdateMockAgent` inherits via embedding steerMockAgent.

## Conclusion framing (`internal/mesnada/conclusion/format.go`, new)
- `FormatForParent(task) string`: compact pointers-not-dumps message. Header
  `[delegated-result task=… engine=… project=… status=… confidence=…]` uses ONLY
  software-owned metadata (id/engine/project label); body carries summary +
  optional follow_up/artifacts/memory_refs (each omitted when empty). `projectLabel`
  fallback chain: ProjectName → base(ProjectPath) → base(WorkDir) → "unknown".
  Nil-safe (no Conclusion → single minimal line).

## Orchestrator — global completion broadcast (`orchestrator.go`)
- New `completionSubs []chan *models.Task` + `completionMu`. `SubscribeCompletions()
  (<-chan *models.Task, func())` registers a cap-16 buffered channel and returns an
  idempotent (`sync.Once`) unsubscribe. `broadcastCompletion` sends non-blocking
  (drop if full) and is invoked in `onTaskComplete` at line 197 — AFTER
  `captureConclusion` (185) so the delivered `*Task` already carries its enriched
  `Conclusion`, and independent of the existing per-task `subscribers` (used by Wait).

## Supervisor (`internal/app/delegation_supervisor.go`, new — package app)
- `delegationSupervisor` with a narrow `conclusionInjector` interface
  (`IsSessionBusy` + `InjectConclusion`) so the decision core is unit-testable.
- `Start(ctx)`: no-op unless `Enabled && InjectIntoLiveLoop`; otherwise subscribes
  to `SubscribeCompletions` and runs a goroutine tied to the app ctx. `Stop()`
  unsubscribes + waits.
- `handleCompletion(task)` (pure core): gate → require ParentSessionID + Conclusion
  → dedupe by CorrelationID (fallback task.ID) → if `IsSessionBusy` then
  `InjectConclusion(parentSession, FormatForParent(task))`. `ErrSessionNotBusy`
  race = clean no-op. `seen` is marked ONLY on successful injection (transient
  failures can retry). Idle session → skip (Phase 3 territory).
- Wired in `app.go`: constructed+Started after `CoderAgent` (app.go:619-627) with
  options from `config.Get().Mesnada.Delegation`; `Stop()` in `Shutdown` (1859-1860).

## Tests
- `internal/llm/agent/inject_conclusion_test.go`, `internal/mesnada/conclusion/
  format_test.go`, `internal/mesnada/orchestrator/completions_test.go`,
  `internal/app/delegation_supervisor_test.go` (busy→inject once, dedupe, gated-off,
  not-busy→skip).

## Verification (parent-run, forced)
- `go build ./...` → clean. (Editor diagnostic claiming `modelUpdateMockAgent`
  missing InjectConclusion was STALE — it inherits via embedding; package compiles.)
- `go vet ./internal/llm/agent ./internal/mesnada/... ./internal/app` → clean
  (only the 2 pre-existing spawner_template.go warnings, untouched).
- `go test -count=1 ./internal/llm/agent ./internal/mesnada/... ./internal/app
  ./internal/api ./internal/config` → all pass.
- Parent cleanup this phase: none needed; code quality was high.

## Notes for Phase 3 (idle-session resurrection)
- Hook the SAME stream: the `!IsSessionBusy` branch in `handleCompletion` (currently
  a debug skip) is where resurrection belongs, gated by `ResurrectIdleLoop`.
- Needs: a first-class `agent.Resume(ctx, sessionID, ResumeReason{...})` entrypoint
  (distinct from Run/steering); the agent.go end-of-turn `Done` gate to record
  "outstanding correlated tasks" intent; threading `ResurrectIdleLoop`,
  `MaxResurrections`, `MaxDepth`, `MaxConcurrent`, `ResurrectionTimeout` into the
  supervisor (plain values) and possibly orchestrator DelegationConfig.
- For batched sibling conclusions (per ResurrectionTimeout), use the deferred
  `subscribersBySession` index / `ListByParentSession` (Phase 0). The supervisor's
  `seen` map only grows — Phase 3 may want session-scoped tracking + eviction.
