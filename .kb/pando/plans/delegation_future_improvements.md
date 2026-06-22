---
created_at: 2026-06-22T07:16:55.208035063Z
updated_at: 2026-06-22T07:42:14.964121795Z
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

Created 2026-06-22. Status: BACKLOG. Consolidates every deferred /
optional / "future enhancement" item left after the delegated-task conclusions +
agent-loop resurrection feature shipped COMPLETE (Phases 0-6) and the warm
per-project instance reuse re-plan shipped COMPLETE (Phases 7.1-7.5).

Progress: **A1 DONE (2026-06-22)** — see `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`.

## Related documents (read these first for context)
- Feature (current state): `pando/features/delegated_conclusions_resurrection.md`
- Master plan (Phases 0-6): `pando/plans/delegated_conclusion_resurrection_plan.md`
- Phase 7 re-plan (warm reuse, decisions): `pando/plans/delegation_phase7_warm_instance_replan.md`
- Phase changes: `pando/changes/delegation_phase{0..6}_*.md` and
  `pando/changes/delegation_phase7_{1,2,3,4,5}_*.md`
- A1 fix: `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`
- Memory index entry: `plan-delegated-conclusion-resurrection`

Each item below notes its SOURCE (where it was deferred), the GAP it closes, a
sketch of the APPROACH, and rough EFFORT/RISK. Ordered roughly by value/effort.

---

## A. Correctness / robustness (do first)

### A1. Fix the viper nested-default shadowing bug (KNOWN PRE-EXISTING) — ✅ DONE 2026-06-22
- SOURCE: surfaced in 7.3; `TestMesnadaDelegationDefaults` (internal/config) failed.
- GAP: the full `config.Load()` path shadowed nested `mesnada.delegation.*` viper
  defaults → they unmarshalled to zero (MaxConcurrent/MaxDepth/MaxResurrections to
  0, timeout to ""). Bit whenever a config file set any sibling `[Mesnada]` key
  without restating every cap.
- RESOLUTION: root cause is viper dropping the `mesnada.delegation` subtree from
  the merged view once `mesnada` exists in the config layer (boolean fields,
  incl. default-true `autoStartWarmInstance`, survive; int/string caps don't, and
  `IsSet`/direct `Get` are equally unreliable). Fixed Go-side, mirroring
  `normalizeRemembrancesDefaults`: documented-default constants
  (`defaultDelegation{MaxResurrections,MaxDepth,MaxConcurrent,ResurrectionTimeout}`)
  shared by `setDefaults` and a new `normalizeMesnadaDelegationDefaults()` (called
  from `applyDefaultValues`) that backfills zero/blank caps post-unmarshal.
  Booleans are left to unmarshal so a user-set `AutoStartWarmInstance=false` is
  preserved. Documented consequence: an explicit cap of 0 now means "use default"
  (was unreachable before anyway). Regression tests added
  (`TestMesnadaDelegationWarmDefaultsUnderShadowing`,
  `TestMesnadaDelegationAutoStartFalseWins`); `TestMesnadaDelegationDefaults` now
  passes. Full detail: `pando/fixes/delegation_a1_viper_nested_default_shadowing.md`.

### A2. Re-attach warm delegated sessions on reconnect (parent restart)
- SOURCE: explicitly out of MVP in the re-plan "Bookkeeping → Parent restart".
- GAP: today, after a parent restart the stale-task recovery marks any running
  warm Task failed and synthesizes a blocked/failed conclusion (the parent
  resurrects with a failure). The child may actually still be running.
- APPROACH: on reconnect, `LoadSession`/`ResumeSession` in the child by the stored
  `ACPSessionID` (already persisted on the Task for correlation), re-subscribe the
  capturing client to that session, and only synthesize-fail if the resume fails.
  Idempotency is already guarded by CorrelationID.
- EFFORT: M-L. RISK: med (cross-restart session lifecycle). Depends on child ACP
  `ResumeSession` semantics being reliable.

### A3. Queue (don't cold-fall-back) when over the per-instance cap
- SOURCE: re-plan 7.3 ("over the cap → cold-spawn (or queue)").
- GAP: when `MaxConcurrent` warm sessions are in flight, the next delegated task
  cold-spawns a fresh CLI subprocess — losing the warm-context benefit precisely
  under load.
- APPROACH: optional bounded FIFO per `Instance`; a released slot pulls the next
  queued delegation. Add a config knob (e.g. `WarmQueueDepth`, 0 = today's
  cold-fallback). Surface queue depth in the panel count.
- EFFORT: M. RISK: low-med (must respect ctx-cancel while queued so a panel Stop
  drains the queue to the cold path).

---

## B. Routing / targeting

