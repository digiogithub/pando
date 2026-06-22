---
created_at: 2026-06-21T09:48:07.544164889Z
updated_at: 2026-06-21T09:48:07.544164889Z
tags:
    - change
    - mesnada
    - delegation
    - phase0
    - config
    - model
    - orchestrator
    - store
---
# Change: Delegation Phase 0 — Foundations

Implemented 2026-06-21 (via delegated subagent + parent review). Status: DONE,
verified. Part of plan `pando/plans/delegated_conclusion_resurrection_plan.md`
(Phase 0). **No runtime behavior change — everything is default-OFF**; this phase
only adds the data model, correlation capture, config, and a store query that
later phases (1-7) will consume.

## What changed & why
Foundations for delegated-task conclusions + agent-loop resurrection: the system
must be able to (a) correlate a spawned mesnada task back to its parent agent
session, (b) carry a future `Conclusion` on the task record, (c) resolve the
project a task ran in, and (d) be toggled/persisted via config. None of these are
wired to behavior yet.

## Files touched
- `pkg/mesnada/models/task.go` — new `Conclusion` struct (Status, Summary,
  Artifacts, MemoryRefs, FollowUp, Confidence, Synthesized, CapturedAt). New
  `Task` fields: `ParentSessionID`, `ParentTaskID`, `CorrelationID`, `ProjectID`,
  `ProjectPath`, `Conclusion *Conclusion`, `Depth`. Same six correlation fields
  added to `SpawnRequest`.
- `internal/llm/tools/mesnada.go` — new `spawnCorrelation(ctx, workDir)` helper
  (mesnada.go:160): reads parent session from `ctx.Value(tools.SessionIDContextKey)`,
  generates `CorrelationID` via `github.com/google/uuid` (already a dep in the
  package), resolves `config.CanonicalProjectPath(workDir)` into `ProjectPath`.
  Wired into the new-task Spawn path (mesnada.go:267+). `ProjectID` left empty
  (DB registry lookup deferred to Phase 1).
- `internal/mesnada/orchestrator/orchestrator.go` — `Spawn` copies the six
  correlation fields onto the created `Task`; `retryFailed` and paused-resume
  preserve the original task's correlation fields. (Relaunch/replayPending mutate
  the loaded Task in place, so fields persist automatically.)
- `internal/mesnada/store/store.go` — `ListFilter.ParentSessionID` + filtering in
  `matchesFilter`; new `Store.ListByParentSession(sessionID)` interface method +
  `FileStore` impl (thin wrapper over `List`).
- `internal/config/config.go` — `MesnadaDelegationConfig` (config.go:260) with
  `Delegation` field on `MesnadaConfig` (config.go:253). Viper defaults
  (Enabled=false, MaxResurrections=4, MaxDepth=3, MaxConcurrent=8,
  ResurrectionTimeout="10m"); explicit `PANDO_DELEGATION_*` env overrides via a
  `parseEnvBool` helper (viper has no global EnvKeyReplacer, so the explicit
  os.Getenv pattern is used, matching internal-tools API keys).
  `UpdateMesnadaDelegation` (config.go:3182) mirrors `UpdateMesnada` with
  rollback-on-failure.

## Tests added
- `pkg/mesnada/models/task_delegation_test.go` — Task JSON round-trip incl.
  Conclusion; conclusion JSON-tag contract; SpawnRequest field preservation.
- `internal/mesnada/store/store_parent_session_test.go` — ListByParentSession +
  ListFilter.ParentSessionID filtering.
- `internal/config/mesnada_delegation_test.go` — update persist+reload, rollback
  on corrupted file, defaults applied, user-values-win, parseEnvBool.
- `internal/llm/tools/mesnada_correlation_test.go` — spawnCorrelation sets parent
  session from ctx, non-empty unique correlation id, canonical project path.

## Verification
- `go build ./...` → clean (exit 0). (An editor compiler diagnostic about
  `*FileStore` missing `ListByParentSession` was a STALE mid-edit snapshot; real
  build is clean and the method exists at store.go:209.)
- `go test ./internal/llm/tools ./internal/config ./internal/mesnada/...` → all
  pass.
- Lint nits flagged (`interface{}→any` at task.go:147/271; `slices.Contains` at
  store.go:217/237) are PRE-EXISTING code whose line numbers shifted; not
  introduced by Phase 0, left unchanged to avoid scope creep.

## Notes that adjust later phases
- The plan listed an in-memory `subscribersBySession` index under Phase 0; it is
  consumer-facing, so it was deferred to Phases 2/3 where the supervisor actually
  uses it. Only the persistent `ListByParentSession` query was added now.
- `Relaunch` reuses the stored Task object → it preserves `Depth`/`CorrelationID`
  unchanged (no increment). Correct for in-place re-execution, but Phase 3 depth
  caps won't trip on relaunch — keep in mind.
- `ProjectID` resolution (work_dir → projects SQLite registry) is intentionally
  NOT done yet; Phase 1's enricher will add it where the project service is wired.
- Viper has no project-wide EnvKeyReplacer; future nested env knobs (Phase 5) must
  use the explicit os.Getenv pattern.
