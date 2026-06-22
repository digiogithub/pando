---
created_at: 2026-06-21T19:27:43.923721554Z
updated_at: 2026-06-22T07:08:18.37058859Z
tags:
    - feature
    - mesnada
    - delegation
    - orchestrator
    - agent
    - warm-instance
    - projects
    - config
---
# Feature: Delegated-Task Conclusions + Agent-Loop Resurrection

Status: COMPLETE (Phases 0-6, 2026-06-21) + Phase 7 warm per-project instance
reuse COMPLETE (7.1-7.5, 2026-06-22). Plans:
`pando/plans/delegated_conclusion_resurrection_plan.md` and the Phase 7 re-plan
`pando/plans/delegation_phase7_warm_instance_replan.md`. **Default-OFF.**

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

## Caps (anti-fork-bomb / cost control)
MaxResurrections (per session per turn-chain, auto-reset on user Run), MaxDepth
(delegated-of-delegated, env PANDO_DELEGATION_DEPTH), MaxConcurrent (outstanding
per parent AND concurrent warm sessions per instance), ResurrectionTimeout.

## Configuration (Mesnada.Delegation, default-off)
config.MesnadaDelegationConfig: Enabled, InjectIntoLiveLoop, ResurrectIdleLoop,
SynthesizeFallback, MaxResurrections (4), MaxDepth (3), MaxConcurrent (8),
ResurrectionTimeout ("10m"), **ReuseWarmInstances (false), AutoStartWarmInstance
(true)**. Persisted via UpdateMesnadaDelegation; env overrides PANDO_DELEGATION_*
(incl. PANDO_DELEGATION_REUSE_WARM_INSTANCES /
PANDO_DELEGATION_AUTO_START_WARM_INSTANCE). Editable from: TUI Settings →
Subagents (mesnada.delegation.*); WebUI Settings → General → "Subagent
Delegation"; REST GET/PUT /api/v1/settings flat delegation_* fields (incl.
delegation_reuse_warm_instances / delegation_auto_start_warm). The warm-routing
resolver is wired in app.go via makeWarmTargetResolver (nil when
ReuseWarmInstances is off → every delegated task cold-spawns).

## Key code map
- pkg/mesnada/models/task.go — Conclusion struct + correlation/project/depth/
  EngineWarmACP fields.
- internal/mesnada/conclusion/ — parse.go, enrich.go, format.go, brief.go.
- internal/mesnada/orchestrator/orchestrator.go + warm.go — captureConclusion,
  DelegationConfig, ProjectResolver, WarmTargetResolver/tryStartWarm.
- internal/project/{delegation.go,delegation_client.go,instance.go,manager.go} —
  Delegate/WarmDelegate/EnsureInstance, capturing ACP client, Instance.inflight,
  DelegationInfo/StopReport/EvDelegationChanged.
- internal/app/app.go — makeWarmTargetResolver + isWarmColdFallback; supervisor.
- internal/api/handlers_settings.go, internal/tui/page/settings.go,
  web-ui (GeneralSettings.tsx, settingsStore.ts, types/index.ts, 7 i18n locales).

## Tests / verification
- internal/mesnada/orchestrator/{delegation_e2e,warm}_test.go — routing
  (reuse/autostart/cold/cap/gating), warm success/failure terminal.
- internal/project/{delegation_internal,manager_warm}_test.go — capturing client,
  reuse, cap, DelegationInfo, event publishing, **parallel sessions (race),
  activeID-unchanged, StopReport cancelled count**.
- WebUI tsc clean; go build/vet/gofmt clean. go test -race green.
