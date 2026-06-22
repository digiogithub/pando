---
created_at: 2026-06-22T21:03:50.689205964Z
updated_at: 2026-06-22T21:04:28.648476665Z
tags:
    - plan
    - improvements
    - roadmap
    - mesnada
    - delegation
    - warm-instance
    - orchestrator
    - future
    - a2
    - deferred
    - d1
    - d2
---
# Plan: Delegation — Future Improvements & Open Opportunities

Created 2026-06-22. Status: BACKLOG (partially in progress). Consolidates every deferred / optional / "future enhancement" item left after the delegated-task conclusions + agent-loop resurrection feature shipped COMPLETE (Phases 0-6) and the warm per-project instance reuse re-plan shipped COMPLETE (Phases 7.1-7.5).

Progress:
- **A1 DONE (2026-06-22)** — `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`.
- **C1 + C2 DONE (2026-06-22)** — `pando/changes/delegation_c1_c2_idle_gc_promote.md`.
- **B1 DONE (2026-06-22)** — `pando/changes/delegation_b1_project_target_from_spawn.md`.
- **A3 DONE (2026-06-22)** — `pando/changes/delegation_a3_warm_queue_depth.md`.
- **E1 DONE (2026-06-22)** — `pando/changes/delegation_e1_metrics.md`.
- **A2 DEFERRED (2026-06-22)** — folded into B2/B3 (see below).
- **D1 + D2 DONE (2026-06-22)** — `pando/changes/delegation_d1_conclusion_schema_validation.md`, `pando/changes/delegation_d2_format_artifact_resolution.md`.

## Related documents
- Feature (current state): `pando/features/delegated_conclusions_resurrection.md`
- Master plan (Phases 0-6): `pando/plans/delegated_conclusion_resurrection_plan.md`
- Phase 7 re-plan: `pando/plans/delegation_phase7_warm_instance_replan.md`
- A1 fix / C1+C2 / B1 / A3 / E1 / D1 / D2 changes under pando/{fixes,changes}/...

## A. Correctness / robustness
- A1. Fix viper nested-default shadowing — ✅ DONE.
- A2. Re-attach warm delegated sessions on reconnect (parent restart): on reconnect LoadSession/ResumeSession by stored ACPSessionID, re-subscribe capturing client, synthesize-fail only if resume fails; idempotent via CorrelationID. EFFORT M-L, RISK med. **DEFERRED (2026-06-22) — blocked on B2/B3.** Investigation finding: warm instances are manager-spawned stdio subprocesses (`Manager.spawnChild`, `exec.CommandContext(procCtx<-m.ctx, pandoBin, "acp")`); a parent restart closes the stdio pipes (child EOF) and cancels procCtx (SIGKILL), so a warm child CANNOT survive the parent — there is nothing to re-attach to. `EnsureInstance` also refuses external/editor instances (`ErrExternalInstance`). The mechanism A2 needs already exists (`PandoACPAgent.LoadSession` replays history, `AgentCapabilities.LoadSession=true`, SDK `conn.LoadSession`), and the warm path is fully synchronous with EPHEMERAL sessions closed immediately (`closeDelegatedSession`), persisting `ACPSessionID` only at completion — so today a mid-flight crash leaves a `running` task with no session breadcrumb and `recoverStaleTasks` just marks it failed. Net: literal cross-restart re-attach is INERT until warm targets can be surviving peers (external/IPC/server-mode), i.e. B2 (external-as-warm) and/or B3 (hot-peer IPC delegation). Re-attach should be implemented AS PART OF whichever of B2/B3 makes peers survivable, where it can actually be exercised end-to-end. Decision: defer (per user, 2026-06-22).
- A3. Queue over the per-instance cap — ✅ DONE.

## B. Routing / targeting
- B1. Target a registered project by id/name/path from spawn tool — ✅ DONE.
- B2. Reconsider external (editor-launched) instances as warm targets (opt-in, read-mostly, no Stop-cancel guarantee; design note first). EFFORT M, RISK med-high. (A2 re-attach lands here once peers survive parent restart.)
- B3. Hot-peer delegation across instances via IPC (builds on inter_instance_communication_plan; WarmTargetResolver gains remote branch; own epic). EFFORT L, RISK high. (A2 re-attach lands here once peers survive parent restart.)

## C. Lifecycle / resource management
- C1. Idle auto-GC of router-spawned warm instances — ✅ DONE.
- C2. Promote delegation-spawned → user-activated on focus — ✅ DONE.

## D. Conclusion contract / quality
- D1. Schema-validate the `<pando:conclusion>` block (soft schema, warn don't discard) — ✅ DONE 2026-06-22. `Conclusion.Warnings []string`; `Parse` accumulates soft warnings (unknown status, confidence out-of-range, missing summary, blank artifact/memory entries dropped, malformed/non-mapping YAML salvaged); never discards data; `brief.go` schema instruction tightened; warnings surfaced on `FormatForParent`'s trailing `warnings:` line. Detail: `pando/changes/delegation_d1_conclusion_schema_validation.md`.
- D2. Richer artifact/memory_ref resolution in FormatForParent — ✅ DONE 2026-06-22. New `conclusion.ResolveOptions`/`ArtifactResolver`/`MemoryRefResolver` + `NewFilesystemArtifactResolver`; `FormatForParent(task, *ResolveOptions)` renders resolved per-line artifacts/memory_refs (artifact = filesystem size/missing), nil = byte-identical legacy output; wired at supervisor (3 sites) + mesnada_await; memory-ref resolver plumbed but nil pending kb wiring. Detail: `pando/changes/delegation_d2_format_artifact_resolution.md`.

## E. Observability
- E1. Delegation metrics + panel telemetry — ✅ DONE 2026-06-22. Lock-free DelegationMetrics (atomic counters) on the orchestrator — warm_attempts/hits/failures, cold_fallbacks, cap_rejections (cap subset via new orchestrator.ErrWarmCapReached sentinel wrapping ErrNoWarmTarget), resurrections + live_injections (recorded by supervisor), derived warm_hit_rate. Surfaced via Orchestrator.DelegationMetrics(), GET /api/v1/orchestrator/delegation/metrics, a TUI dashboard header line and a WebUI DelegationMetricsBar (both shown only with activity). Read-only, no config knob, default unchanged. Tests internal/mesnada/orchestrator/metrics_test.go. Detail: pando/changes/delegation_e1_metrics.md.
- E2. Per-session model/persona surfaced in the panel. EFFORT M, RISK low. NOT STARTED.

## Sequencing suggestion
1) A1 → 2) C1+C2 → 3) B1 → 4) A3 → 5) E1 → ~~6) A2 (reconnect)~~ DEFERRED into B2/B3 → 6) ~~D1/D2~~ DONE → 7) B2 / B3 (surviving-peer warm targets; A2 re-attach lands here) and/or E2 (opportunistic). All knobs default-off-consistent. (A1, C1, C2, B1, A3, E1, D1, D2 done; A2 deferred — folded into B2/B3. Remaining: B2, B3, E2.)