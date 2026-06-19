---
created_at: 2026-06-19T09:47:09.044308994Z
updated_at: 2026-06-19T09:47:09.044308994Z
tags:
    - fix
    - webui
    - i18n
    - translations
    - settings
---
# Fix: missing translation keys in web-UI General settings (raw `settings.general.*` shown)

Date: 2026-06-19

## Symptom
The web-UI Settings → General section rendered raw i18n keys such as
`settings.general.autoCompact`, `settings.general.debug`, etc., because recently
added toggles/fields referenced keys that did not exist in any locale file (so
not even the English fallback resolved them).

## Root cause
`GeneralSettings.tsx` uses `t('settings.general.*')` for fields that were added
after the locale files were last updated. Seven keys were absent from ALL locales:
`homeDirectory`, `autoCompact`, `autoCompactDescription`, `showHiddenFiles`,
`showHiddenFilesDescription`, `debug`, `debugDescription`. Additionally, the
`toolDiscovery*` block (10 keys) existed only in `en.json` and `es.json`, so
`de/fr/pt/ja/zh` fell back to English instead of being localized.

## Changes (web-ui/src/i18n/locales/*.json)
- Added the 7 missing `settings.general.*` keys to all 7 locales
  (en, es, de, fr, pt, ja, zh) with proper translations.
- Backfilled the 10 `settings.general.toolDiscovery*` keys into
  `de, fr, pt, ja, zh` so every locale now has full parity with `en`.

## Verification
- Scanned every `src/components/settings/*.tsx` for `t('...')` keys → 0 missing
  from `en.json` after the change.
- Parity check: all of es/de/fr/pt/ja/zh now match `en.json`'s `settings.general`
  key set exactly (no missing, no extras).
- `npx tsc --noEmit` in web-ui — clean.
- Note: web-ui assets must be rebuilt (`npm run build`) for changes to ship.

## Method (reusable)
A small Python script extracts `t('<key>')` literals from the settings
components and diffs them against `en.json` (nested lookup) to surface missing
keys; another inserts new keys into each locale's ordered `settings.general`
dict, writing back with `indent=2, ensure_ascii=False`.
