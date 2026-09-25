---
created_at: 2026-09-25T08:29:27.357425962Z
updated_at: 2026-09-25T08:29:27.357425962Z
tags:
    - pando
    - webui
    - brand
    - feature
    - PANDO-US-0061
    - PANDO-EP-0011
---
# WebUI B3 — Section identities: Remembrances, Mesnada, Orchestrator

Story PANDO-US-0061 (epic PANDO-EP-0011, "Pando v1 brand identity"). Gives Remembrances (本)
and Mesnada (众) their own small identity in the WebUI: a section hero header with their brand
mark, plus the mark replacing the generic lucide icon in the places that represent them.

## What changed

New shared component:
- `web-ui/src/components/brand/SectionHero.tsx` — a restrained identity header for a feature
  section: `<BrandMark variant tile size={44} />` + Space Grotesk title (`.brand-display`) +
  one-line muted tagline, optional right-side `actions` slot. Hairline bottom border, tokens
  only, wraps on narrow widths (tested at 390px). Exported from `web-ui/src/components/brand/index.ts`.
- `web-ui/src/styles/sections.css` (new) — `.section-hero*` classes, imported directly by
  `SectionHero.tsx` (same self-import pattern as `BrandMark.tsx` + `brand.css`), so no shared
  `index.css` edit was needed.

Usages of `SectionHero`:
- `web-ui/src/components/settings/RemembrancesSettings.tsx` — replaced the plain
  `settings-page-header` with `<SectionHero variant="remembrances" .../>`.
- `web-ui/src/components/settings/MesnadaSettings.tsx` — same, `variant="mesnada"`.
- `web-ui/src/components/orchestrator/OrchestratorView.tsx` — added above the tab bar (applies
  to both the Tasks and CronJobs tabs), `variant="mesnada"`, title "Mesnada · Orchestrator"
  (the Orchestrator view *is* the Mesnada screen). `className="px-6 pt-5"` matches the existing
  `.view-header` padding rhythm since `.section-hero` itself carries no horizontal padding.

Icon swaps (BrandMark replacing a lucide icon, same call sites, same `size`):
- `web-ui/src/components/settings/SettingsView.tsx` — `renderNavButton`: the Remembrances and
  Mesnada category-nav rows now render `<BrandMark variant=... size={16} bold />` instead of
  the `Bookmark`/`Workflow` lucide icons (both imports kept — still referenced by
  `CATEGORY_KEYS`, unused-import-safe).
- `web-ui/src/components/layout/Sidebar.tsx` — `renderNavItem`: the `/orchestrator` nav item
  (full sidebar and the collapsed rail, `size={rail?18:16}`) renders the Mesnada mark instead
  of `Network`. Verified in both sidebar variants by screenshot.
- `web-ui/src/components/extensions/MemorySyncIndicator.tsx` — the status-bar "memory sync"
  indicator (rendered from `StatusBar.tsx` via `<MemorySyncIndicator />`) now shows
  `<BrandMark variant="remembrances" size={12} bold />` in the non-error state, keeping
  `TriangleAlert` for the failing state (that alert shape carries real meaning and was kept on
  purpose). **Scope note:** the actual icon glyph lives in `MemorySyncIndicator.tsx`
  (`components/extensions/`), not literally inside `StatusBar.tsx` — `StatusBar.tsx` only
  imports and renders `<MemorySyncIndicator />` unconditionally. This is the only
  remembrance-related indicator reachable from the status bar, so the edit was made there
  instead, kept to the two icon lines only (no logic touched).

## i18n

Added `hero.title`/`hero.tagline` keys, translated in all 7 locales
(`web-ui/src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json`):
- `settings.remembrances.hero.*` (new namespace)
- `settings.mesnada.hero.*` (new namespace)
- `orchestrator.hero.*` (added inside the existing `orchestrator` namespace, alongside
  `delegationMetrics`)

Proper nouns ("Mesnada") were kept untranslated in every locale, matching the existing
`settings.categories.mesnada` convention. "Remembrances" follows the existing convention too:
kept as-is except zh, which already translates it to 记忆库 in `settings.categories.remembrances`
— `settings.remembrances.hero.title` mirrors that (记忆库 for zh, "Remembrances" elsewhere).
Note: `RemembrancesSettings.tsx`/`MesnadaSettings.tsx` were not otherwise i18n'd before this
change (most of their body text is still hardcoded English) — only the new hero strings are
translated; the rest of those panels is unchanged.

## Design decisions

- Mark form: used `tile` (Bosque rounded-square app-icon form) at 44px rather than the bare
  mark, after comparing both by screenshot — the filled Bosque tile reads as a self-contained
  "section icon" next to the display-type title, closer to how macOS/Zeron settings show a
  colour tile per category, and doesn't need `currentColor` theming to look right against the
  page background.
- Kept `.section-hero` with zero horizontal padding so it composes cleanly in both contexts:
  settings (`settings-content-inner` already pads 32px) and the orchestrator view (padded via
  `className="px-6 pt-5"` at the call site to match `.view-header`).
- Orchestrator: the hero sits above the Tabs bar (shared by both Tasks/CronJobs tabs) rather
  than replacing the tab-specific "Mesnada Tasks" toolbar, which keeps its own create-task
  action. Two hairlines stack close together (hero's + tab bar's) but read fine in screenshots.

## Verification

- `cd web-ui && bun run typecheck` — 0 errors.
- `cd web-ui && bun run lint` — 0 errors, 4 pre-existing warnings in unrelated files
  (`KeyValueEditor.tsx`, `ModelCombobox.tsx`, `react-refresh/only-export-components`, not
  touched by this change).
- Visual check via headless Chrome + playwright-core against `bunx --bun vite --config
  vite.dev-https.config.ts --port 5182` (backend not running, so the app runs against a failed
  health check — `App.tsx` falls through the connecting/error splash after ~3s and renders
  anyway, confirmed by inspecting `src/App.tsx`'s `initApp`). Screenshots taken light + dark,
  desktop (1440×900) + mobile (390×844), sidebar expanded + collapsed rail, and the status-bar
  indicator (both non-error and failing state, mocked by intercepting
  `GET /api/v1/extensions/memory` since it only renders when the memory-sync capability is
  active). Screenshots saved to the shared scratchpad `shots/` dir, prefix `b3-`. All marks
  render at the correct variant (Remembrances: 2 ring nodes + gold root stroke; Mesnada: solid
  "lord" node + 2 ring "retinue" nodes) even at 16px in the settings nav and sidebar rail.

## Files touched

- `web-ui/src/components/brand/SectionHero.tsx` (new)
- `web-ui/src/components/brand/index.ts`
- `web-ui/src/styles/sections.css` (new)
- `web-ui/src/components/settings/RemembrancesSettings.tsx`
- `web-ui/src/components/settings/MesnadaSettings.tsx`
- `web-ui/src/components/settings/SettingsView.tsx`
- `web-ui/src/components/orchestrator/OrchestratorView.tsx`
- `web-ui/src/components/layout/Sidebar.tsx`
- `web-ui/src/components/extensions/MemorySyncIndicator.tsx` (scope note above)
- `web-ui/src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json`
