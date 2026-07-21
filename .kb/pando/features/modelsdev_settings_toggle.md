---
created_at: 2026-07-21T19:42:14.229766771Z
updated_at: 2026-07-21T19:42:14.229766771Z
tags:
    - pando
    - feature
    - settings
    - modelsdev
    - webui
    - tui
---
# Feature: Settings → General toggle for the models.dev catalog (2026-07-21)

Follow-up to [[modelsdev_model_metadata_catalog]]: the catalog was only configurable from
`.pando.toml`. It now has a General-settings toggle on both surfaces, default **on**.

## Live switch instead of a startup-only flag

`internal/llm/models/modelsdev/fetch.go` was reworked: the exported `Disabled` package var (read
once inside a `sync.Once`) became an `atomic.Bool` behind `SetDisabled(bool)` / `IsDisabled()`,
and the memoization moved from `sync.Once` to `mu sync.Mutex` + `attempted bool`. `Get` checks the
switch **outside** the memoized state, so:

- turning the toggle off takes effect immediately, even mid-refresh;
- turning it on later lets the next `Get` load the catalog without a restart or a `Reset()`;
- the ~3 MB download still happens at most once per process.

`Reset()` stays as a test-only hook (now mutex-guarded).

## Wiring

- `internal/config/config.go`: new `UpdateModelsDev(enabled bool)` — persists
  `[ModelsDev] Enabled` through `updateCfgFile`, rolls back the in-memory value on write failure,
  and applies `modelsdev.SetDisabled(!enabled)`. `config.Load` now also uses `SetDisabled`.
- `internal/api/handlers_settings.go`: `SettingsResponse.ModelsDevEnabled`
  (`models_dev_enabled`), the matching `*bool` in `SettingsUpdateRequest`, read from
  `cfg.ModelsDev.Enabled` and written via `config.UpdateModelsDev`.
- TUI `internal/tui/page/settings.go`: field **"Model Catalog (models.dev)"**, key
  `modelsDev.enabled`, in the General section between "LLM Prompt Cache" and "Auto-Approve Tool
  Changes"; save branch calls `config.UpdateModelsDev`.
- WebUI `web-ui/src/components/settings/GeneralSettings.tsx`: `Toggle` bound to
  `models_dev_enabled`; field added to `types/index.ts` and defaulted to `true` in
  `stores/settingsStore.ts`; i18n keys `settings.general.modelsDev` /
  `modelsDevDescription` added to all 7 locales (en, es, de, fr, pt, ja, zh).
- README documents the Settings path and notes the switch is live.

## Verification

- `go build ./...`; `go test ./internal/api ./internal/config ./internal/tui/...
  ./internal/llm/models/...` all pass.
- New `internal/api/handlers_settings_modelsdev_test.go`:
  `TestGetSettingsExposesModelsDevEnabled` and `TestPutSettingsTogglesModelsDevCatalog` (PUT false
  → `modelsdev.IsDisabled()` true and the response mirrors it; PUT true → re-enabled), using a
  temp working dir so the developer's real `~/.pando.toml` is untouched.
- `web-ui`: `npx tsc --noEmit` clean, `npm run build` OK, all locale JSON files re-parsed after
  editing (an apostrophe-quoting bug briefly broke `fr.json` and was repaired).
