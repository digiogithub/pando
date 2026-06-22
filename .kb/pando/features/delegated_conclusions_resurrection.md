---
created_at: 2026-06-22T11:18:38.908642081Z
updated_at: 2026-06-22T11:19:49.978045844Z
tags:
    - feature
    - mesnada
    - delegation
    - orchestrator
    - agent
    - warm-instance
    - projects
    - config
    - lifecycle
---
# Feature: Delegated-Task Conclusions + Agent-Loop Resurrection

Status: COMPLETE (Phases 0-6, 2026-06-21) + Phase 7 warm per-project instance
reuse COMPLETE (7.1-7.5, 2026-06-22) + lifecycle hygiene C1/C2 (2026-06-22) +
project targeting B1 (2026-06-22) + warm-queue-under-load A3 (2026-06-22).
Plans: `pando/plans/delegated_conclusion_resurrection_plan.md`, the Phase 7
re-plan `pando/plans/delegation_phase7_warm_instance_replan.md`, and the future
improvements backlog `pando/plans/delegation_future_improvements.md`. Default-OFF.

## What it does
When the agent delegates work to subagent tasks through the mesnada orchestrator
(`mesnada_spawn`), this feature captures each subagent's result as a structured
conclusion and feeds it back into the parent agent loop (live injection Case A or
idle resurrection Case B) instead of fire-and-forget. The subagent emits ONLY a
thin `<pando:conclusion>` sentinel; the SOFTWARE fills launch metadata from the
Task record. `mesnada_await` lets the model end its turn and be resurrected on a
join policy.

## Warm per-project instance reuse (Phase 7)
Optionally route a delegated task whose project is known to an already-running
warm per-project ACP instance instead of cold-spawning a `pando -p` CLI, capturing
the conclusion over the wire. 7.1 per-session model/persona hardening; 7.2
capturing ACP client + `Manager.Delegate`/`DelegateResult`; 7.3 orchestrator
`WarmTargetResolver` + `tryStartWarm` (reuse-then-autostart-else-cold) +
`EnsureInstance` + `WarmDelegate` (per-instance cap via `Instance.inflight`); 7.4
Projects panel integration; 7.5 config UI + e2e + docs. External (editor-launched)
instances are never warm targets. Stopping an instance cancels in-flight
delegations → cold path → always-terminal conclusion (idempotent via CorrelationID).

## Lifecycle hygiene (C1 + C2, 2026-06-22)
C2 promote-on-focus: user activation reusing a warm instance clears
`delegationSpawned` (`Manager.Activate`). C1 idle auto-GC: janitor
(`StartIdleGC`/`gcIdleInstances`/`teardownIdleInstance` in `delegation_gc.go`)
stops delegation-spawned, non-active, idle, zero-inflight instances after
`WarmInstanceIdleTimeout` ("0" = off). Race-safe via `Instance.closing` +
`tryBeginClose`. Change: `pando/changes/delegation_c1_c2_idle_gc_promote.md`.

## Targeting a specific project from the spawn tool (B1, 2026-06-22)
`mesnada_spawn_agent` + MCP `spawn_agent` take an optional `project` arg (registry
id / display name / directory path), resolved via `orchestrator.ProjectRefResolver`
(+ `Orchestrator.ResolveProjectRef`/`ProjectRefsSupported`/`ListProjectRefs`)
backed by `app.projectRefResolverAdapter` over `project.Service` (id → canonical
path → exact name). Sets `SpawnRequest.ProjectID`, defaults `work_dir`; unknown ref
fails fast at the tool boundary with a known-projects list. Change:
`pando/changes/delegation_b1_project_target_from_spawn.md`.

## Warm-instance queue under load (A3, 2026-06-22)
When a warm instance is at MaxConcurrent, a delegated task normally cold-falls-back.
`WarmQueueDepth > 0` instead lets up to that many delegations BLOCK in a bounded
FIFO per `Instance` waiting for a freed slot; beyond cap+depth they still
cold-fall-back. `Instance.cond`/`waiters` + `acquireDelegationSlotOrQueue(ctx, max,
queueDepth)`; `releaseDelegationSlot` broadcasts; `beginCloseAndWake` (Stop) and
ctx-cancel release queued waiters to the cold path (ErrWarmCapReached). Default 0 =
prior behaviour. Change: `pando/changes/delegation_a3_warm_queue_depth.md`.

## Caps
MaxResurrections, MaxDepth (env PANDO_DELEGATION_DEPTH), MaxConcurrent (outstanding
per parent AND concurrent warm sessions per instance; over it a warm task queues up
to WarmQueueDepth then cold-falls-back), ResurrectionTimeout.

## Configuration (Mesnada.Delegation, default-off)
config.MesnadaDelegationConfig: Enabled, InjectIntoLiveLoop, ResurrectIdleLoop,
SynthesizeFallback, MaxResurrections (4), MaxDepth (3), MaxConcurrent (8),
ResurrectionTimeout ("10m"), ReuseWarmInstances (false), AutoStartWarmInstance
(true), WarmInstanceIdleTimeout ("0" = idle GC disabled), WarmQueueDepth (0 =
cold-fallback at cap). Persisted via UpdateMesnadaDelegation; env PANDO_DELEGATION_*.
Editable: TUI Settings → Subagents; WebUI Settings → General → Subagent Delegation;
REST GET/PUT /api/v1/settings flat delegation_* fields (incl.
delegation_warm_queue_depth). Caps defended against viper nested-default shadowing
by normalizeMesnadaDelegationDefaults (pando/fixes/delegation_a1_viper_nested_default_shadowing.md).

## Key code map
- pkg/mesnada/models/task.go — Conclusion struct + correlation/project/depth fields.
- internal/mesnada/conclusion/ — parse/enrich/format/brief.
- internal/mesnada/orchestrator/{orchestrator.go,warm.go} — captureConclusion,
  WarmTargetResolver/tryStartWarm, ProjectRefResolver (B1).
- internal/llm/tools/mesnada.go + internal/mesnada/server/tools.go — spawn `project`
  arg → SpawnRequest.ProjectID (B1); knownProjectsHint.
- internal/project/{delegation.go,delegation_client.go,delegation_gc.go,instance.go,
  manager.go} — Delegate/WarmDelegate/EnsureInstance, Instance.inflight/lastActiveAt/
  closing, StartIdleGC; Instance.cond/waiters + acquireDelegationSlotOrQueue/
  beginCloseAndWake + WarmDelegate queueDepth (A3).
- internal/app/app.go — makeWarmTargetResolver + StartIdleGC + projectRefResolverAdapter.
- internal/api/handlers_settings.go, internal/tui/page/settings.go, web-ui
  (GeneralSettings.tsx, settingsStore.ts, types/index.ts, 7 i18n locales).

## Tests / verification
- orchestrator/{delegation_e2e,warm}_test.go — routing, warm terminal, ProjectRef.
- project/{delegation_internal,manager_warm,delegation_gc}_test.go — capture, reuse,
  cap, DelegationInfo, parallel (race), StopReport count, idle GC, promote-on-focus.
- B1: app/project_ref_resolver_test.go, tools/mesnada_project_target_test.go,
  orchestrator/warm_test.go.
- A3: project/instance_queue_test.go (immediate, cold-fallback at depth 0,
  queue-then-proceed + bounded-FIFO refusal + freed-slot handoff, ctx-cancel,
  beginCloseAndWake — race-clean).
- WebUI tsc clean; go build/vet/gofmt clean; go test -race green.
