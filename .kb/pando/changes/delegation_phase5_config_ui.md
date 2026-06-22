---
created_at: 2026-06-21T17:59:30.8956377Z
updated_at: 2026-06-21T17:59:49.825365369Z
tags:
    - change
    - mesnada
    - delegation
    - phase5
    - config
    - ui
    - tui
    - webui
    - api
    - i18n
---
# Change: Delegation Phase 5 — Config UI (TUI + WebUI + API + i18n)

Implemented 2026-06-21. Status: DONE, verified. Part of plan
`pando/plans/delegated_conclusion_resurrection_plan.md` (Phase 5). Builds on
Phases 0-4. **Still default-OFF** — this phase only EXPOSES the existing
`MesnadaDelegationConfig` flags (created in Phase 0) through the settings panels;
no new config knobs and no runtime-behavior change beyond letting a user toggle
the delegation feature that Phases 1-4 already wired.

## What changed & why
Phases 0-4 added the delegation config struct + `UpdateMesnadaDelegation` and all
the runtime consumers, but the flags could only be edited by hand in the TOML/JSON
config file. Phase 5 surfaces them in the TUI settings page, the WebUI General
settings panel, and the REST settings API (with i18n), so the feature can be
enabled/tuned from the UI as the plan requires.

Eight exposed fields (all from `config.MesnadaDelegationConfig`):
`Enabled`, `InjectIntoLiveLoop`, `ResurrectIdleLoop`, `SynthesizeFallback`
(toggles) + `MaxResurrections`, `MaxDepth`, `MaxConcurrent` (ints) +
`ResurrectionTimeout` (duration string, validated via `time.ParseDuration`).
Effective defaults surfaced when unset: 4 / 3 / 8 / "10m".

## Files touched
- `internal/api/handlers_settings.go` — flat `delegation_*` fields added to
  `SettingsResponse` (read from `cfg.Mesnada.Delegation`; ints via `intOrDefault`;
  timeout via new `delegationTimeoutOrDefault`) and pointer fields to
  `SettingsUpdateRequest`. New PUT block (mirrors tool-discovery): loads
  `Mesnada.Delegation`, applies provided fields with `>= 0` int validation and
  `time.ParseDuration` timeout validation, persists via
  `config.UpdateMesnadaDelegation`. Added `"time"` import.
- `internal/tui/page/settings.go` — 8 new fields in `buildMesnadaSection`
  (`mesnada.delegation.*` keys; toggles + text; Hints; disabled-when-mesnada-off).
  New `delegationTimeoutString` display helper. `saveMesnada` routes
  `mesnada.delegation.*` to new `saveMesnadaDelegation` (separate writer, persists
  through `UpdateMesnadaDelegation`, not `UpdateMesnada`).
- `web-ui/src/types/index.ts` — 8 `delegation_*` fields in `SettingsConfig`.
- `web-ui/src/stores/settingsStore.ts` — 8 fields in `DEFAULTS`
  (enabled=false; 4/3/8/"10m"). Store PUTs the whole config; backend decodes only
  changed fields.
- `web-ui/src/components/settings/GeneralSettings.tsx` — new "Subagent
  Delegation" subsection after Tool Discovery: 4 Toggles + 4 inputs (3 numeric, 1
  text timeout); numeric/timeout disabled when `delegation_enabled` is false.
- `web-ui/src/i18n/locales/{en,de,es,fr,ja,pt,zh}.json` — 13 new keys each under
  `settings.general`, fully translated per locale.

## Verification
- `go build ./...` → clean.
- `go test ./internal/api ./internal/config ./internal/tui/...` → all pass.
- `gofmt -l` on the two touched Go files → clean.
- `cd web-ui && npm run typecheck` → clean.
- All 7 locale JSON files validated with `JSON.parse`.

## Notes / remaining
- Hot-reload caveat (Phase 4): the `mesnada_await` tool is gated at agent-tool
  registration time on `Enabled && ResurrectIdleLoop`; toggling those live does
  not rebuild the running tool set (follows on next agent rebuild/restart). The
  live supervisor reads flags per-event, so Case A/B injection vs. resurrection
  honor changes immediately.
- Only Phase 6 (e2e + final feature doc) and optional Phase 7 (warm per-project
  instance reuse) remain.
