---
created_at: 2026-06-22T11:00:27.608686586Z
updated_at: 2026-06-22T11:01:09.491773744Z
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
project targeting B1 (2026-06-22).
Plans: `pando/plans/delegated_conclusion_resurrection_plan.md`, the Phase 7
re-plan `pando/plans/delegation_phase7_warm_instance_replan.md`, and the future
improvements backlog `pando/plans/delegation_future_improvements.md`. **Default-OFF.**

## What it does
When the agent delegates work to subagent tasks through the mesnada orchestrator
(`mesnada_spawn`), this feature captures each subagent's result as a structured
**conclusion** and feeds it back into the parent agent loop so the parent can act
on it — instead of the delegated work being fire-and-forget.

Two generalizations of mechanisms Pando already had (not new subsystems):
1. Conclusion re-entry = a second event class for the existing agent-loop steering
   inbox (steeringQueue/Steer/drainSteeringInto).
2. Resurrection = the orchestrator's completion stream turns the linear agent loop
   into a supervisor-with-continuations.

## The conclusion contract (software-filled metadata)
The subagent emits ONLY a thin sentinel block with model-known fields:
status / summary / artifacts / memory_refs / follow_up / confidence inside
`<pando:conclusion> ... </pando:conclusion>`. The SOFTWARE fills the launch
metadata (task id, engine, model, work_dir, project id/path/name, parent session,
depth, timestamps, exit code) from the Task record — the model never re-states it.
If the block is absent, a fallback synthesizes a conclusion from output tail /
error (gated by SynthesizeFallback).

## Re-entry cases (each behind its own flag)
- Case A — live injection (InjectIntoLiveLoop): parent loop still running → inject
  via the steering inbox at a safe tool boundary, rendered by
  conclusion.FormatForParent (pointers, not dumps).
- Case B — resurrection (ResurrectIdleLoop): parent loop idle → new
  system-initiated turn via agent.Resume, framed "you are resuming because task X
  reported…". Near-simultaneous sibling completions batched (ResurrectionTimeout).

## Non-blocking await
mesnada_await (registered only when Enabled && ResurrectIdleLoop) lets the model
declare it cannot proceed without results: registers a wait intent and ENDS its
turn (never polls/blocks); the supervisor resurrects it when the join policy
(all/any/quorum) is satisfied or a safety deadline fires.

## Warm per-project instance reuse (Phase 7)
Optionally route a delegated task whose project is known to an already-running
("warm") per-project ACP instance instead of cold-spawning a `pando -p` CLI
subprocess, capturing the conclusion over the wire.
- 7.1 — per-session model/persona concurrency hardening (`SessionLLMOverrides`):
  one instance safely runs N parallel agent loops with different model/persona.
- 7.2 — capturing delegating ACP client + `Manager.Delegate`/`DelegateResult`
  (ephemeral child session over the wire, ctx-cancel → session/cancel).
- 7.3 — orchestrator `WarmTargetResolver` + `tryStartWarm`: reuse-then-autostart
  routing (running&&!external → reuse; else autostart → start+reuse; else
  off/external/unregistered/cap → cold). `Manager.EnsureInstance` (no activeID
  mutation) + `WarmDelegate` (per-instance cap via `Instance.inflight`).
- 7.4 — Projects panel integration: `Delegations`/`DelegationSpawned` computed
  fields; `DelegationInfo`/`StopReport`(cancelled count)/`EvDelegationChanged`;
  WebUI `auto`+`N loops` badges + stop toast; TUI `[auto]`/`[N loops]` suffixes;
  live SSE/IPC refresh.
- 7.5 — config UI for the two warm flags (TUI/WebUI/API + 7-locale i18n), e2e
  tests, docs.

Decisions: external (editor-launched) instances are never warm targets (always
cold); stopping an instance with in-flight delegations cancels them and they fall
back to the cold path, reaching a terminal (failed/synthesized) conclusion so the
parent never hangs (bookkeeping = single-owner parent FileStore + always-terminal
conclusion, idempotent via CorrelationID).

## Warm-instance lifecycle hygiene (C1 + C2, 2026-06-22)
- C2 promote-on-focus: a user activation that reuses a router-spawned ("warm")
  instance clears its `delegationSpawned` flag (`Manager.Activate` reuse branch),
  so a focused project is owned by the user and no longer a GC candidate.
- C1 idle auto-GC: a janitor (`Manager.StartIdleGC`/`gcIdleInstances`/
  `teardownIdleInstance` in `internal/project/delegation_gc.go`) stops warm
  instances that are delegation-spawned, not the active project, and idle (no
  in-flight delegated sessions for `WarmInstanceIdleTimeout`). Race-safe via
  `Instance.closing`+`tryBeginClose` (a concurrent delegation either keeps the
  instance or is refused a slot → ErrWarmCapReached → cold fallback). Disabled by
  default (`WarmInstanceIdleTimeout = "0"`). Change doc:
  `pando/changes/delegation_c1_c2_idle_gc_promote.md`.

