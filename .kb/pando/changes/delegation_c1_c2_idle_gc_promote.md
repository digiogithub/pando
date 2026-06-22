---
created_at: 2026-06-22T08:24:24.597281818Z
updated_at: 2026-06-22T08:24:24.597281818Z
tags:
    - change
    - delegation
    - warm-instance
    - lifecycle
    - project-manager
---
# Change C1 + C2 — warm-instance idle auto-GC + promote-on-focus

Date: 2026-06-22. Backlog items **C1** (idle auto-GC of router-spawned warm
instances) and **C2** (promote a delegation-spawned instance to user-activated on
focus) from `pando/plans/delegation_future_improvements.md`. Both ship together as
the "lifecycle / resource management" slice of the delegation feature.

## Motivation
A warm instance auto-started by the delegation router (`delegationSpawned == true`)
previously persisted in the Projects panel until the user stopped it — even with
zero in-flight delegated sessions and no user activation — slowly leaking child
processes (C1). And a user clicking such an instance should stop it being a GC
candidate (C2).

## C2 — promote-on-focus (was already in place; now verified + tested)
`Manager.Activate`'s reuse branch (manager.go) already calls
`inst.markDelegationSpawned(false)` when a user activation reuses an existing
instance, so a user-focused activation claims a router-spawned instance. Both
activation entry points route through `Activate`: TUI `tui.go` and API
`handlers_projects.go`. Added `TestActivatePromotesDelegationSpawnedInstance`
(internal, stub Service + temp dir with `.pando.toml`) locking this behaviour.

## C1 — idle auto-GC
### internal/project/instance.go
- New `Instance` fields (guarded by mu): `lastActiveAt time.Time` (idle clock) and
  `closing bool` (GC-claimed flag).
- `acquireDelegationSlot` now refuses when `closing` (returns false → caller takes
  cold path) and stamps `lastActiveAt`. `releaseDelegationSlot` stamps it too.
- New `tryBeginClose() bool`: atomically sets `closing` only when `inflight==0 &&
  !closing` — this is the race guard. A delegation acquiring a slot concurrently
  either wins the slot (GC then sees inflight>0 and skips) or is refused once
  `closing` is set (→ ErrWarmCapReached → cold fallback). No lost/duplicated work.
- New `idleFor(now) time.Duration` for the idle measurement.

### internal/project/delegation_gc.go (new)
- `StartIdleGC(idleTimeout)`: launches a janitor (ticker = idleTimeout/2, floored
  at `gcMinInterval = 30s`, capped at idleTimeout) bound to `m.ctx`. No-op when
  idleTimeout <= 0.
- `gcIdleInstances(idleTimeout) []string`: one sweep. Under `m.mu`, for each
  instance that is `!= activeID`, `isDelegationSpawned()`, `idleFor >= timeout`,
  and `tryBeginClose()` succeeds, removes it from the map and tears it down
  (returns the stopped ids; used by tests).
- `teardownIdleInstance`: `cancel()` + SIGTERM (guarded nil Process) + 5s drain →
  Kill; publishes `EvDelegationChanged(count 0)` + `EvStatusChanged(stopped)`. The
  spawnChild monitor records the persistent stopped status on exit.

### internal/project/manager.go
- `spawnChild` initialises `lastActiveAt: time.Now()` so a freshly spawned but
  unused instance still gets a full idle grace period before GC.

### Config (internal/config/config.go)
- New `MesnadaDelegationConfig.WarmInstanceIdleTimeout string` (json
  `warmInstanceIdleTimeout`). Go duration; `"0"`/empty disables the GC (default).
  viper default `"0"`, env `PANDO_DELEGATION_WARM_INSTANCE_IDLE_TIMEOUT`, and
  `normalizeMesnadaDelegationDefaults` fills blank→"0" (consistent with the A1
  shadowing fix so GET round-trips a stable value).

### Wiring (internal/app/app.go)
- In the `cfg.Mesnada.Enabled` block, after `makeWarmTargetResolver`: when
  `ReuseWarmInstances` is on and `WarmInstanceIdleTimeout` parses to a positive
  duration, call `ProjectManager.StartIdleGC(idle)` (logged).

### Config UI (mirrors the resurrectionTimeout pattern from Phase 7.5)
- API `internal/api/handlers_settings.go`: GET `delegation_warm_idle_timeout`
  (via `warmIdleTimeoutOrDefault` → "0"); PUT validates with `time.ParseDuration`
  ("0 to disable, e.g. 10m, 1h") and persists.
- TUI `internal/tui/page/settings.go`: FieldText "Delegation Warm Instance Idle
  Timeout" + `warmIdleTimeoutString` + save case (blank→"0", duration-validated).
- WebUI `GeneralSettings.tsx` TextInput (disabled unless delegation + reuse on),
  `types/index.ts` + `settingsStore.ts` default `'0'`, and label
  `delegationWarmIdleTimeout` added to all 7 locales (en/es/de/fr/ja/pt/zh).

## Tests
- `internal/project/delegation_gc_test.go` (new): `TestGCIdleStopsIdleSpawnedInstance`,
  `TestGCIdleSkipsProtectedInstances` (user/active/busy/fresh all skipped),
  `TestInstanceCloseBlocksSlotAcquire` (race guard), and
  `TestActivatePromotesDelegationSpawnedInstance` (C2).

## Verification
- `go build ./...` green; `go vet` (project/api/tui/page/app) clean; gofmt clean.
- `go test -race ./internal/project ./internal/config` green;
  `go test ./internal/project ./internal/config ./internal/api ./internal/llm/agent ./internal/app` green.
- WebUI `npx tsc --noEmit` exit 0; all 7 locale JSON parse.

## Safety / defaults
Default-off-consistent: `WarmInstanceIdleTimeout` defaults to "0" (GC disabled),
so today's behaviour (warm instances persist until panel-stopped) is unchanged
until explicitly enabled. Only delegation-spawned, non-active, idle instances with
no in-flight loops are ever GC'd; user-activated instances are never touched.

## Related
- Backlog: `pando/plans/delegation_future_improvements.md` (C1, C2 → DONE).
- Feature: `pando/features/delegated_conclusions_resurrection.md`.
- Prior: `pando/changes/delegation_phase7_4_projects_panel_integration.md`
  (delegationSpawned flag) and `..._phase7_5_config_ui_tests_docs.md` (UI pattern).
