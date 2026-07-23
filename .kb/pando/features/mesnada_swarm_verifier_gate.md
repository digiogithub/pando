---
created_at: 2026-07-23T14:01:28.866135108Z
updated_at: 2026-07-23T14:01:28.866135108Z
tags:
    - feature
    - mesnada
    - delegation
    - orchestration
    - swarm
    - verifier
    - gate
    - coordination
---
# Mesnada verifier gate + `mesnada_swarm` topology helper (P3)

Implements the [[mesnada_swarm_verifier_gate_plan]] (P3 from
[[hermes-kanban-swarm-vs-pando-delegation]]): a first-class fan-out → **verify (gate)** →
synthesize topology for delegated subagents, built on the existing `Dependencies` DAG and on
P1/P2 ([[mesnada_swarm_blackboard_conclusion_forwarding]]). Built 2026-07-23. Default-off:
gated on `Mesnada.Delegation.Enabled`.

## Design: gate without new task states
As planned, **no `TaskStatusBlocked`/`Review`** were added (wide blast radius across
`parseMesnadaStatuses`, `server/gin.go` validation, `server/ui.go`, delegation supervisor, 7
spawners' `explicitStop`, `IsTerminal`). The gate is modeled on the dependency edge using the
`Conclusion.Status` Pando already captures.

## What changed

### Model (`pkg/mesnada/models/task.go`)
- `Task.GateDeps []string` and `SpawnRequest.GateDeps []string` — a subset of `Dependencies`
  that must additionally PASS a conclusion gate, not merely reach `completed`. Empty ⇒ exact
  prior behavior. Persisted (whole-Task FileStore, no migration).

### Orchestrator (`internal/mesnada/orchestrator/orchestrator.go`)
- `canStart` gate check: for each `depID` in `GateDeps`, require `conclusionPasses(dep)` on top
  of `status==completed`. Helpers `gateSet`, `conclusionPasses` (fail-closed: passes only when
  `Conclusion != nil && Status == "success"` — `partial`/`failed`/`blocked`/missing do not
  pass), `gateBlocks`.
- Gate-fail handling in `processDependentTasks` (chosen "leave-pending + signal" option): a
  dependent held pending purely by a failed gate stays pending and emits a `gate_failed` log
  event (`logTaskGateFailed`). The verifier's own `blocked`/`failed` conclusion propagates up
  the completion bus (Case A/B) so the parent agent adjusts and `Relaunch`es the verifier; its
  next completion re-runs the evaluation and re-opens the gate. No card is destroyed.
- `GateDeps` wired into the `Task` built in `Spawn`.
- Defensive nil-manager guard added to `startTask` (marks failed instead of dereferencing nil).

### Swarm builder (`internal/mesnada/orchestrator/swarm.go`, new)
- Types `SwarmWorkerSpec`, `SwarmRoleSpec`, `SwarmSpec`, `SwarmCreated`.
- `CreateSwarm(ctx, spec)`: spawns each worker (no deps, `Background=true`) → verifier
  (`Dependencies=workerIDs`, built-in brief: close with `status=success` only when evidence
  suffices, else `status=blocked` + exact missing work) → synthesizer (`Dependencies=[verifier]`,
  `GateDeps=[verifier]`, built-in merge brief). All cards share `spec.ParentSessionID` ⇒ inherit
  the P1 blackboard + P2 conclusion forwarding via `buildSwarmContext`. Idempotent when
  `IdempotencyKey` set (topology persisted as a `swarm:topology:<key>` blackboard note;
  `existingSwarm` returns it if the synthesizer still exists).

### Tool (`internal/llm/tools/mesnada.go`)
- `mesnada_swarm` (`MesnadaSwarmTool`): args `goal`, `workers[]` (`prompt`,`engine?`,`model?`,
  `persona?`), optional `verifier`/`synthesizer` overrides, `work_dir`, `idempotency_key`.
  Resolves the parent session id via `spawnCorrelation`, calls `CreateSwarm`, returns worker/
  verifier/synthesizer ids + a `mesnada_await` hint. Registered in `agent/tools.go` (only when
  `Delegation.Enabled`) and allow-listed in `builtin_names.go`
  (`mesnadaSwarmToolName = "mesnada_swarm"`).

## Files touched
- New: `internal/mesnada/orchestrator/swarm.go`, `internal/mesnada/orchestrator/swarm_test.go`.
- Modified: `pkg/mesnada/models/task.go` (GateDeps ×2),
  `internal/mesnada/orchestrator/orchestrator.go` (gate in `canStart`, `gateSet`,
  `conclusionPasses`, `gateBlocks`, gate-fail signal, `logTaskGateFailed`, `GateDeps` in Spawn,
  nil-manager guard), `internal/llm/tools/mesnada.go` (`mesnadaSwarmToolName`, `MesnadaSwarmTool`),
  `internal/llm/agent/tools.go` (register), `internal/llm/tools/builtin_names.go` (allow-list).

## Verification
- `go build ./...` clean.
- Tests pass: `./internal/mesnada/... ./internal/llm/agent ./internal/api`.
- New tests (`swarm_test.go`): `conclusionPasses` truth table, `canStart` gate semantics
  (blocked ⇒ hold, success ⇒ start, non-gate dep unaffected), `gateBlocks`, `CreateSwarm` wiring
  (worker→verifier→synthesizer deps + GateDeps + shared swarm id), validation, idempotency.

## Follow-ups (not done)
- P3.2 real human-review lane (`TaskStatusBlocked`/`Review`), if wanted beyond the
  conclusion-status gate.
- Dedicated `Mesnada.Swarm` config toggle (currently reuses `Delegation.Enabled`).
- `partial` as a configurable passing status.
- Blackboard GC (shared with [[mesnada_swarm_blackboard_conclusion_forwarding]]).
