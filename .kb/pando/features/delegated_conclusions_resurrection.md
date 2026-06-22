---
created_at: 2026-06-22T12:06:06.889068075Z
updated_at: 2026-06-22T12:07:22.724403314Z
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
    - metrics
---
# Feature: Delegated-Task Conclusions + Agent-Loop Resurrection

Status: COMPLETE (Phases 0-6, 2026-06-21) + Phase 7 warm per-project instance reuse COMPLETE (7.1-7.5, 2026-06-22) + lifecycle hygiene C1/C2 (2026-06-22) + project targeting B1 (2026-06-22) + warm-queue-under-load A3 (2026-06-22) + delegation metrics/telemetry E1 (2026-06-22). Default-OFF.

## What it does
When the agent delegates work to subagent tasks through the mesnada orchestrator, this feature captures each subagent's result as a structured conclusion and feeds it back into the parent loop (live injection Case A or idle resurrection Case B) instead of fire-and-forget. The subagent emits ONLY a thin `<pando:conclusion>` sentinel; the SOFTWARE fills launch metadata from the Task record. `mesnada_await` lets the model end its turn and be resurrected on a join policy.

## Warm per-project instance reuse (Phase 7)
Optionally route a delegated task whose project is known to a running warm per-project ACP instance instead of cold-spawning a `pando -p` CLI, capturing the conclusion over the wire. 7.3 orchestrator WarmTargetResolver + tryStartWarm (reuse-then-autostart-else-cold) + WarmDelegate (per-instance cap via Instance.inflight); 7.4 Projects panel; 7.5 config UI + e2e + docs. External instances are never warm targets. Stopping an instance cancels in-flight delegations → cold path → always-terminal conclusion (idempotent via CorrelationID).

## Lifecycle hygiene (C1 + C2)
C2 promote-on-focus clears delegationSpawned on Manager.Activate. C1 idle auto-GC janitor stops delegation-spawned, non-active, idle, zero-inflight instances after WarmInstanceIdleTimeout ("0"=off). delegation_c1_c2_idle_gc_promote.md.

## Targeting a specific project from the spawn tool (B1)
mesnada_spawn_agent + MCP spawn_agent take an optional `project` arg (id/name/path) via orchestrator.ProjectRefResolver. delegation_b1_project_target_from_spawn.md.

## Delegation metrics & panel telemetry (E1)
Orchestrator keeps lock-free DelegationMetrics (atomic counters): warm_attempts/hits/failures, cold_fallbacks, cap_rejections (cap subset via new orchestrator.ErrWarmCapReached wrapping ErrNoWarmTarget), resurrections + live_injections (recorded by the supervisor on successful Resume / inject), plus derived warm_hit_rate. Surfaced via Orchestrator.DelegationMetrics(), GET /api/v1/orchestrator/delegation/metrics, a TUI dashboard header line and a WebUI DelegationMetricsBar — both shown only once there is activity. Read-only, no config knob, default unchanged. delegation_e1_metrics.md.

## Warm-instance queue under load (A3)
WarmQueueDepth > 0 lets up to that many over-cap delegations BLOCK in a bounded FIFO per Instance; beyond cap+depth they cold-fall-back. Instance.cond/waiters + acquireDelegationSlotOrQueue. Default 0 = prior behaviour. delegation_a3_warm_queue_depth.md.

## Caps
MaxResurrections, MaxDepth, MaxConcurrent (outstanding per parent AND concurrent warm sessions per instance; over it a warm task queues up to WarmQueueDepth then cold-falls-back), ResurrectionTimeout.

## Configuration (Mesnada.Delegation, default-off)
config.MesnadaDelegationConfig: Enabled, InjectIntoLiveLoop, ResurrectIdleLoop, SynthesizeFallback, MaxResurrections (4), MaxDepth (3), MaxConcurrent (8), ResurrectionTimeout ("10m"), ReuseWarmInstances (false), AutoStartWarmInstance (true), WarmInstanceIdleTimeout ("0"=off), WarmQueueDepth (0=cold-fallback at cap). E1 metrics are read-only (no config). Caps defended against viper nested-default shadowing by normalizeMesnadaDelegationDefaults.

## Key code map
- internal/mesnada/orchestrator/{orchestrator.go,warm.go,metrics.go} — captureConclusion, WarmTargetResolver/tryStartWarm, ProjectRefResolver (B1), DelegationMetrics + ErrWarmCapReached + DelegationMetrics()/RecordResurrection()/RecordLiveInjection() (E1).
- internal/api/handlers_orchestrator.go — GET /api/v1/orchestrator/delegation/metrics (E1).
- internal/app/delegation_supervisor.go — records resurrection / live injection (E1).
- internal/project/{delegation.go,delegation_gc.go,instance.go,manager.go} — WarmDelegate/EnsureInstance, Instance.inflight/cond/waiters, acquireDelegationSlotOrQueue (A3), StartIdleGC (C1).
- web-ui orchestrator (OrchestratorView.tsx DelegationMetricsBar, orchestratorStore.ts, types/index.ts) for E1; settings surface for config knobs.

## Tests / verification
- orchestrator/{delegation_e2e,warm,metrics}_test.go — routing, warm terminal, ProjectRef, metrics counters.
- project/{delegation_internal,manager_warm,delegation_gc,instance_queue}_test.go.
- E1: orchestrator/metrics_test.go (snapshot hit-rate + nil-safe; tryStartWarm records hit/failure/cold/cap; record resurrection/injection — race-clean).
- WebUI tsc clean; go build/vet/gofmt clean; go test -race green (pre-existing TestTryStartWarmGating TempDir-cleanup flake under -race only).