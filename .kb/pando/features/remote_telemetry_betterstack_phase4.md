---
created_at: 2026-09-10T20:40:26.238115625Z
updated_at: 2026-09-10T20:40:26.238115625Z
tags:
    - feature
    - telemetry
    - settings
    - webui
    - api
---
# Phase 4: REST API + WebUI for remote telemetry (Better Stack) — 2026-09-10

Implements [[remote_telemetry_betterstack_plan]] Phase 4. Builds on the already-complete
Phase 0/1 config+telemetry layer ([[remote_telemetry_betterstack_phase0_1]]):
`config.Get().Telemetry`, `config.UpdateTelemetry`, `config.RegenerateTelemetryID`,
`config.UpdateTelemetryMinLevel`, `config.TelemetryDebugIDDisplay()`, `telemetry.Available()`,
`telemetry.FormatDebugID`.

## What changed

### Backend — `internal/api/handlers_settings.go`
- `SettingsResponse` (routes at `internal/api/routes.go:51`) gains:
  `telemetry_enabled bool`, `telemetry_debug_id string` (display format "1234-5678-9012-3456",
  omitted/empty until first enable), `telemetry_min_level string`, `telemetry_available bool`.
- `SettingsUpdateRequest` gains pointer fields: `TelemetryEnabled *bool`,
  `TelemetryRegenerateID *bool`, `TelemetryMinLevel *string`.
- `buildSettingsResponse` fills the new fields from `cfg.Telemetry` + `telemetry.Available()`;
  added `telemetryMinLevelOrDefault` helper (defaults to `config.TelemetryLevelInfo`), matching
  the existing `*OrDefault` helper style in the file.
- `handlePutSettings`, placed right after the existing Debug block:
  - `TelemetryEnabled`: only calls `config.UpdateTelemetry` when the value actually differs from
    the current config (avoids an unnecessary global-file write on every save). Enabling while
    `!telemetry.Available()` returns `422 Unprocessable Entity` with a clear message and does
    **not** persist anything.
  - `TelemetryMinLevel`: calls `config.UpdateTelemetryMinLevel`; invalid values → `400`.
  - `TelemetryRegenerateID`: one-shot flag, applied last (after enable/min-level in the same
    request) so it always reflects the request's other changes; calls
    `config.RegenerateTelemetryID()`.
  - The response after PUT is rebuilt via `buildSettingsResponse()`, so it always carries the
    fresh debug id.
- New import: `github.com/digiogithub/pando/internal/telemetry`.

### Backend tests — `internal/api/handlers_settings_telemetry_test.go` (new)
- `withTelemetrySettings(t)`: isolates `$HOME`/`$XDG_CONFIG_HOME` to a temp dir (mirrors
  `internal/config.isolateGlobalConfig`) **and** uses the real `config.Load(dir, false)` path
  (like `loadTestConfig` in `handlers_container_test.go`), because `UpdateTelemetry` /
  `RegenerateTelemetryID` always persist through `updateGlobalCfgFile`, which needs a config
  actually loaded via viper to resolve a global path — `config.SetForTests` alone doesn't wire
  that up. This guarantees the tests never touch the real `~/.pando`.
- Covers: GET exposes the 4 fields (defaults: disabled, empty id, "info", unavailable without a
  token); PUT enable with `t.Setenv("PANDO_TELEMETRY_TOKEN", "test")` generates and returns a
  16-digit id in grouped display format; PUT enable without the token → `422`, config untouched;
  PUT regenerate changes the id and leaves `Enabled` alone; PUT min level updates/validates
  (rejects `"verbose"` with `400`, leaves the persisted level unchanged).
- `go test ./internal/api -count=1` — all pass (existing suite + 5 new tests).
- `go build ./internal/api/... ./internal/telemetry/... ./internal/config/...` — clean.
- Note: `go build ./...` currently fails in `internal/tui/page/settings.go` — that is Phase 5's
  in-progress work on a file this phase was explicitly told not to touch (concurrent agent), not
  a regression from this change. Confirmed by excluding `internal/tui` from the build target.

### pando-client — `web-ui/packages/pando-client/src`
- `types/index.ts` `SettingsConfig` (~:541): added `telemetry_enabled`, `telemetry_debug_id`,
  `telemetry_min_level`, `telemetry_available` (all mirror the response), plus an optional
  request-only `telemetry_regenerate_id?: boolean`.
