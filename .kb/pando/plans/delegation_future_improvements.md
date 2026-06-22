---
created_at: 2026-06-22T11:17:54.441421088Z
updated_at: 2026-06-22T11:19:16.436036462Z
tags:
    - plan
    - improvements
    - roadmap
    - mesnada
    - delegation
    - warm-instance
    - orchestrator
    - future
---
# Plan: Delegation — Future Improvements & Open Opportunities

Created 2026-06-22. Status: BACKLOG (partially in progress). Consolidates every
deferred / optional / "future enhancement" item left after the delegated-task
conclusions + agent-loop resurrection feature shipped COMPLETE (Phases 0-6) and
the warm per-project instance reuse re-plan shipped COMPLETE (Phases 7.1-7.5).

Progress:
- **A1 DONE (2026-06-22)** — `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`.
- **C1 + C2 DONE (2026-06-22)** — `pando/changes/delegation_c1_c2_idle_gc_promote.md`.
- **B1 DONE (2026-06-22)** — `pando/changes/delegation_b1_project_target_from_spawn.md`.
- **A3 DONE (2026-06-22)** — `pando/changes/delegation_a3_warm_queue_depth.md`.

## Related documents (read these first for context)
- Feature (current state): `pando/features/delegated_conclusions_resurrection.md`
- Master plan (Phases 0-6): `pando/plans/delegated_conclusion_resurrection_plan.md`
- Phase 7 re-plan (warm reuse, decisions): `pando/plans/delegation_phase7_warm_instance_replan.md`
- A1 fix: `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`
- C1+C2 change: `pando/changes/delegation_c1_c2_idle_gc_promote.md`
- B1 change: `pando/changes/delegation_b1_project_target_from_spawn.md`
- A3 change: `pando/changes/delegation_a3_warm_queue_depth.md`

## A. Correctness / robustness

### A1. Fix the viper nested-default shadowing bug — ✅ DONE 2026-06-22
Resolved. Detail: `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`.

### A2. Re-attach warm delegated sessions on reconnect (parent restart)
- GAP: after a parent restart the stale-task recovery marks any running warm Task
  failed; the child may still be running.
- APPROACH: on reconnect, `LoadSession`/`ResumeSession` in the child by the stored
  `ACPSessionID`, re-subscribe the capturing client, only synthesize-fail if the
  resume fails. Idempotency guarded by CorrelationID. EFFORT: M-L. RISK: med.

### A3. Queue (don't cold-fall-back) when over the per-instance cap — ✅ DONE 2026-06-22
Resolved: bounded FIFO per `Instance` (`Instance.cond`/`waiters` +
`acquireDelegationSlotOrQueue(ctx, max, queueDepth)`; `releaseDelegationSlot`
broadcasts; `beginCloseAndWake` for Stop). Over `MaxConcurrent` up to
`WarmQueueDepth` delegations block for a freed slot; beyond that they still
cold-fall-back; a queued waiter that loses ctx or whose instance closes returns
`ErrWarmCapReached`. Config `MesnadaDelegationConfig.WarmQueueDepth` (default 0 =
prior behaviour) wired through `WarmDelegate` + app resolver + full TUI/WebUI/API
+ 7-locale i18n. Tests `internal/project/instance_queue_test.go` (race-clean).
Detail: `pando/changes/delegation_a3_warm_queue_depth.md`.

## B. Routing / targeting

### B1. Target a registered project by id from the spawn tool / UI — ✅ DONE 2026-06-22
Resolved. Detail: `pando/changes/delegation_b1_project_target_from_spawn.md`.

### B2. Reconsider external (editor-launched) instances as warm targets
- Opt-in warm reuse behind a separate flag, read-mostly, no Stop-cancel guarantee.
  Design note first. EFFORT: M. RISK: med-high.

### B3. Hot-peer delegation across instances via IPC (original Phase 7 idea)
- Build on `pando/plans/inter_instance_communication_plan.md`; the orchestrator's
  `WarmTargetResolver` gains a remote branch. Large; own epic. EFFORT: L. RISK: high.

## C. Lifecycle / resource management

### C1. Idle auto-GC of router-spawned warm instances — ✅ DONE 2026-06-22
Resolved. Detail: `pando/changes/delegation_c1_c2_idle_gc_promote.md`.

### C2. Promote a delegation-spawned instance to user-activated on focus — ✅ DONE 2026-06-22
Resolved (shipped with C1).

## D. Conclusion contract / quality
- D1. Schema-validate the `<pando:conclusion>` block (soft schema, warn don't
  discard). EFFORT: S-M. RISK: low.
- D2. Richer artifact/memory_ref resolution in FormatForParent. EFFORT: S-M. RISK: low.

## E. Observability
- E1. Delegation metrics + panel telemetry (warm-reuse hit rate, cap rejections,
  resurrection counts). EFFORT: M. RISK: low.
- E2. Per-session model/persona surfaced in the panel. EFFORT: M. RISK: low.

## Sequencing suggestion
1) ~~A1~~ → 2) ~~C1+C2~~ → 3) ~~B1~~ → 4) ~~A3~~ → 5) E1 (metrics) →
6) A2 (reconnect) → 7) B3 / B2 (epics). D-items independent, opportunistic.
All knobs default-off-consistent: every new knob defaults to today's behavior.
