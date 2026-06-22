---
created_at: 2026-06-22T07:08:45.306018999Z
updated_at: 2026-06-22T07:08:45.306018999Z
tags:
    - change
    - mesnada
    - delegation
    - phase7
    - warm-instance
    - config
    - webui
    - tui
    - i18n
    - tests
    - docs
---
# Change: Delegation Phase 7.5 — Config UI, e2e tests, docs

Implemented 2026-06-22. Status: DONE, verified. Fifth and final sub-phase of the
Phase 7 re-plan `pando/plans/delegation_phase7_warm_instance_replan.md`. Builds on
7.1 (per-session concurrency), 7.2 (capturing ACP client), 7.3 (warm-target
routing) and 7.4 (Projects panel integration). Exposes the two warm-reuse config
flags across all three UIs, fills the remaining e2e test gaps, and updates docs.

## What changed

### Config UI — expose ReuseWarmInstances + AutoStartWarmInstance
The two flags were added to config + routing in 7.3 but had no UI surface. Now
editable in all three layers, following the Phase 5 settings pattern:
- **API** (`internal/api/handlers_settings.go`): added
  `DelegationReuseWarmInstances` + `DelegationAutoStartWarm` to both
  `SettingsResponse` (GET mapping from `cfg.Mesnada.Delegation.*`) and
  `SettingsUpdateRequest` (`*bool`), plus the apply block (the guard condition now
  also fires on these two fields) calling `config.UpdateMesnadaDelegation`.
- **TUI** (`internal/tui/page/settings.go`): two `FieldToggle` rows
  (`mesnada.delegation.reuseWarmInstances`, `…autoStartWarmInstance`) in the
  delegation field list; added both keys to the save dispatch `case` and to
  `saveMesnadaDelegation` (strconv.ParseBool → del.ReuseWarmInstances /
  del.AutoStartWarmInstance).
- **WebUI**: two `<Toggle>`s in `GeneralSettings.tsx`
  (`delegation_reuse_warm_instances`, `delegation_auto_start_warm`); added the
  fields to `types/index.ts` (`SettingsConfig`) and `settingsStore.ts` defaults
  (false / true); added 4 i18n keys
  (`delegationReuseWarmInstances{,Description}`, `delegationAutoStartWarm{,Description}`)
  to all 7 locales (en, es, de, fr, ja, pt, zh).

### Tests (e2e gaps from the re-plan)
Added to `internal/project/delegation_internal_test.go`:
- `TestWarmInstanceServesParallelSessions` — a new `parallelFakeAgent` (unique
  session id per NewSession + a barrier in Prompt) drives 2 `WarmDelegate` calls
  on ONE warm instance; the barrier only releases once both prompts have arrived,
  proving they run in parallel (neither blocks). Asserts distinct in-flight
  sessions, in-flight peak == 2, per-session conclusion captured without
  cross-talk, and slots return to 0. Passes under `-race`.
- `TestWarmDelegateDoesNotChangeActiveID` — a warm delegation leaves
  `Manager.ActiveID()` empty (delegation must never switch the user's focus).
- `TestStopReportCancelsInflightDelegations` — builds a minimal Instance
  (cmd=&exec.Cmd{} so Process-nil guards skip, closed errCh, recording cancel
  func) with 2 in-flight slots; `StopReport` returns cancelled==2, calls cancel,
  and removes the instance (decision 3). The cold-path fallback + always-terminal
  conclusion for those cancelled sessions is covered by the orchestrator's
  existing `TestTryStartWarmRunFailureIsTerminal`.
Routing tests (running→reuse, autostart, off/external/unregistered→cold, cap,
gating, nil-resolver) were already present from 7.3/7.4
(`warm_test.go`, `manager_warm_test.go`).

### Docs
- README feature bullet: new "Warm per-project instance reuse" entry (parallel
  loops, panel badges, stop→cold-fallback, config flags + env vars).
- KB feature doc `pando/features/delegated_conclusions_resurrection.md` updated to
  mark Phase 7 complete and document the warm-reuse flags/code map.

## Files touched
- internal/api/handlers_settings.go
- internal/tui/page/settings.go
- web-ui/src/components/settings/GeneralSettings.tsx
- web-ui/src/stores/settingsStore.ts
- web-ui/src/types/index.ts
- web-ui/src/i18n/locales/{en,es,de,fr,ja,pt,zh}.json
- internal/project/delegation_internal_test.go
- README.md

## Verification
- `go build ./...` clean; `go vet` on touched packages clean.
- `go test -race ./internal/project/ ./internal/mesnada/orchestrator/ ./internal/api/`
  all green (incl. the 3 new tests).
- web-ui `npx tsc --noEmit` clean; all 7 locale JSON files parse.
- gofmt clean on edited Go files (pre-existing unformatted files elsewhere
  untouched).

## Known caveat (unchanged from 7.3)
`TestMesnadaDelegationDefaults` (internal/config) still fails due to a viper
nested-default shadowing via Load — pre-existing, not introduced here.

## Phase 7 status
With 7.5 done, the entire Phase 7 re-plan (7.1-7.5) is COMPLETE. Future optional
enhancements remain: targeting a registered project by id from the spawn tool/UI,
idle auto-GC of router-spawned instances, and re-attach-on-reconnect of warm
delegated sessions after a parent restart.
