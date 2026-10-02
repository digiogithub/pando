---
id: PANDO-US-0117
type: story
title: "F1 Base and tokens: design tokens, self-hosted fonts, baseof, header, footer, theme switch, official brand marks"
status: backlog
priority: high
parent: PANDO-EP-0020
author: mcp
labels: [docs, pando-docs, design, hugo]
created: 2026-10-02T12:15:37Z
updated: 2026-10-02T12:15:37Z
---

## Description

Foundation every other phase builds on. Spec: `_design/HANDOFF.md` (tokens, theme, a11y) and the `<helmet>` style block + `<header>` / `<footer>` of `_design/design/Main.dc.html`; compact header variants in `Guides.dc.html` (72px, no search) and `Guide.dc.html` (68px, full-bleed, `/ docs` suffix).

**Brand marks: use the official Pando v1 assets, not the board drawings.** The 木 / 本 / 众 SVGs inlined in the `.dc.html` files are an unofficial reinterpretation and must NOT be copied. Source of truth is `assets/pando-brand-v1/` in the `pando` repo (`pando-mark-{dark,light}.svg`, `pando-logo-*`, `pando-icon.svg`, `remembrances/*`, `mesnada/*`), already partly synced to `pando-docs/static/images/brand/`. Board geometry is kept only for size, position and spacing of the mark.

### Tasks

1. `assets/css/pando.css`: tokens for `:root,[data-theme="light"]` and `[data-theme="dark"]` exactly as in HANDOFF, plus `--shot`, `--code`, `--sel` found in the boards; radii scale (12 / 14–16 / 22–24 / 28–32 / 999); `.wrap` (1280px, 40px, 16px under 900px); utilities and components `.mono`, `.kanji`, `.nav`, `.chip`, `.dchip`, `.ibtn`, `.btn`, `.btn--ghost`, `.flow`, `.breathe`, `.hide-m`, `.hide-t`, `.stack-m`; `prefers-reduced-motion` guard. Retire `assets/css/custom.css` (keep only rules still needed by content, e.g. `.brand-family`, moved into `pando.css`).
2. Fonts self-hosted in `static/fonts/` with `@font-face` + `font-display: swap`, preload of Space Grotesk; Noto Serif SC subset limited to the glyphs used (众). Remove the Google Fonts links in `layouts/_partials/custom/head-end.html`.
3. `layouts/baseof.html`: `<html lang data-theme>`, skip link, header, `main` block, footer, scripts. `layouts/_partials/head.html` override: meta, opengraph, favicons, `pando.css`, hextra CSS kept for shortcode internals, inline anti-flash script (reads `localStorage["color-theme"]` in `try/catch`, falls back to `prefers-color-scheme`, light default; sets `data-theme` AND the `dark` class).
4. `layouts/_partials/header.html`: official mark + `pando` wordmark + `v1.x` pill, main nav (Docs, Guides, Features, SDKs, Blog) from `menus.main` with `aria-current`, ⌘K search trigger wired to hextra FlexSearch, language dropdown linking to `.Translations` with active marker, theme toggle (`aria-label`, sun/moon swap), GitHub button. Variants via a param: `home` (72px, `.wrap`), `section` (72px, segmented EN/ES), `docs` (68px, full-bleed, `/ docs`). Mobile: nav and search collapse into a menu button (not on the boards; designed here, 44px targets).
5. `layouts/_partials/footer.html`: official mark + 木本众, tagline, four link columns (Docs, Learn, Ecosystem, Project) driven by `menus.footer_*` in `hugo.toml`, bottom bar.
6. `layouts/_partials/svg/` wrappers that inline the official mark files so they inherit `currentColor` / theme where the asset allows, otherwise light/dark pair swapped by `data-theme`.
7. `hugo.toml`: add `guides` to `menus.main`, footer menus, `params.version = "1.x"`.
8. `i18n/en.yaml`, `i18n/es.yaml`: header, footer, theme and language strings.
9. Small JS (`assets/js/pando.js`, deferred): theme toggle, language dropdown (click outside + Escape), ⌘K / Ctrl+K, copy-to-clipboard.

## Acceptance Criteria

- No flash of wrong theme on hard reload in either mode; toggle persists; with no stored value the OS preference wins.
- Header and footer match `Main.dc.html` at 1440px in light and dark, except the mark, which is the official asset.
- No request to `fonts.googleapis.com` / `fonts.gstatic.com`.
- hextra shortcodes (`callout`, `cards`, code blocks) still follow light/dark.
- Language switch lands on the translated page, not the home.
- All interactive controls are real `<a>`/`<button>`, >= 44px, keyboard reachable with visible focus.
- Validation: `hugo serve`, compare header/footer against the Main board, light + dark, 1440px and 390px.

## Notes

At the end of this phase existing pages (home, docs, blog) still use hextra bodies inside the new chrome; they may look transitional until F2–F5.
