---
created_at: 2026-07-14T15:08:37.903263842Z
updated_at: 2026-07-14T15:08:37.903263842Z
tags:
    - change
    - caveman
    - settings
    - tui
    - webui
    - api
    - token-optimization
---
# Caveman — Phases 5 & 6: settings UI (TUI + Web UI/API)

Part of [[caveman-persistent-output-brevity-mode]]. Follows
[[caveman_phase4_slash_commands]]. Phases 1-6 are now complete; only Phase 7
(documentation + final quality gates) remains.

## What was changed

Exposed the **global default** for caveman output brevity in both settings
surfaces. It is a default only: a session that ran `/caveman` or
`/caveman-finish` keeps its own level (the session override always wins, per
`cavemanModeForContext`).

### Shared strings (`internal/caveman/caveman.go`)
- `SettingDescription` — the exact user-facing help text from the plan,
  including the output-only caveat ("Output savings vary; input and reasoning
  tokens are not reduced").
- `SettingOptions()` — `["off","lite","full","ultra","wenyan"]`, so TUI and Web
  UI cannot drift apart on the option list.

### Phase 5 — TUI (`internal/tui/page/settings.go`)
- New `caveman.defaultMode` `FieldSelect` added **inside the existing "Token
  Optimization" section** (the plan allows "beside token/output optimization
  settings" and forbids a duplicate general setting). Value renders the config's
  empty string as `off`; `Hint` carries `caveman.SettingDescription`.
- `persistSetting` routes the `caveman.` prefix to the new `saveCaveman`, which
  validates with `caveman.ParseMode` and calls `config.UpdateCaveman`. An
  unsupported level returns an error and leaves the config untouched — a typo
  must never silently disable or change the mode.

### Phase 6 — API (`internal/api/handlers_settings.go`)
- `caveman_default_mode` added to `SettingsResponse`, `SettingsUpdateRequest`
  and `buildSettingsResponse` (reads `cfg.CavemanDefaultMode()`).
- PUT validates the value and calls `config.UpdateCaveman`; unknown values are a
  400 that preserves the previous state. **The empty string is accepted as off**
  even though `ParseMode("")` is deliberately invalid (a bare `/caveman` means
  `full`, not `off`), because `""` is how "no default" is stored and how the Web
  UI round-trips an off configuration.

### Phase 6 — Web UI
- `caveman_default_mode: string` in `web-ui/src/types/index.ts` and
  `DEFAULTS` (`''`) in `web-ui/src/stores/settingsStore.ts`.
- Select control in `GeneralSettings.tsx` (next to the RTK output-filter
  toggle). It lives in `GeneralSettings` rather than `TokenOptimizationSettings`
  because that component is backed by a **different store** with its own save
  button and endpoint; `caveman_default_mode` belongs to `/api/v1/settings`, so
  putting it there would have placed a field under a Save button that does not
  save it. The UI maps `off` ↔ `''`.
- i18n keys `caveman`, `cavemanOff`, `cavemanDescription` added to all 7 locales
  (en, es, de, fr, pt, ja, zh). Level names stay untranslated: they double as the
  `/caveman` arguments.
- Config-change events already refresh the store, so a change made in the TUI
  shows up in the Web UI without extra wiring.

## Files touched
`internal/caveman/caveman.go`, `internal/tui/page/settings.go`,
`internal/api/handlers_settings.go`, `web-ui/src/types/index.ts`,
`web-ui/src/stores/settingsStore.ts`,
`web-ui/src/components/settings/GeneralSettings.tsx`,
`web-ui/src/i18n/locales/{en,es,de,fr,pt,ja,zh}.json`.
New tests: `internal/api/handlers_settings_caveman_test.go`,
`internal/tui/page/settings_caveman_test.go`.

## Verification
- `go build ./...` clean.
- `go test ./internal/api ./internal/tui/... ./internal/caveman ./internal/config
  ./internal/llm/agent ./internal/mesnada/acp ./internal/commands` — all ok.
- `go test -race ./internal/api ./internal/caveman ./internal/config` — ok.
- `npx tsc --noEmit` and `eslint` on the touched Web UI files — clean.
- New tests cover: GET exposes the level; PUT persists it to TOML; `off`/`""`
  clear it; an unknown value is a 400 that leaves the previous level intact; a
  PUT without the field does not touch it; the TUI field is built with the right
  type/options/value (including `off` for an empty config) and its hint carries
  the output-only caveat; `saveCaveman` persists, clears, rejects unknown levels
  and is reachable through `persistSetting`.
- Both API and TUI tests isolate the config in a temp dir with its own
  `.pando.toml` (`ResolveConfigFilePath` prefers the local file), so they never
  read or write the developer's real `~/.pando.toml`.

## Note
`gofmt -l` flags `internal/api/handlers_settings.go` and
`internal/config/config.go`, but both were already misaligned at HEAD; not
reformatted to keep the diff readable.
