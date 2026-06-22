---
created_at: 2026-06-21T21:51:57.668005465Z
updated_at: 2026-06-21T21:51:57.668005465Z
tags:
    - change
    - mesnada
    - delegation
    - phase7
    - acp
    - projects
    - warm-instance
    - orchestrator
    - routing
---
# Change: Delegation Phase 7.3 — Warm-target routing (reuse-then-autostart)

Implemented 2026-06-21. Status: DONE, verified. Third sub-phase of the Phase 7
re-plan `pando/plans/delegation_phase7_warm_instance_replan.md`. Builds on 7.1
(per-session concurrency hardening) and 7.2 (capturing ACP client +
`Manager.Delegate`). Wires the warm transport into the orchestrator spawn path so
a delegated task whose project is known runs inside an already-running ("warm")
per-project ACP instance instead of cold-spawning a `pando -p` CLI subprocess.

## Problem
After 7.2 the project `Manager` could open a session in a warm child, run a
prompt and capture the `<pando:conclusion>` block (`Manager.Delegate`), but
nothing called it: the orchestrator always cold-spawned. 7.3 adds the routing
(reuse-then-autostart, external exclusion, concurrency cap), a no-activeID
start-or-reuse Manager path, the config flags, and the synthesis of a terminal
`models.Task` (engine=`warm-acp`) so the existing Phases 1–6 pipeline
(`conclusion.Enrich` → supervisor) consumes the result unchanged.

## Design (no import cycle)
- The orchestrator must not import `internal/project`. Same pattern as
  `ProjectResolver`/`ModelResolver`: a narrow `WarmTargetResolver` interface is
  defined IN the orchestrator and an adapter in `internal/app` bridges it to
  `project.Manager`.
- `WarmTargetResolver.RunWarm(ctx, projectID, projectPath, prompt) (*WarmRunResult, error)`
  returns the sentinel `orchestrator.ErrNoWarmTarget` to mean "take the cold
  path"; any other error is a genuine warm-run failure (→ terminal-failed task).

## Files & symbols
- `internal/config/config.go` — `MesnadaDelegationConfig` gains
  `ReuseWarmInstances` (master, default OFF) + `AutoStartWarmInstance` (default
  TRUE). viper defaults (`reuseWarmInstances=false`, `autoStartWarmInstance=true`)
  + env overrides `PANDO_DELEGATION_REUSE_WARM_INSTANCES` /
  `PANDO_DELEGATION_AUTO_START_WARM_INSTANCE`. `MaxConcurrent` doc note: now also
  bounds concurrent warm sessions per instance.
- `internal/project/instance.go` — `Instance.inflight` counter +
  `acquireDelegationSlot(max)` / `releaseDelegationSlot()` /
  `InflightDelegations()` (cap enforcement + panel count; max<=0 = unlimited).
- `internal/project/errors.go` — `ErrWarmCapReached`, `ErrProjectNotRegistered`.
- `internal/project/delegation.go` —
  - `Delegate` refactored to a thin wrapper over new `delegateOn(ctx, projectID, inst, prompt)`.
  - `EnsureInstance(ctx, projectID, autoStart) (*Instance, error)` — reuse a
    running manager-owned instance, else (autoStart) `spawnChild` WITHOUT touching
    `activeID`; refuses external instances (`ErrExternalInstance`); `ErrInstanceNotRunning`
    when reuse-only and nothing running; `ErrProjectNeedsInit` when autostart but
    no config file. Double-checked under the write lock. Publishes
    `EvStatusChanged(running)` (delegation-spawned; no `EvProjectSwitched`).
  - `resolveProjectID(ctx, projectID, projectPath)` — id passthrough else
    canonicalise path + `service.GetByPath` (→ `ErrProjectNotRegistered`).
  - `WarmDelegate(ctx, projectID, projectPath, prompt, autoStart, maxConcurrent)` —
    resolveProjectID → EnsureInstance → acquire slot (`ErrWarmCapReached`) →
    delegateOn. Single project-package entry point for the adapter.
- `internal/mesnada/orchestrator/warm.go` (NEW) — `ErrNoWarmTarget`,
  `WarmRunResult`, `WarmTargetResolver` interface, and `tryStartWarm(task)`:
  gates on `warmResolver != nil && delegation.Enabled && ReuseWarmInstances &&
  (ProjectID||ProjectPath)`; calls `RunWarm`; on `ErrNoWarmTarget` returns false
  (task untouched → cold path); otherwise sets `Engine=warm-acp`,
  StartedAt/CompletedAt, and either completed (Output, ACPSessionID=childSession,
  ExitCode=0) or failed (Error), then drives `onTaskComplete` exactly once.