## Targeting a specific project from the spawn tool (B1, 2026-06-22)
The `mesnada_spawn_agent` (in-process) and standalone MCP `spawn_agent` tools take
an optional `project` argument — a registry **id**, **display name**
(case-insensitive), or **directory path**. It is resolved against the project
registry via `orchestrator.ProjectRefResolver` (interface + `Orchestrator`
accessors `ResolveProjectRef`/`ProjectRefsSupported`/`ListProjectRefs`), backed by
`app.projectRefResolverAdapter` over `project.Service` (tries id → canonical path →
exact name). The resolved id is set on `SpawnRequest.ProjectID` (which already
flowed end-to-end to `WarmDelegate`/`resolveProjectID`), and `work_dir` defaults to
the project directory. An unknown reference fails fast at the tool boundary with a
"known projects" list, so a typo never becomes a terminal-failed warm task. Change
doc: `pando/changes/delegation_b1_project_target_from_spawn.md`.

## Caps (anti-fork-bomb / cost control)
MaxResurrections (per session per turn-chain, auto-reset on user Run), MaxDepth
(delegated-of-delegated, env PANDO_DELEGATION_DEPTH), MaxConcurrent (outstanding
per parent AND concurrent warm sessions per instance), ResurrectionTimeout.

## Configuration (Mesnada.Delegation, default-off)
config.MesnadaDelegationConfig: Enabled, InjectIntoLiveLoop, ResurrectIdleLoop,
SynthesizeFallback, MaxResurrections (4), MaxDepth (3), MaxConcurrent (8),
ResurrectionTimeout ("10m"), **ReuseWarmInstances (false), AutoStartWarmInstance
(true), WarmInstanceIdleTimeout ("0" = idle GC disabled)**. Persisted via
UpdateMesnadaDelegation; env overrides PANDO_DELEGATION_* (incl.
PANDO_DELEGATION_REUSE_WARM_INSTANCES / PANDO_DELEGATION_AUTO_START_WARM_INSTANCE /
PANDO_DELEGATION_WARM_INSTANCE_IDLE_TIMEOUT). Editable from: TUI Settings →
Subagents (mesnada.delegation.*); WebUI Settings → General → "Subagent
Delegation"; REST GET/PUT /api/v1/settings flat delegation_* fields (incl.
delegation_reuse_warm_instances / delegation_auto_start_warm /
delegation_warm_idle_timeout). The warm-routing resolver + idle GC are wired in
app.go (makeWarmTargetResolver nil when ReuseWarmInstances off → every delegated
task cold-spawns; StartIdleGC only when reuse on and timeout > 0). Caps/timeouts
defended against viper nested-default shadowing by normalizeMesnadaDelegationDefaults
(see pando/fixes/delegation_a1_viper_nested_default_shadowing.md).

## Key code map
- pkg/mesnada/models/task.go — Conclusion struct + correlation/project/depth/
  EngineWarmACP fields.
- internal/mesnada/conclusion/ — parse.go, enrich.go, format.go, brief.go.
- internal/mesnada/orchestrator/orchestrator.go + warm.go — captureConclusion,
  DelegationConfig, ProjectResolver, WarmTargetResolver/tryStartWarm,
  ProjectRefResolver + ResolveProjectRef/ListProjectRefs (B1 targeting).
- internal/llm/tools/mesnada.go + internal/mesnada/server/tools.go — spawn tool
  `project` arg → SpawnRequest.ProjectID (B1); knownProjectsHint helpers.
- internal/project/{delegation.go,delegation_client.go,delegation_gc.go,
  instance.go,manager.go} — Delegate/WarmDelegate/EnsureInstance, capturing ACP
  client, Instance.inflight/lastActiveAt/closing, DelegationInfo/StopReport/
  EvDelegationChanged, StartIdleGC/gcIdleInstances.
- internal/app/app.go — makeWarmTargetResolver + isWarmColdFallback + StartIdleGC +
  makeProjectRefResolver/projectRefResolverAdapter (B1); supervisor.
- internal/api/handlers_settings.go, internal/tui/page/settings.go,
  web-ui (GeneralSettings.tsx, settingsStore.ts, types/index.ts, 7 i18n locales).

## Tests / verification
- internal/mesnada/orchestrator/{delegation_e2e,warm}_test.go — routing
  (reuse/autostart/cold/cap/gating), warm success/failure terminal, ProjectRef
  accessors.
- internal/project/{delegation_internal,manager_warm,delegation_gc}_test.go —
  capturing client, reuse, cap, DelegationInfo, event publishing, parallel
  sessions (race), activeID-unchanged, StopReport cancelled count, **idle GC
  stop/skip-protected, close-vs-acquire race guard, promote-on-focus**.
- B1 targeting: internal/app/project_ref_resolver_test.go (id/path/name/miss),
  internal/llm/tools/mesnada_project_target_test.go (knownProjectsHint),
  internal/mesnada/orchestrator/warm_test.go (ProjectRef accessor nil/wired).
- WebUI tsc clean; go build/vet/gofmt clean. go test -race green.
