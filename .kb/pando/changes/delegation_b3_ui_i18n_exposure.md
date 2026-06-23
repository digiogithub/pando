---
created_at: 2026-06-23T08:10:37.487021053Z
updated_at: 2026-06-23T08:10:37.487021053Z
tags:
    - changes
    - delegation
    - b3
    - ui
    - i18n
---
# B3 Hot-Peer IPC Delegation — UI/i18n Exposure (Phase 5)

## What was changed

Surfaced two new config flags (`AllowExternalWarmTargets`, `AcceptDelegations`) and one new metric (`external_hits`) in all three UI layers (REST API, TUI settings, WebUI settings), with full 7-locale i18n support.

## Files touched

### Go backend
- `internal/api/handlers_settings.go`
  - `SettingsResponse` struct: added `DelegationAllowExternalWarmTargets bool` (`delegation_allow_external_warm_targets`) and `DelegationAcceptDelegations bool` (`delegation_accept_delegations`)
  - `SettingsUpdateRequest` struct: added the same two fields as `*bool` pointers with `omitempty`
  - `buildSettingsResponse()`: mapped both fields from `cfg.Mesnada.Delegation.*`
  - PUT handler guard: extended the `req.* != nil` OR condition to include both new fields
  - PUT apply block: added `if req.DelegationAllowExternalWarmTargets != nil { del.AllowExternalWarmTargets = *req... }` and same for `AcceptDelegations`

- `internal/tui/page/settings.go`
  - Added two `settings.FieldToggle` rows after `warmQueueDepth` in `buildSubagentsSection`:
    - Key `mesnada.delegation.allowExternalWarmTargets`
    - Key `mesnada.delegation.acceptDelegations`
  - Added two `case` blocks in the `saveDelegation` switch for the same keys (using `strconv.ParseBool` pattern like the existing warm toggles)

- `internal/tui/page/orchestrator.go`
  - `delegationMetricsText()`: appended ` · ext=<N>` to the dashboard header line when `ExternalHits > 0`

### Web UI TypeScript
- `web-ui/src/types/index.ts`
  - `DelegationMetrics` interface: added `external_hits: number`
  - `SettingsConfig` interface: added `delegation_allow_external_warm_targets: boolean` and `delegation_accept_delegations: boolean`
- `web-ui/src/stores/settingsStore.ts`: added defaults `delegation_allow_external_warm_targets: false` and `delegation_accept_delegations: false`
- `web-ui/src/components/settings/GeneralSettings.tsx`: added two `<Toggle>` blocks after `delegationWarmQueueDepth` input, bound to the new fields
- `web-ui/src/components/orchestrator/OrchestratorView.tsx`: added `external_hits` `<Metric>` next to `warm_hits` in `DelegationMetricsBar`, shown only when `metrics.external_hits > 0`

### i18n locales (all 7 files)
Under `settings.general` in each locale:
- `delegationAllowExternalWarmTargets` + `delegationAllowExternalWarmTargetsDescription`
- `delegationAcceptDelegations` + `delegationAcceptDelegationsDescription`

Under `orchestrator.delegationMetrics`:
- `externalHits`

Translations provided for all 6 non-English locales (es, de, fr, pt, ja, zh).

## New JSON field names (API)
- `delegation_allow_external_warm_targets` (bool, GET/PUT)
- `delegation_accept_delegations` (bool, GET/PUT)
- `external_hits` (int, read-only in delegation metrics snapshot)

## Verification
- `gofmt -l` on all modified Go files: empty (clean)
- `go build ./internal/api/... ./internal/tui/...`: no errors
- `npx tsc --noEmit` in web-ui: no errors
- `python3 -m json.tool` on all 7 locale JSON files: all valid

## Constraints respected
- Did NOT touch: internal/config, internal/project, internal/ipc, internal/app, cmd/, orchestrator logic
- Backend config structs unchanged; only read existing `AllowExternalWarmTargets` / `AcceptDelegations` fields