- `internal/mesnada/orchestrator/orchestrator.go` — `warmResolver` field;
  `DelegationConfig.ReuseWarmInstances`; `Config.WarmTargetResolver` wired in
  `New`; `startTask` calls `tryStartWarm` first, falling back to `manager.Spawn`.
- `internal/app/app.go` — `warmTargetResolverFunc` adapter type +
  `makeWarmTargetResolver(mgr, cfg)` (nil when ReuseWarmInstances off) mapping
  project sentinels (`ErrInstanceNotRunning`/`ErrExternalInstance`/
  `ErrProjectNeedsInit`/`ErrProjectNotRegistered`/`ErrWarmCapReached`) →
  `ErrNoWarmTarget` via `isWarmColdFallback`; `convertMesnadaConfig` forwards
  `ReuseWarmInstances`; resolver injected after `ProjectResolver`.

## Bookkeeping (decision 4: always terminal)
On warm-run failure or cancellation the task is marked failed and still flows
through `onTaskComplete` → `captureConclusion` (SynthesizeFallback yields a
failed/blocked conclusion) → supervisor, so a delegated run can never leave the
parent loop hanging. ACPSessionID stores the child session id for correlation.
Stop-with-inflight → cold-fallback (decision 3) is deferred to Phase 7.4; for 7.3
a cancellation surfaces as a terminal-failed warm task.

## Tests (all green under -race)
- `internal/project/delegation_internal_test.go` (internal, io.Pipe-wired fake
  agent): `TestEnsureInstanceReusesRunning` (reuse, no activeID change),
  `TestWarmDelegateReuseCaptures` (capture + slot released),
  `TestWarmDelegateCapReached` (blocking agent holds the only slot,
  maxConcurrent=1 → `ErrWarmCapReached`).
- `internal/project/manager_warm_test.go` (service-backed, no spawn):
  `TestEnsureInstanceReuseOnlyNotRunning` (`ErrInstanceNotRunning`),
  `TestEnsureInstanceAutoStartNoConfig` (`ErrProjectNeedsInit`),
  `TestWarmDelegateUnregisteredProject` (`ErrProjectNotRegistered`).
- `internal/mesnada/orchestrator/warm_test.go` (fake resolver):
  success→completed+warm-acp+ACPSessionID+conclusion, run-failure→terminal failed,
  `ErrNoWarmTarget`→cold fallback (task untouched), gating matrix
  (disabled/flag-off/no-project), nil resolver→cold.
- `go build ./...` OK; `go test -race ./internal/project ./internal/mesnada/orchestrator`
  green; `go test ./internal/llm/agent ./internal/api ./pkg/mesnada/models` green.

## Known pre-existing issue (NOT introduced here)
`internal/config` `TestMesnadaDelegationDefaults` fails: via the full `Load()`
path (config file present) the nested `mesnada.delegation.*` viper defaults are
shadowed and unmarshal to zero (affects MaxResurrections/MaxDepth/MaxConcurrent/
ResurrectionTimeout — all of Phase 0, not just 7.3). Proven pre-existing: removing
the two new 7.3 `SetDefault` lines reproduces the identical failure, and an
isolated `setDefaults`+`viper.Unmarshal` probe yields the correct values
(MaxConcurrent=8, AutoStartWarmInstance=true). Practical impact on 7.3: with a
config file present, `AutoStartWarmInstance` may fall back to false (reuse-only)
and `MaxConcurrent` to 0 (treated as unlimited by acquireDelegationSlot). The
feature is default-OFF so this only matters once a user enables
ReuseWarmInstances; Phase 7.5 (UI persistence) will set the value explicitly. A
proper fix to viper nested-default shadowing is out of 7.3 scope.

## Next (Phase 7.4)
Projects panel integration (WebUI + TUI): pre-warm/stop = manage delegation
targets; surface `InflightDelegations()` per row ("N delegated loops");
stop-with-inflight → cancel + cold fallback + UI warning; `ManagerEvent` live
updates; optionally target a registered project by id from the spawn tool.
