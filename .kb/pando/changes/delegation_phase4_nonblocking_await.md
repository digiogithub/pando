---
created_at: 2026-06-21T16:21:15.861139746Z
updated_at: 2026-06-21T16:21:15.861139746Z
tags:
    - change
    - mesnada
    - delegation
    - phase4
    - await
    - non-blocking
    - supervisor
    - orchestrator
    - tools
---
# Change: Delegation Phase 4 — NON-BLOCKING await primitive

Implemented 2026-06-21 (delegated subagent + parent review). Status: DONE,
verified. Part of plan `pando/plans/delegated_conclusion_resurrection_plan.md`
(Phase 4, REFRAMED). Builds on Phases 0-3. **Default-OFF.**

## Reframing (important)
The plan's original Phase 4 described `await_task`/`await_any` as "a thin wrapper
over Orchestrator.Wait/WaitMultiple" — i.e. BLOCKING. That was explicitly REJECTED
by the project owner: the main agent loop must NEVER busy-poll or block waiting for
a subagent. Instead, when the agent has nothing else to do it must END ITS TURN and
be RESUMED (Phase 3) when the awaited subagent(s) complete — an event delivers the
result without polling.

So Phase 4 is a **non-blocking await primitive**: register a wait intent + tell the
model to finish; the supervisor resurrects when the join condition is met. No
`Orchestrator.Wait`, no polling, no holding the loop open.

## Await-intent registry (`internal/mesnada/orchestrator/await.go`, new)
- `AwaitPolicy` ("all"|"any"|"quorum") + `AwaitIntent{SessionID, TaskIDs, Policy,
  Quorum, Deadline, CreatedAt}`.
- On `*Orchestrator` (new `awaitIntents map[string]*AwaitIntent` + `awaitMu`):
  `SetAwaitIntent`, `AwaitIntentFor`, `ClearAwaitIntent`. In-memory, keyed by
  session (mirrors supervisor batch state; not persisted — restart drops intents,
  conclusions stay durable on the task).

## The `mesnada_await` tool (`internal/llm/tools/mesnada_await.go`, new) — NON-BLOCKING
- Params: `task_ids` (optional; empty = all currently-outstanding correlated tasks),
  `policy` (all|any|quorum, default all), `quorum` (required for quorum), `timeout`
  (safety deadline, default 1h).
- Run: reads sessionID from ctx; resolves+validates the awaited set (rejects foreign
  task ids); **short-circuits** when the join condition is ALREADY satisfied —
  returns the conclusions immediately via `conclusion.FormatForParent` (no intent
  registered, no deadlock); otherwise `SetAwaitIntent` + returns an unambiguous
  "END YOUR TURN now, do not poll, you will be AUTOMATICALLY RESUMED" message with
  `end_turn:true` metadata. Never blocks, never calls Orchestrator.Wait.
- Registered in `internal/llm/agent/tools.go` ONLY when
  `Delegation.Enabled && ResurrectIdleLoop` (await relies on resurrection; else the
  model could deadlock). `mesnada_spawn` description updated to nudge toward
  await+end-turn over polling with get_task/list_tasks. Blocking `mesnada_wait_task`
  kept for short foreground waits.
- Uses a narrow `awaitOrchestrator` interface for unit-testability.

## Supervisor — await-aware resurrection (`internal/app/delegation_supervisor.go`)
- New `awaitReader` interface (`AwaitIntentFor`/`ClearAwaitIntent`), wired from the
  concrete orchestrator (`s.awaits`).
- `enqueueResurrection`: always appends the completed task's conclusion to the
  session batch first (nothing lost, even incidental non-awaited completions). If an
  await intent exists → `handleAwaitCompletion` (SUPPRESSES the Phase-3
  per-completion/debounce flush); else the existing Phase-3 path runs unchanged.
- `handleAwaitCompletion`: evaluates `awaitJoinSatisfied` (all/any/quorum over
  current statuses; any terminal state = "reported", so failed tasks count) and the
  deadline. If satisfied OR deadline passed → `ClearAwaitIntent` + `flushAwait`
  (Resume with ALL accumulated conclusions). Else arms a SINGLE safety timer at the
  deadline (so a stuck awaited task can't hang the session) and waits — no
  resurrection, no polling.
- `flushWith` extracted as the shared flush core (cap re-check + Resume/ErrSessionBusy
  →InjectConclusion fallback) reused by Phase-3 `flush` and new `flushAwait`.
  `buildAwaitResurrectionContent` frames satisfied-vs-timed-out wakes. `Stop`/flush
  stop the await timer too.

## Deadlock safety (two independent guards)
1. Tool short-circuit: if already satisfied at call time → return results now (model
   never ends its turn waiting for a wake that won't come).
2. Supervisor deadline timer: a never-completing awaited task still wakes the parent
   after the timeout with whatever completed.

## Tests
- `internal/mesnada/orchestrator/await_test.go`: set/get/clear, replace, nil/empty,
  concurrency.
- `internal/llm/tools/mesnada_await_test.go`: session required; empty→outstanding;
  already-satisfied→conclusions (no intent); no-outstanding short-circuit;
  foreign-id rejected; quorum validation; pending→intent+end-turn; invalid policy.
- `internal/app/delegation_supervisor_await_test.go`: policy=all (resurrect only on
  2nd completion, both conclusions, intent cleared); policy=any (1st completion);
  deadline-passed flush with timeout framing; no-intent → Phase-3 unchanged.

## Verification (parent-run, full)
- `go build ./...` → clean.
- `gofmt -l` on every touched file → clean (the only repo gofmt offender is a
  PRE-EXISTING alignment issue in the top-level `Config` struct in config.go,
  present in HEAD, unrelated to this work — left untouched to avoid scope creep).
- `go vet` (all affected pkgs) → only the 2 pre-existing spawner_template.go warnings.
- `go test ./...` (FULL repo, ~55 packages) → all `ok`, exit 0.
- `go test -race ./internal/app ./internal/mesnada/orchestrator` → pass.

## Notes for Phase 5 (UI/config)
- No new config knobs: await reuses the existing `Mesnada.Delegation` flags; per-call
  policy/quorum/timeout are model-supplied. Phase 5 only needs to expose the existing
  delegation toggles (TUI/WebUI/API) + i18n.
- Tool gating is at registration time (`Enabled && ResurrectIdleLoop`); live
  hot-reload of these flags would need the agent tool set rebuilt (same caveat as
  other config-gated tools).
- Optional UI: surface per-session `AwaitIntentFor` ("awaiting N tasks, policy=…,
  deadline=…"); `AgentEventTypeResurrected` is the wake hook — Phase 5 could
  differentiate await-satisfied / await-timeout / opportunistic resurrection.
- Restart drops in-memory await intents (conclusions remain durable).
