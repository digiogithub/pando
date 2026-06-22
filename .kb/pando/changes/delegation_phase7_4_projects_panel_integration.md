---
created_at: 2026-06-22T05:56:24.238360627Z
updated_at: 2026-06-22T05:56:24.238360627Z
tags:
    - change
    - mesnada
    - delegation
    - phase7
    - projects
    - warm-instance
    - webui
    - tui
    - sse
---
# Change: Delegation Phase 7.4 — Projects panel integration (WebUI + TUI)

Implemented 2026-06-22. Status: DONE, verified. Fourth sub-phase of the Phase 7
re-plan `pando/plans/delegation_phase7_warm_instance_replan.md`. Builds on 7.1
(per-session concurrency), 7.2 (capturing ACP client + `Manager.Delegate`) and
7.3 (warm-target routing). Surfaces warm-delegation state in the Projects panel
(WebUI + TUI), publishes live delegation events, and reports cancelled delegated
loops when an instance is stopped (decision 3 of the re-plan).

## What changed

### Manager bookkeeping + events (internal/project)
- `Instance` (instance.go): new `delegationSpawned bool` (guarded by mu) +
  `markDelegationSpawned(v)` / `isDelegationSpawned()`. Distinguishes a router
  auto-started ("warm") instance from a user-activated one.
- `Project` domain struct (project.go): two computed (non-persisted) fields,
  `Delegations int` and `DelegationSpawned bool`, populated on demand (same
  pattern as the existing `External` field).
- `Manager` (manager.go):
  - New event type `EvDelegationChanged` ("delegation_changed") + `Count int`
    field on `ManagerEvent` (carries the current in-flight delegated-session
    count; zero for other event types).
  - `DelegationInfo(projectID) (inflight int, spawned, running bool)` — reads the
    in-memory instance; all-zero when no manager-owned instance exists.
  - `publishDelegationChanged(projectID, count)` — nil-broker-safe (tests build
    `&Manager{}` directly without a broker).
  - `StopReport(ctx, projectID) (cancelled int, err error)` — Stop with
    bookkeeping; returns how many in-flight delegated sessions were running at
    stop time. `Stop` is now a thin wrapper that discards the count (preserves
    both existing callers). Bookkeeping decision 4 already guarantees those
    cancelled sessions reach a terminal (failed/synthesized) conclusion via the
    cold-path fallback, so the count is purely informational for the UI warning.
  - `Activate` reuse-branch now calls `inst.markDelegationSpawned(false)` so a
    user activation claims a previously router-spawned instance as user-focused.
- `delegation.go`:
  - `EnsureInstance` auto-start branch tags the new child
    `inst.markDelegationSpawned(true)`.
  - `WarmDelegate` publishes `EvDelegationChanged` after acquiring the slot
    (count N) and again after releasing it (count N-1), so panels refresh live.

### API (internal/api/handlers_projects.go)
- `projectResponse`: new `delegations` (omitempty) and `delegation_spawned`
  (omitempty) JSON fields; `enrichRuntime` populates them from `DelegationInfo`.
- `handleStopProject` now calls `StopReport` and returns
  `cancelled_delegations` in the JSON body (response shape switched to
  map[string]interface{}).
- SSE `handleProjectEvents`: payload now includes `delegations` (the event
  `Count`) and maps `EvDelegationChanged` → SSE event name `delegation_changed`.

### WebUI
- `types/index.ts` Project: `delegations?: number`, `delegation_spawned?: boolean`.
- `stores/projectStore.ts`: `stopProject` reads `cancelled_delegations` from the
  stop response and shows an info toast ("cancelled N delegated loop(s) … cold
  path") when >0; new `delegation_changed` SSE listener → `fetchProjects()`.
- `components/projects/ProjectsView.tsx`: per-row badges in the Status cell — an
  `auto` badge (faRobot) when `delegation_spawned`, and a spinning
  `N loop(s)` badge (faCircleNotch) when `delegations > 0`.

### TUI
- `tui.go` `listProjectsEnriched`: populates `Delegations` / `DelegationSpawned`
  per row via `DelegationInfo`. `stopProject` uses `StopReport` and appends
  "(cancelled N delegated loops)" to the info message.
- `components/dialog/projects.go` `renderProjectItem`: appends `[auto]` (muted)
  and `[N loops]` (warning colour) suffixes next to the existing `[external]` /
  `[active]` markers.

## Files/symbols touched
- internal/project/instance.go, project.go, manager.go, delegation.go
- internal/api/handlers_projects.go
- internal/tui/tui.go, internal/tui/components/dialog/projects.go
- web-ui/src/types/index.ts, src/stores/projectStore.ts,
  src/components/projects/ProjectsView.tsx

## Tests
- internal/project/delegation_internal_test.go (new):
  - `TestDelegationInfoReflectsState` — inflight count + spawned flag + running,
    and zero values for an unknown project.
  - `TestWarmDelegatePublishesDelegationEvents` — wires a real broker, subscribes,
    runs `WarmDelegate`, drains buffered events and asserts the delegation count
    went 1 then back to 0.
- Verified: `go build ./...`; `go test -race ./internal/project
  ./internal/mesnada/orchestrator`; `go test ./internal/llm/agent ./internal/api
  ./pkg/mesnada/models`; `go vet` on touched packages; `npx tsc --noEmit` for the
  web-ui; gofmt clean (handlers_projects.go reflowed by gofmt for the new fields).

## Notes / deferred to 7.5
- i18n of the new WebUI strings is explicitly part of 7.5 ("panel integration +
  i18n"); the new labels are inline English for now, consistent with the rest of
  ProjectsView which already hardcodes English.
- Targeting a registered project by id from the spawn tool/UI, and idle auto-GC
  of router-spawned instances, remain future enhancements.
- Pre-existing `TestMesnadaDelegationDefaults` (internal/config) failure from 7.3
  (viper nested-default shadowing) is unrelated and still unfixed.

## Next (7.5)
Tests/docs/config UI/e2e: concurrency e2e (2+ parallel warm sessions), routing
tests, stop-with-inflight test, expose `ReuseWarmInstances` +
`AutoStartWarmInstance` in TUI/WebUI/API per the Phase 5 pattern + i18n, update
feature doc + README.