- `stores/settingsStore.ts`: added the 4 fields to `DEFAULTS` (not `telemetry_regenerate_id` —
  it must never live in `config`/`original` state). Added a dedicated action
  `regenerateTelemetryId()` that PUTs a **minimal** body `{ telemetry_regenerate_id: true }`
  (not the full draft config) so it can never accidentally persist other unsaved field edits,
  and merges the server's response into `config`/`original` on success. The ordinary
  `saveSettings()` (full-config PUT) already round-trips the other 3 read/write fields fine —
  the two read-only ones (`telemetry_debug_id`, `telemetry_available`) are harmless in the PUT
  body since the backend's `SettingsUpdateRequest` has no matching JSON field for them.

### WebUI — `web-ui/src`
- **New** `components/shared/CopyButton.tsx`: extracted from the code-block copy button in
  `components/chat/MessageBubble.tsx` (was `~:553-599`). Props: `text: string | (() => string)`
  (a getter, not just a plain string, so the code-block use case can keep reading the live DOM
  text of a still-streaming `<pre>` — same trick the original inline component used via a ref),
  optional `label`, `title`, `className`, `size` ('sm' | 'md'), `style`. Uses the new
  `utils/clipboard.ts` helper instead of calling `navigator.clipboard.writeText` directly.
  `MessageBubble.tsx`'s `PreWithCopy` now renders `<CopyButton text={() => preRef.current?.textContent ?? ''} title="Copy code" style={{position:'absolute', ...}} />` — no visual change (verified via `bun run build`).
- **New** `utils/clipboard.ts`: `copyToClipboard(text): Promise<boolean>`. Tries
  `navigator.clipboard.writeText` first; on absence or rejection, falls back to a hidden
  `<textarea>` + `document.execCommand('copy')`. This matters for the Wails desktop webview and
  for a plain `http://` LAN address reached via the External Access toggle (non-secure context,
  where `navigator.clipboard` isn't exposed).
- `components/shared/FormInput.tsx` `Toggle`: added optional `disabled` and `hint` props (dims
  the row, blocks the click/keyboard handlers, shows a hint line under the description). Used for
  the telemetry toggle when `!telemetry_available`. No default-prop behavior change for existing
  callers.
- `components/settings/GeneralSettings.tsx`: new "Diagnostics / Remote Diagnostics" subsection
  right after the Debug toggle (own `<h3>` + divider, matching the existing "Subagent Delegation"
  subsection pattern): the telemetry `Toggle` (disabled + hint when unavailable), a monospace
  read-only debug id with `CopyButton` (size="md") + a "Regenerate" button calling
  `regenerateTelemetryId()` (shown whenever an id exists, independent of the enabled/available
  state — mirrors the backend, which allows regenerate unconditionally), and a `SelectInput` for
  `telemetry_min_level` shown only while enabled. Follows the existing `updateField` + Save-button
  draft pattern, so the id only appears once the server confirms it post-save.

### i18n
- Added `settings.general.telemetry*` keys (`telemetryTitle`, `telemetryEnabled`,
  `telemetryEnabledDescription`, `telemetryUnavailableHint`, `telemetryDebugId`,
  `telemetryRegenerateId`, `telemetryMinLevel`, `telemetryMinLevel{Debug,Info,Warn,Error}`) to
  all 7 locale files (`en`, `es`, `fr`, `de`, `pt`, `ja`, `zh`), right after `debugDescription`
  and before `delegation`, in every file — keeping key order consistent across locales. All 7
  files validated with `python3 -c "import json; json.load(open(...))"`.

## Verification
- `go build ./internal/api/...` clean; `go vet ./internal/api/...` clean; `gofmt -l` clean.
- `go test ./internal/api -count=1` → `ok` (full package, incl. the 5 new telemetry tests).
- `bun run typecheck` → clean.
- `bun run lint` → 0 errors (pre-existing 4 warnings in unrelated files, react-refresh rule).
- `bun run build` → succeeds, emits `web-ui/dist` (gitignored, not tracked by jj/git).
- Manual verification (browser/Desktop) deferred — out of scope for this backend+frontend-code
  pass; flagged for whoever does the end-to-end pass in Phase 6.

## Deviations from the plan text
- Chose `422 Unprocessable Entity` (plan said "400/422") for "enable while unavailable" — reads
  more precisely as a semantic/business-rule rejection than a malformed request.
- `CopyButton`'s `text` prop is typed `string | (() => string)` rather than plain `string`: the
  code-block extraction needed the lazy-read behavior to stay correct for streaming content
  without changing behavior; a plain-string call site (the debug id) just passes a string as
  before.

Related: [[remote_telemetry_betterstack_plan]], [[remote_telemetry_betterstack_phase0_1]]