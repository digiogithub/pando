---
created_at: 2026-06-28T16:00:08.260838215Z
updated_at: 2026-06-28T16:00:08.260838215Z
tags:
    - change
    - remembrances
    - settings
    - tui
    - webui
    - document-conversion
---
# Change: Expose KBConvertDocuments toggle in TUI + WebUI (follow-up #7)

Date: 2026-06-28

## What changed
Surfaced the existing `RemembrancesConfig.KBConvertDocuments` setting (default true)
as a user-facing toggle in both the TUI and the WebUI settings, completing the
deferred UI follow-up of the document-conversion feature
([[feature-document-conversion-markitdown]], `pando/features/document_conversion_markitdown.md`).

No API change was required: `internal/api/handlers_config.go` serializes the whole
`config.RemembrancesConfig` struct (json tag `kb_convert_documents` already present),
so GET/POST `/api/v1/config/services` (remembrances) already round-tripped the field.

## Files / symbols touched
- `internal/tui/page/settings.go`
  - Added a `settings.Field` toggle `remembrances.kb_convert_documents`
    ("KB Convert Documents") right after `remembrances.kb_auto_import` in the
    Remembrances field list.
  - Added the matching `case "remembrances.kb_convert_documents"` in the field-apply
    switch (parseBoolValue → `remCfg.KBConvertDocuments`).
- `web-ui/src/types/index.ts` — added `kb_convert_documents: boolean` to
  `RemembrancesConfig`.
- `web-ui/src/stores/servicesSettingsStore.ts` — added `kb_convert_documents: true`
  to `DEFAULT_REMEMBRANCES`.
- `web-ui/src/components/settings/RemembrancesSettings.tsx` — added a `Toggle`
  ("Convert Documents") after the Auto Import toggle, in the KB Filesystem Sync block.

## i18n note
The Remembrances settings panel is NOT internationalized in either surface: TUI
field labels (`internal/tui/page/settings.go`) and the WebUI `RemembrancesSettings.tsx`
labels (including the pre-existing kb_watch / kb_auto_import toggles) are hardcoded
English. There is no `settings.remembrances` subtree in `web-ui/src/i18n/locales/*.json`.
The new toggle therefore follows the panel's actual pattern (hardcoded English
labels) — there were no locale keys to add for this section. Full internationalization
of the whole Remembrances panel would be a separate, larger task.

## Verification
- `go build ./internal/tui/... ./internal/config/... ./internal/api/...` → OK.
- `gofmt -l internal/tui/page/settings.go` → clean.
- `web-ui`: `npx tsc --noEmit` → exit 0.
- `go test ./internal/config/... ./internal/api` → ok.
