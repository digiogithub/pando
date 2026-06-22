---
created_at: 2026-06-22T10:59:06.880139501Z
updated_at: 2026-06-22T10:59:46.086855172Z
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

## Related documents (read these first for context)
- Feature (current state): `pando/features/delegated_conclusions_resurrection.md`
- Master plan (Phases 0-6): `pando/plans/delegated_conclusion_resurrection_plan.md`
- Phase 7 re-plan (warm reuse, decisions): `pando/plans/delegation_phase7_warm_instance_replan.md`
- Phase changes: `pando/changes/delegation_phase{0..6}_*.md` and
  `pando/changes/delegation_phase7_{1,2,3,4,5}_*.md`
- A1 fix: `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`
- C1+C2 change: `pando/changes/delegation_c1_c2_idle_gc_promote.md`
- B1 change: `pando/changes/delegation_b1_project_target_from_spawn.md`
- Memory index entry: `plan-delegated-conclusion-resurrection`

Each item below notes its SOURCE (where it was deferred), the GAP it closes, a
sketch of the APPROACH, and rough EFFORT/RISK. Ordered roughly by value/effort.

---

## A. Correctness / robustness (do first)

### A1. Fix the viper nested-default shadowing bug — ✅ DONE 2026-06-22
Resolved: documented-default constants shared by `setDefaults` +
`normalizeMesnadaDelegationDefaults()` backfilling zero/blank caps post-unmarshal;
booleans left to unmarshal so user-set false is preserved. `TestMesnadaDelegationDefaults`
passes. Detail: `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`.

### A2. Re-attach warm delegated sessions on reconnect (parent restart)
- SOURCE: explicitly out of MVP in the re-plan "Bookkeeping → Parent restart".
- GAP: after a parent restart the stale-task recovery marks any running warm Task
  failed and synthesizes a blocked/failed conclusion; the child may still be
  running.
- APPROACH: on reconnect, `LoadSession`/`ResumeSession` in the child by the stored
  `ACPSessionID`, re-subscribe the capturing client, only synthesize-fail if the
  resume fails. Idempotency guarded by CorrelationID.
- EFFORT: M-L. RISK: med.

### A3. Queue (don't cold-fall-back) when over the per-instance cap
- SOURCE: re-plan 7.3 ("over the cap → cold-spawn (or queue)").
- GAP: over `MaxConcurrent` the next delegated task cold-spawns a CLI — losing
  warm-context benefit under load.
- APPROACH: optional bounded FIFO per `Instance`; a released slot pulls the next
  queued delegation. Config knob `WarmQueueDepth` (0 = today's cold-fallback).
- EFFORT: M. RISK: low-med (respect ctx-cancel while queued).

---

## B. Routing / targeting

### B1. Target a registered project by id from the spawn tool / UI — ✅ DONE 2026-06-22
Resolved: new optional `project` arg on `mesnada_spawn_agent` (in-process) and the
standalone MCP `spawn_agent`, accepting a registry **id**, **display name**
(case-insensitive), or **directory path**. Resolved against the registry via a new
`orchestrator.ProjectRefResolver` (interface + `Orchestrator.ResolveProjectRef`/
`ProjectRefsSupported`/`ListProjectRefs`), backed by `app.projectRefResolverAdapter`
over `project.Service` (id → path → name). Sets `SpawnRequest.ProjectID` (already
flowed to `WarmDelegate`/`resolveProjectID`) and defaults `work_dir` to the project
path; unknown reference fails fast at the tool boundary listing known projects (so
it never becomes a terminal-failed warm task). Detail:
`pando/changes/delegation_b1_project_target_from_spawn.md`.

### B2. Reconsider external (editor-launched) instances as warm targets
- SOURCE: re-plan decision 2 ("external → always cold path, for now").
- GAP: a project open in an editor's ACP session cold-spawns instead of reusing.
- APPROACH: policy + safety question (we don't own that process). Opt-in warm
  reuse behind a separate flag, read-mostly, no Stop-cancel guarantee. Design note
  first.
- EFFORT: M. RISK: med-high.

### B3. Hot-peer delegation across instances via IPC (original Phase 7 idea)
- SOURCE: original single Phase 7 before the re-plan.
- GAP: warm reuse today is in-process. Cross-instance delegation would route a
  task to a peer Pando discovered over ZeroMQ IPC.
- APPROACH: build on `pando/plans/inter_instance_communication_plan.md`; the
  orchestrator's `WarmTargetResolver` gains a remote branch. Large; own epic.
- EFFORT: L. RISK: high.

---

## C. Lifecycle / resource management

### C1. Idle auto-GC of router-spawned warm instances — ✅ DONE 2026-06-22
Resolved: `Instance.lastActiveAt`/`closing` + `tryBeginClose` race guard;
`Manager.StartIdleGC`/`gcIdleInstances`/`teardownIdleInstance`
(`internal/project/delegation_gc.go`); config `WarmInstanceIdleTimeout` ("0" =
disabled, default) + env `PANDO_DELEGATION_WARM_INSTANCE_IDLE_TIMEOUT`; started
from `app.go` under ReuseWarmInstances; full API/TUI/WebUI/i18n surface. Only
delegation-spawned, non-active, idle, zero-inflight instances are GC'd. Detail:
`pando/changes/delegation_c1_c2_idle_gc_promote.md`.

### C2. Promote a delegation-spawned instance to user-activated on focus — ✅ DONE 2026-06-22
Verified `Manager.Activate`'s reuse branch already clears `delegationSpawned`
(both TUI + API activation paths route through it) and locked it with
`TestActivatePromotesDelegationSpawnedInstance`. Shipped with C1.

---

## D. Conclusion contract / quality

### D1. Schema-validate the `<pando:conclusion>` block
- GAP: tolerant parser; a malformed block silently degrades to fallback synthesis.
- APPROACH: soft schema (known keys, enum status/confidence) validated in
  `conclusion.parse`/`enrich`; on violation attach a warning, don't discard.
- EFFORT: S-M. RISK: low.

### D2. Richer artifact/memory_ref resolution in FormatForParent
- GAP: artifacts and memory_refs are passed through verbatim.
- APPROACH: resolve memory_refs to titles, validate artifact paths exist.
- EFFORT: S-M. RISK: low.

---

## E. Observability

### E1. Delegation metrics + panel telemetry
- GAP: no aggregate visibility into delegated-loop volume, durations, warm-reuse
  hit rate, cap rejections, resurrection counts.
- APPROACH: counters around `tryStartWarm` outcomes + conclusion latency +
  resurrections; expose via evaluator/logs page and an optional stats endpoint.
- EFFORT: M. RISK: low.

### E2. Per-session model/persona surfaced in the panel
- SOURCE: 7.1 made model/persona per-session; the panel only shows a loop count.
- APPROACH: extend `DelegationInfo` (or a `ListDelegatedSessions`) to return
  per-session {model, persona, started_at}; render in a tooltip/expander.
- EFFORT: M. RISK: low.

---

## Sequencing suggestion
1) ~~A1~~ ✅ DONE → 2) ~~C1+C2~~ ✅ DONE → 3) ~~B1~~ ✅ DONE →
4) A3 (queue under load) → 5) E1 (metrics to guide further tuning) →
6) A2 (reconnect) → 7) B3 / B2 (epics, separate plans). D-items are independent,
pick up opportunistically.

All items are OFF by default-consistent with the feature's design: every new knob
defaults to today's behavior so nothing changes unless explicitly enabled.
