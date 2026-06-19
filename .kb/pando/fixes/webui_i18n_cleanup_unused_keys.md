---
created_at: 2026-06-19T09:56:13.007406673Z
updated_at: 2026-06-19T09:56:13.007406673Z
tags:
    - fix
    - webui
    - i18n
    - translations
    - cleanup
---
# Cleanup: remove unused web-UI i18n keys + fix remaining parity gaps

Date: 2026-06-19 (follow-up to webui_settings_general_missing_translations)

## Goal
Remove translation keys no longer referenced by any component, and leave the 7
locale files fully consistent.

## Method
Python analysis over `web-ui/src`:
- Used keys = literal `t('<key>')` occurrences (regex `(?<![\w.])t\(['"]...`) plus
  dynamic keys resolved at runtime (`settings.categories.*` via `cat.labelKey` in
  SettingsView.tsx — `CATEGORY_KEYS[].labelKey`).
- Unused = flattened `en.json` keys not in the used set; each candidate
  cross-checked with a raw `grep` (0 references) before deletion.

## Changes (web-ui/src/i18n/locales/*.json)
- **Removed 44 unused keys** from all 7 locales (then pruned empty parent
  objects). Notable groups:
  - entire `settings.agents.*` subtree (17 keys) — AgentsSettings.tsx is not
    internationalized; it uses hardcoded English strings.
  - `chat.*` (8), `models.*` (7), `common.{cancel,error,loading,noResults}`,
    `nav.snapshots`, `settings.general.{autoSave*,customInstructions*,
    markdownPreview*,toolDiscoveryMaxDirectToolsDescription,
    toolDiscoverySearchLimitDescription}`.
- **Removed 1 orphan** `settings.categories.providerAccounts` (no component uses
  it; the category id is `providers`).
- **Backfilled used-but-missing keys** surfaced by the audit (had inline English
  defaults or only existed in en/es):
  - `settings.categories.containerRuntime` → es/de/fr/pt/ja/zh
  - `nav.instances` → de/fr/pt/ja/zh
  - `common.estimatedTokens`, `common.toggleAutoApprove`, `header.simpleChat`
    (used via `t(key, 'default')`) → added to all 7 locales for real localization.

## Result / Verification
- 0 component-used keys missing from `en.json`.
- 0 unused keys remaining in `en.json`.
- es/de/fr/pt/ja/zh now have EXACT key parity with `en.json` (no missing, no extras).
- `npx tsc --noEmit` clean. Net diff: +148 / -354 lines across the 7 files.
- Note: web-ui must be rebuilt (`npm run build`) to ship.
