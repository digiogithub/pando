---
created_at: 2026-06-29T06:40:57.154069796Z
updated_at: 2026-06-29T06:40:57.154069796Z
tags:
    - change
    - rtk
    - output-filter
    - token-optimization
    - ui
    - i18n
    - phase5
---
# Change: RTK Output Filter — Phase 5 (UI toggle, i18n, tool-result metadata)

**Date:** 2026-06-29
**Author:** Claude (Opus 4.8)
**Phase:** 5 of the RTK output-token-reduction plan (`pando/plans/rtk_output_token_reduction_plan.md`).
Follows Phase 4 (`rtk_output_filter_phase4_structured_parsers.md`). Phase 3 (token analytics) was SKIPPED per user.

## What was implemented
Three deliverables for Phase 5:

1. **Tool-result metadata** — surface the matched filter/parser name and the char savings.
2. **TUI + WebUI settings toggle** for `Bash.OutputFilterDisabled` (exposed as an "enabled" toggle).
3. **7-locale i18n** for the WebUI toggle label + description.

## Files & symbols touched

### Backend — metadata
- `internal/llm/tools/bash.go`
  - `BashResponseMetadata` gained three `omitempty` fields: `OutputFilter string`
    (`output_filter`), `OutputFilterCharsBefore int` (`output_filter_chars_before`),
    `OutputFilterCharsAfter int` (`output_filter_chars_after`).
  - Both call sites (`Run` host path + `runWithACP` ACP terminal path) now capture the
    `outputFilterResult` and populate those three metadata fields.
- `internal/llm/tools/bash_outputfilter.go`
  - `applyOutputFilter` changed signature: now returns a new `outputFilterResult` struct
    `{Output, Name, Before, After}` instead of a bare string. When no compression is applied,
    `Name` is empty and `Before/After` stay 0 (so `omitempty` keeps them out of the JSON);
    when applied, `Before = len(raw)`, `After = len(filtered)`. Still fully fail-safe.

### Backend — config toggle (TUI + API)
- `internal/tui/page/settings.go`
  - Bash settings section now has an "Output Filter" `FieldToggle` (`bash.outputFilter`),
    value = `!bash.OutputFilterDisabled` (toggle shows *enabled*, config stores *disabled*).
  - `saveBash` handles `bash.outputFilter`: parses the bool and sets
    `OutputFilterDisabled = !enabled` (uses `parseBoolValue`, consistent with other toggles).
- `internal/api/handlers_settings.go`
  - `SettingsResponse.OutputFilterEnabled` (`output_filter_enabled`) = `!cfg.Bash.OutputFilterDisabled`.
  - `SettingsUpdateRequest.OutputFilterEnabled *bool`; apply block sets
    `bashCfg.OutputFilterDisabled = !*req.OutputFilterEnabled` via `config.UpdateBash`.
  - No engine reset needed: `applyOutputFilter` reads `cfg.Bash.OutputFilterDisabled` live per call.

### Frontend (WebUI)
- `web-ui/src/types/index.ts` — `SettingsConfig.output_filter_enabled: boolean`.
- `web-ui/src/stores/settingsStore.ts` — default `output_filter_enabled: true`
  (whole config is PUT on save, so the new field flows automatically).
- `web-ui/src/components/settings/GeneralSettings.tsx` — `<Toggle>` for
  `settings.general.outputFilter` / `outputFilterDescription`, placed with the other toggles.
- `web-ui/src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json` — added `outputFilter` +
  `outputFilterDescription` keys under `settings.general`.

## Design notes
- **Inverted semantics handled consistently**: config stores the negative
  `OutputFilterDisabled`; every UI surface (TUI toggle, API field, WebUI toggle) exposes the
  positive "enabled" so users aren't reasoning about a double negative.
- **No visible badge in TUI/WebUI tool rendering**: today no `BashResponseMetadata` field
  (`total_lines`, `truncated`, …) is rendered in either UI — the metadata travels with the tool
  response (consumed by ACP clients). Phase 5's "surface … in tool-result metadata" is satisfied
  by adding the fields to the response struct; adding a live badge would require plumbing the
  whole metadata channel through the SSE event stream — out of scope and inconsistent with the
  existing fields.

## Verification
- `go build ./...` OK; `go vet ./internal/llm/tools/... ./internal/api ./internal/tui/page/` clean.
- `go test ./internal/llm/tools/... ./internal/api ./internal/tui/page/` all pass
  (tools, outputfilter, api, tui/page).
- `npx tsc --noEmit` (web-ui) exit 0.
- All 7 locale JSON files validate (`JSON.parse`).

## Remaining
- Phase 6 (LOW): README/feature doc, `.pando/filters.toml` authoring guide, optional
  `pando filter test <file>`.