### B1. Target a registered project by id from the spawn tool / UI
- SOURCE: deferred from 7.4 scope ("Optionally target a registered project by id,
  not just a raw path").
- GAP: warm routing resolves a project only by canonicalising `work_dir`. A
  delegating agent cannot say "run this in project X" directly.
- APPROACH: add an optional `project_id` arg to `mesnada_spawn` (and the WebUI
  spawn affordance); thread it through the Task → `WarmDelegate(projectID, …)`
  which already accepts an explicit id (`resolveProjectID` passes it through).
  Validate against the registry; fall back to path resolution when absent.
- EFFORT: S-M. RISK: low.

### B2. Reconsider external (editor-launched) instances as warm targets
- SOURCE: re-plan decision 2 ("external → always cold path, for now").
- GAP: a project already open in an editor's ACP session cold-spawns instead of
  reusing that live process.
- APPROACH: this is mostly a policy + safety question (we don't own that process's
  lifecycle, and a panel Stop refuses external instances). Could allow opt-in warm
  reuse of external instances behind a separate flag, only for read-mostly
  delegations, with no Stop-cancel guarantee. Needs a design note before code.
- EFFORT: M. RISK: med-high (shared ownership of an externally-managed process).

### B3. Hot-peer delegation across instances via IPC (original Phase 7 idea)
- SOURCE: the ORIGINAL single Phase 7 before the re-plan; left as "opt-in,
  deferred" in the master plan.
- GAP: warm reuse today is in-process (one machine, one manager owning child ACP
  processes). Cross-instance / cross-terminal delegation would route a task to a
  peer Pando discovered over the existing ZeroMQ IPC layer.
- APPROACH: build on the inter-instance communication plan
  (`pando/plans/inter_instance_communication_plan.md`): a peer advertises which
  projects it serves warm; the orchestrator's `WarmTargetResolver` gains a
  remote branch that dispatches the delegated prompt over IPC and captures the
  conclusion via the remote event stream. Large; its own plan.
- EFFORT: L. RISK: high. Treat as a separate epic.

---

## C. Lifecycle / resource management

### C1. Idle auto-GC of router-spawned warm instances
- SOURCE: re-plan 7.3/7.4 ("Optional later: idle auto-GC").
- GAP: an instance auto-started by the router (`delegationSpawned == true`)
  persists in the Projects panel until the user stops it, even with zero
  in-flight delegations and no user activation — slowly leaking child processes.
- APPROACH: a janitor that, for instances where `isDelegationSpawned() &&
  InflightDelegations()==0 && id != activeID`, waits an idle grace period then
  `StopReport`s them (publishing `EvDelegationChanged`/status). Config knob
  `WarmInstanceIdleTimeout` (0 = never GC). Never touch user-activated instances.
- EFFORT: M. RISK: low-med (must not race a new incoming delegation; re-check
  inflight under the manager lock before killing).

### C2. Promote a delegation-spawned instance to user-activated on focus
- SOURCE: implied by the `delegationSpawned` flag semantics (7.4).
- GAP: if the user clicks a router-spawned project in the panel, it should stop
  being a GC candidate. `Activate`'s reuse branch already clears the flag — verify
  the WebUI/TUI activation path always routes through it, and add a test.
- EFFORT: S. RISK: low.

---

## D. Conclusion contract / quality

### D1. Schema-validate the `<pando:conclusion>` block
- GAP: the parser is tolerant; a malformed block silently degrades to fallback
  synthesis. A soft schema (known keys, enum for status/confidence) would let the
  software flag a non-conforming subagent and prompt a retry.
- APPROACH: validate in `conclusion.parse`/`enrich`; on violation, attach a
  warning to the synthesized conclusion rather than discarding structure.
- EFFORT: S-M. RISK: low.

### D2. Richer artifact/memory_ref resolution in FormatForParent
- GAP: `FormatForParent` emits pointers (not dumps) — good — but artifacts and
  memory_refs are passed through verbatim. Resolving memory_refs to titles, or
  validating artifact paths exist, would make re-entry messages more actionable.
- EFFORT: S-M. RISK: low.

---

## E. Observability

### E1. Delegation metrics + panel telemetry
- GAP: no aggregate visibility into delegated-loop volume, durations, warm-reuse
  hit rate (warm vs cold), cap rejections, or resurrection counts.
- APPROACH: counters around `tryStartWarm` (reuse/autostart/cold/cap outcomes),
  conclusion capture latency, resurrection events; expose via the evaluator/logs
  page and an optional `/api/v1/...` stats endpoint. Feeds tuning of MaxConcurrent
  / idle timeout.
- EFFORT: M. RISK: low.

### E2. Per-session model/persona surfaced in the panel
- SOURCE: 7.1 made model/persona per-session; the panel only shows a loop count.
- GAP: a user can't see WHICH model/persona each delegated loop is running.
- APPROACH: extend `DelegationInfo` (or a new `ListDelegatedSessions`) to return
  per-session {model, persona, started_at}; render in a panel tooltip/expander.
- EFFORT: M. RISK: low.

---

## Sequencing suggestion
1) ~~A1 (config bug)~~ ✅ DONE → 2) C1+C2 (resource hygiene) →
3) B1 (targeting, cheap UX win) → 4) A3 (queue under load) →
5) E1 (metrics to guide further tuning) → 6) A2 (reconnect) →
7) B3 / B2 (epics, separate plans). D-items are independent, pick up opportunistically.

All items are OFF by default-consistent with the feature's design: every new knob
defaults to today's behavior so nothing changes unless explicitly enabled.
