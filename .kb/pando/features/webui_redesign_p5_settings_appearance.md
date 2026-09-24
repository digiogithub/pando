---
created_at: 2026-09-24T21:43:37.71223457Z
updated_at: 2026-09-24T21:44:13.794998851Z
tags:
    - feature
    - webui
    - design
    - theme
    - settings
---

# WebUI redesign P5 — Settings shell + Appearance (PANDO-US-0055)

Part of [[webui_native_redesign_plan]] (epic PANDO-EP-0010). Builds on
[[webui_redesign_p1_p2_foundations]] (tokens v2, theme store, `ui/*`
primitives) and [[webui_redesign_p3_app_shell]] (`--bg-shell`, shell
conventions).

## What changed

### Native settings shell
`web-ui/src/components/settings/SettingsView.tsx` rewritten as a native
settings window (macOS System Settings / Zeron / Claude Desktop style):
- Left category nav on `--bg-shell`: 32px rows (`.settings-nav-item`),
  lucide icon per category, grouped ("Services" group keeps its faint
  uppercase label; an "Extensions" group appears when extension panels are
  registered), active state via `aria-current="true"` + `--accent-soft`
  fill.
- Right content column: `.settings-content-inner` caps at `max-width: 720px`
  and centers; each panel renders its own `.settings-page-header`
  (icon optional + title + description).
- Mobile (`<=768px`) master-detail preserved exactly: full-width category
  list first, picking one swaps to the panel with a `Button`-based "back to
  categories" control (was a raw `<button>` + FontAwesome `faBars`, now
  `ArrowLeft` from `@/components/ui/icons` — this was the only FontAwesome
  usage anywhere in `components/settings/*`).
