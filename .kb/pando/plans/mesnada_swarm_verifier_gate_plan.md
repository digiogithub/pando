---
created_at: 2026-07-23T13:55:51.078297099Z
updated_at: 2026-07-23T13:55:51.078297099Z
tags:
    - plan
    - mesnada
    - delegation
    - orchestration
    - swarm
    - verifier
    - gate
    - coordination
---
# P3 plan — mesnada verifier gate + `mesnada_swarm` topology helper

Design for **P3** from [[hermes-kanban-swarm-vs-pando-delegation]]: a first-class
fan-out → **verify (gate)** → synthesize topology for Pando delegated subagents, built on the
existing `Dependencies` DAG and on P1/P2 already shipped
([[mesnada_swarm_blackboard_conclusion_forwarding]]). **Plan only — no code yet.**

## Goal
Turn ad-hoc fan-out (parent manually spawns N tasks + a merge task) into a repeatable pattern
where a **verifier** task gates a **synthesizer**: the synthesizer must not start until the
verifier's result *passes*. Mirrors Hermes `kanban_swarm.create_swarm`
(root → parallel workers → verifier `{"gate":"pass"}` → synthesizer) but in Pando's push,
in-process orchestrator.

## Key design decision — model the gate without new task states
Hermes uses dedicated `blocked`/`review` states. Adding `TaskStatusBlocked`/`TaskStatusReview`
to Pando has a **wide blast radius**: `parseMesnadaStatuses` (mesnada.go:810),
`server/gin.go:303` validation, `server/ui.go` + delegation supervisor switches, spawner
`explicitStop` checks in all 7 spawners, `IsTerminal`. For the prototype we **avoid new
states** and model the gate on the dependency edge using the `Conclusion.Status` Pando already
captures (`success|partial|failed|blocked`).

- **Gate = a subset of a task's dependencies that must additionally pass a conclusion check**,
  not just be `completed`.
- Passing predicate (default, fail-closed): `dep.Conclusion != nil && Status == "success"`.
  `partial` is configurable-in later; `failed`/`blocked`/missing = not passing.

This reuses terminal states only; `blocked`/`review` become a documented P3.2 refinement if a
real human-review lane is wanted.

## Data model changes (`pkg/mesnada/models/task.go`)
- `Task.GateDeps []string` — subset of `Dependencies` that must pass the conclusion gate (not
  merely complete). Empty ⇒ today's behavior. Persisted (FileStore stores whole Task, no
  migration).
- `SpawnRequest.GateDeps []string` — flow-through, wired in `Spawn` like `Dependencies`.

No `IsTerminal`/status enum change.

## Orchestrator changes (`internal/mesnada/orchestrator/orchestrator.go`)
1. **`canStart` gate check** — after the existing `dep.Status == Completed` loop, for each
   `depID` in `task.GateDeps` require `conclusionPasses(dep)`. New helper
   `conclusionPasses(*Task) bool` (nil-safe, fail-closed).
2. **Gate-fail handling** (the "stuck pending" problem: a verifier that completes *not-passing*
   would leave the synthesizer `pending` forever). In `processDependentTasks`, when the just-
   completed task is a gate dep of a pending dependent and it did **not** pass:
   - **Recommended**: leave the dependent `pending`, emit a `gate_failed` signal and let the
     verifier's own conclusion (`status=blocked/failed`) propagate up the normal completion bus
     (Case A live-injection / Case B resurrection) so the **parent agent** decides — relaunch
     workers, adjust, then `Relaunch` the verifier (its completion re-runs
     `processDependentTasks`, re-evaluating the gate). This matches Pando's existing delegation
     feedback loop and keeps the graph re-drivable.
   - **Fallback (simpler)**: mark gated dependents `failed` with
     `error="gate <verifierID> did not pass: <summary>"`. Honest terminal signal but requires
     relaunching dependents too. Document as the alternative.
   - Decision: implement **Recommended** (leave-pending + signal); it composes with
     resurrection and avoids destroying the synthesizer card.
