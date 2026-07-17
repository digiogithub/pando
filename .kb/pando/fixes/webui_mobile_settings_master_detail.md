---
created_at: 2026-07-17T13:10:45.957800171Z
updated_at: 2026-07-17T13:10:45.957800171Z
tags:
    - fix
    - webui
    - responsive
    - settings
---
# Fix: settings view unusable on mobile — category list turned into master/detail (2026-07-17)

## Symptom

On mobile widths the WebUI settings view (`/settings`, `SettingsView`) split the screen
between the 180px category sidebar and the section content, leaving too little width to
actually operate any settings section.

Same responsive round as [[fix_webui_mobile_chat_info_sidebar]] /
`pando/fixes/webui_mobile_chat_info_sidebar_layout.md`.

## Behaviour

- **Desktop (>768px)**: unchanged — 180px `<nav>` on the left, section on the right.
- **Mobile (<=768px)**: master/detail. The category list is the landing screen and takes
  the full width; picking a category hides the list and gives the section the full width,
  with a `Categories` button (bars icon) at the top of the section to bring the list back.
  The previously chosen category stays highlighted when the list returns.

## Changes

`web-ui/src/components/settings/SettingsView.tsx`:

- new `MOBILE_QUERY = '(max-width: 768px)'` const and an `isMobile` state fed by
  `window.matchMedia(...).addEventListener('change', ...)`, so rotating/resizing switches
  layouts live.
- new `menuOpen` state (starts `true`, i.e. the list is the mobile landing screen) plus
  derived `showMenu = !isMobile || menuOpen` and `showContent = !isMobile || !menuOpen`,
  which gate the `<nav>` and the content `<div>` respectively.
- `selectCategory(id)` replaces the direct `setActiveCategory` in both category `map`s
  (plain and `services` group): it sets the category and closes the menu. On desktop
  `menuOpen` is simply ignored, so nothing changes there.
- `<nav>` width is `isMobile ? '100%' : 180`, and its `borderRight` is dropped on mobile.
- content `<div>` got `minWidth: 0`, mobile padding `1rem` (vs `2rem`), and renders the
  back control (`backButtonStyle`, `faBars` + `t('settings.backToCategories')`) only when
  `isMobile`.

New i18n key `settings.backToCategories` added to all 7 locales in
`web-ui/src/i18n/locales/`: en `Categories`, es `Categorías`, fr `Catégories`,
de `Kategorien`, pt `Categorias`, ja `カテゴリ`, zh `分类`. Locales were edited with a
`json.load(object_pairs_hook=OrderedDict)` + `json.dump(indent=2, ensure_ascii=False)`
script, which produced a clean 1-line diff per file (verified via `git diff --stat`).

## Verification

- `npx tsc --noEmit -p tsconfig.app.json` — clean.
- Live browser checks at a 400px viewport on `http://localhost:5173/settings`:
  - landing: settings `<nav>` 400px wide with 18 buttons, no section rendered.
  - after clicking `Bash`: the nav is gone, content is 400px wide, first button in
    `.main-content` reads `Categories`, section text is `Bash Settings…`.
  - after clicking `Categories`: nav is back at 400px and `Bash` is still the highlighted
    (font-weight 600) entry.