- All ad hoc inline `style={{}}` layout removed in favour of a new
  `web-ui/src/styles/settings.css` (imported via one `@import` line added to
  `index.css`): `.settings-shell/-nav/-nav-item/-nav-group-label`,
  `.settings-content/-content-inner`, `.settings-page-header/-title/
  -description`, `.settings-banner` (+ `--warning`/`--danger`/`--success`
  variants), `.settings-actions`, `.settings-code-value`, `.settings-field`
  + `.settings-field-label` + `.settings-field-grid` (stacked label+control
  pattern for content that isn't a `SettingsRow`), `.settings-loading`,
  `.settings-empty-row`, `.settings-mobile-back`, plus the "UI scale" rule
  below.

### New "Appearance" category
`web-ui/src/components/settings/AppearanceSettings.tsx` (new), placed right
after "General" in the nav:
- **Mode**: `SegmentedControl` Light/Dark/System → `useTheme().setMode`.
- **Theme**: `THEME_FAMILIES` (pando/paper/slate/forest) rendered as the
  existing `.ui-theme-grid`/`.ui-theme-card` preview cards (window-mock:
  shell + 2 text lines + a raised line + an accent dot), values pulled live
  from `styles/themes.ts` `FAMILY_PALETTES[family][resolvedMode]` →
  `useTheme().setFamily`.
- **Accent**: round `.ui-swatch` row — first swatch is "Theme default"
  (`accent === null`), styled as a tri-tone `conic-gradient` using
  `var(--accent)`/`var(--danger)`/`var(--info)` (new
  `.settings-swatch-default` class, no hardcoded hex), then the 7
  `ACCENT_PRESETS` (gold/terracotta/violet/blue/green/rose/graphite) as
  solid swatches → `useTheme().setAccent`.
- **Interface font size**: new `small | default | large` control (13/14/15px
  base text), `SegmentedControl` → new tiny module
  `web-ui/src/components/settings/uiScale.ts` (`getUIScale`/`setUIScale`),
  which sets `data-ui-size` on `<html>` and persists to localStorage
  `pando_ui_size`. `styles/settings.css` maps the attribute to
  `--text-sm`/`--text-base` overrides:
  ```css
  :root[data-ui-size='small'] { --text-sm: 12px; --text-base: 13px; }
  :root[data-ui-size='large'] { --text-sm: 14px; --text-base: 15px; }
  ```
  Applied before first paint by a **targeted edit to `web-ui/index.html`**
  (alongside the existing theme boot IIFE — reads `pando_ui_size`, sets
  `data-ui-size` if `small`/`large`), mirrored by `uiScale.ts`'s own
  boot-time apply (fallback + keeps multiple tabs in sync via `storage`
  events, same shape as `hooks/useTheme.ts`). `uiScale.ts` lives under
  `components/settings/` rather than `hooks/` since `hooks/useTheme.ts` was
  outside this agent's area.
- Theme (`mode`/`family`) selection **moved out of `GeneralSettings.tsx`**
  into this panel. `ThemePicker.tsx` (`components/shared/`) is now unused
  (was only imported by `GeneralSettings.tsx`) and was **deleted**.
- Backend sync preserved exactly as `ThemePicker`'s old `onChange` did: any
  mode/family change computes the combined `"family-mode"` id and calls
  `useSettingsStore().updateField('theme', id)` (staging it dirty — the
  page's own Save/Reset buttons persist it, same as before), while
  `useTheme()`'s setter applies it instantly + persists to
  `localStorage('pando_theme')`. Accent and font size are local-only (never
  sent to the backend), matching the old accent behaviour.
- The guarded "adopt backend theme on a browser with no local choice yet"
  effect (`isWebThemeId(config.theme) && !hasStoredTheme()`) is kept as a
  silent copy in **both** `GeneralSettings.tsx` (no picker UI now, but still
  the default landing category so it still fires as soon as Settings opens)
  and `AppearanceSettings.tsx` (idempotent, so visiting Appearance directly
  is also covered).

### All 23 settings files migrated to `@/components/ui` primitives
Every panel under `components/settings/*` (`SettingsView` + 22 panels,
including the orphaned/unreferenced `ProvidersSettings.tsx` — superseded by
`ProviderAccountsSettings.tsx` but still migrated for consistency) now uses
`Button`, `IconButton`, `Input`, `Select`, `Switch`, `Checkbox`, `Textarea`,
`Badge`, `Card`, `Dialog`, `SettingsSection`/`SettingsRow`, `EmptyState`,
`Tooltip`, `SegmentedControl` instead of the old `components/shared/
FormInput.tsx` (`Toggle`/`TextInput`/`SelectInput`) and hundreds of inline
`style={{}}` objects / hardcoded hex / legacy `var(--primary)`/`var(--
sidebar-bg)`/`var(--error)`/etc. tokens. Zero inline styles remain except
genuinely dynamic per-theme swatch colours (Appearance) and one CPU/memory
grid `grid-template-columns` (converted to a Tailwind arbitrary-value class
instead). `MaskedInput`, `KeyValueEditor`, `TagListEditor`, `ModelCombobox`
(all in `components/shared/`) were kept exactly as-is (owned by another
agent) — only their call sites were adapted, notably `MaskedInput.onChange`
being `(value: string) => void`, not a change event.

A recurring local pattern (used in `AgentsSettings.tsx`, `InternalToolsSettings.tsx`,
`SkillsSettings.tsx`, `LSPSettings.tsx`, `MCPServersSettings.tsx`,
`ProviderAccountsSettings.tsx`, `ProvidersSettings.tsx`) for collapsible
list entries (agents/tools/providers/servers): `<Card padding="none">` with
a button header row (icon/title/description + optional `Badge` status +
`ChevronDown`/`ChevronUp`) instead of a rotating "▼" text arrow.

Files migrated directly by this agent: `SettingsView.tsx`,
`AppearanceSettings.tsx` (new), `uiScale.ts` (new), `GeneralSettings.tsx`,
`BashSettings.tsx`, `MCPGatewaySettings.tsx`, `APIServerSettings.tsx`,
`SnapshotsSettings.tsx`, `LuaSettings.tsx`, `WebUIAccessSettings.tsx`,
`TokenOptimizationSettings.tsx`, `MesnadaSettings.tsx`,
`ContainerRuntimeSettings.tsx`, `DesignSystemSettings.tsx`,
`SandboxSettings.tsx`, `AgentsSettings.tsx`, `EvaluatorSettings.tsx`,
`InternalToolsSettings.tsx`, `ProvidersSettings.tsx`. Delegated to 5
parallel fork sub-agents (same conventions, reviewed after landing):
`SkillsSettings.tsx`, `LSPSettings.tsx`, `MCPServersSettings.tsx`,
`ProviderAccountsSettings.tsx`, `RemembrancesSettings.tsx`.

### i18n
Added `settings.categories.appearance` to all 7 locales (en "Appearance",
es "Apariencia", fr "Apparence", de "Erscheinungsbild", pt "Aparência", ja
"外観", zh "外观") — the only string used without an inline `t(key, default)`
fallback (`CATEGORY_KEYS` calls `t(cat.labelKey)` uniformly for every
category). All other new Appearance/description strings use the existing
codebase convention `t('settings.appearance.xxx', 'English default')`
inline, without adding keys to the 7 locale JSON files (a scope trade-off
given the size of this task — flagged as a leftover below).

## Files touched
- `web-ui/src/components/settings/SettingsView.tsx` (rewritten)
- `web-ui/src/components/settings/AppearanceSettings.tsx` (new)
- `web-ui/src/components/settings/uiScale.ts` (new)
- `web-ui/src/components/settings/*.tsx` — all other 21 panels migrated
- `web-ui/src/components/shared/ThemePicker.tsx` (deleted, superseded by
  AppearanceSettings)
- `web-ui/src/styles/settings.css` (new)
- `web-ui/src/index.css` (+1 `@import './styles/settings.css'`)
- `web-ui/index.html` (targeted edit: `data-ui-size` boot script)
- `web-ui/src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json` (+`categories.appearance`)

## Verification
- `cd web-ui && bun run typecheck` — clean, whole repo, 0 errors.
- `bun run lint` — clean, whole repo, 0 errors (4 pre-existing warnings in
  `components/shared/{KeyValueEditor,ModelCombobox}.tsx`, not touched by
  this task, `react-refresh/only-export-components`).
- Structural review of all 5 forked files (grep for `style={{`,
  `fortawesome`, hex literals, old `FormInput` imports) — all zero.
- **Not verified**: live browser screenshots (light/dark, desktop/mobile).
  The dev server was started on port 5613 per the brief and responded
  200 OK to `curl`, but the shared `browser_navigate` MCP tool returned
  `context canceled` on every attempt (~10 retries over several minutes,
  including against an unrelated URL, over the whole session) — almost
  certainly resource contention from other WebUI-redesign agents (P3/P4/P6)
  running the same shared browser tool concurrently. Dev server was killed
  at the end regardless. Recommend a follow-up visual pass once the epic's
  parallel agents have finished.

## Leftovers / follow-ups for other areas or a later pass
- Appearance/new-string i18n keys use inline `t(key, default)` fallbacks
  rather than being added to all 7 locale JSON files (only the nav label
  `settings.categories.appearance` was added everywhere, since it's the one
  called without a default). Low risk (renders correctly in every locale as
  English), but a full-translation pass would be more consistent with the
  rest of the app.
- `MCPServersSettings.tsx`'s header (migrated by a fork) uses
  `.settings-page-title` directly in a custom flex row (title + "Add
  Server" button) rather than the standard `.settings-page-header` column
  layout, since that class is hard-coded `flex-direction: column` and can't
  hold an inline trailing action — same trade-off will recur for any future
  panel that wants a header-level action button; worth a `.settings-page-header--row`
  variant if this pattern repeats.