3. **`CreateSwarm`** — new method that builds the graph in one call:
   - Spawn each worker (`SpawnRequest` per `SwarmWorkerSpec`, no deps).
   - Spawn verifier: `Dependencies = workerIDs`, plus a verifier brief instructing it to close
     with `<pando:conclusion status=success>` only when evidence is sufficient, else
     `status=blocked` with the exact missing work.
   - Spawn synthesizer: `Dependencies = [verifierID]`, `GateDeps = [verifierID]`.
   - Reuse P1: all tasks share the parent's swarm id (ParentSessionID), so workers already get
     the blackboard pointer + P2 conclusion forwarding via `buildSwarmContext`.
   - Return `SwarmCreated{RootWorkerIDs, VerifierID, SynthesizerID}`. Idempotency: honor
     `CorrelationID` (a repeated call with the same key returns existing IDs — reuse the P1
     blackboard `topology` note pattern from Hermes).

## Tool (`internal/llm/tools/mesnada.go`)
- New `mesnada_swarm` tool (`MesnadaSwarmTool`): args `goal` (string), `workers` (array of
  `{prompt, engine?, model?, persona?}`), optional `verifier`/`synthesizer` overrides
  (`{engine, model, persona}`), `work_dir`, `project`. Calls `orchestrator.CreateSwarm`, returns
  the IDs + a hint to `mesnada_await` on the synthesizer.
- Register in `agent/tools.go` (only when delegation enabled, like `mesnada_await`) + allow-list
  in `builtin_names.go` (`mesnadaSwarmToolName = "mesnada_swarm"`).
- Verifier/synthesizer default engine = the orchestrator default (pando); default assignees are
  the same session so they inherit config.

## Gating / config
- Whole feature gated on `Mesnada.Delegation.Enabled` (consistent with P1/P2). A dedicated
  `Mesnada.Swarm` toggle is a later refinement; noted, not built now.

## Tests
- `canStart`: gate dep completed+passing ⇒ startable; completed+not-passing ⇒ blocked;
  completed+no-conclusion ⇒ blocked (fail-closed); non-gate dep unaffected.
- Gate-fail handling: verifier completes `blocked` ⇒ synthesizer stays `pending`, `gate_failed`
  emitted; verifier `Relaunch`→passes ⇒ synthesizer starts.
- `CreateSwarm`: wiring (worker→verifier→synthesizer deps + GateDeps), swarm-id shared,
  idempotent on CorrelationID.
- `mesnada_swarm` tool: arg decode, error paths, returned IDs.
- Regression: existing orchestrator + tools + `./internal/llm/agent ./internal/api` suites green.

## Blast-radius / risks
- `GateDeps` is additive; empty for every existing task ⇒ no behavior change.
- Leave-pending gate-fail relies on the parent driving recovery; if the parent never relaunches
  the verifier the synthesizer waits — acceptable (same shape as any unmet dependency) and
  observable via the emitted signal + verifier conclusion.
- No new statuses ⇒ UI/server/supervisor untouched.

## Explicitly out of scope (future)
- `TaskStatusBlocked`/`Review` + human-review lane (P3.2).
- Per-worker `max_runtime`/retry caps, per-profile concurrency (P4/P7).
- Blackboard GC (tracked in [[mesnada_swarm_blackboard_conclusion_forwarding]]).

## File touch list (when implementing)
- `pkg/mesnada/models/task.go`: `Task.GateDeps`, `SpawnRequest.GateDeps`,
  `SwarmWorkerSpec`/`SwarmCreated` types (or in orchestrator pkg).
- `internal/mesnada/orchestrator/orchestrator.go`: `canStart` gate, `conclusionPasses`,
  gate-fail handling in `processDependentTasks`, `CreateSwarm`, wire `GateDeps` in `Spawn`.
- `internal/mesnada/orchestrator/swarm.go` (new): `CreateSwarm` + spec types + verifier/
  synthesizer briefs.
- `internal/llm/tools/mesnada.go`: `MesnadaSwarmTool`, `mesnadaSwarmToolName`.
- `internal/llm/agent/tools.go`, `internal/llm/tools/builtin_names.go`: register + allow-list.
- Tests: `swarm_test.go`, `canstart_gate_test.go`, tool test.
