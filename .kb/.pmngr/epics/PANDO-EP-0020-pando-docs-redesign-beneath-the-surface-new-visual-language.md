---
id: PANDO-EP-0020
type: epic
title: "pando-docs redesign \"Beneath the surface\": new visual language, home, video guides section and docs/blog layouts on the Hugo site"
status: backlog
priority: high
author: mcp
labels: [docs, pando-docs, design, hugo]
created: 2026-10-02T12:14:18Z
updated: 2026-10-02T12:14:18Z
---

## Description

Full visual redesign of the public documentation site (`../pando-docs`, Hugo + hextra v0.12.2, bilingual en/es). Target repo is `pando-docs`, not the Pando application.

Visual specification (exact markup, styles, copy, behaviour; NOT code to serve):

- `_design/HANDOFF.md` — implementation brief, tokens, proposed Hugo architecture.
- `_design/design/Main.dc.html` — home (light default, `theme=dark` variant via `MainDark`).
- `_design/design/Guides.dc.html` — guides index with track filter.
- `_design/design/Guide.dc.html` — single guide: video, chapters, written steps, sidebar, TOC (`GuideDark` variant).
- `_design/design/Moodboard.dc.html` — palette, typography, principles.

Concept: the site is a cross-section of the Pando grove. Strata 表 Surface (0 m), 根 Roots (−1 m: 木 Pando, 本 Remembrances, 众 Mesnada), 土 Soil (−3 m). Deeper = darker. Gold only on nodes and flows.

The boards do not cover blog, SDK, taxonomy term or 404 pages: these are adapted using the redesign as guide (US F5).

## Architecture decisions

1. **Keep hextra as a Hugo module, own every layout.** Content uses hextra shortcodes heavily (91 `callout`, 26 `card`, 6 `cards`, 6 `asciinema`, `youtube`, `icon`) plus its render hooks (code blocks with copy button, alerts, headings anchors) and FlexSearch index. Removing the module would force a content migration, which the brief forbids ("keep `content/`, migrate only what is needed"). So: project-level `layouts/baseof.html`, `_partials/head.html`, header, footer, `home.html`, `guides/*`, `docs/*`, `blog/*` override hextra; hextra CSS stays loaded only to serve shortcode/prose internals and is re-skinned through tokens.
2. **Hugo >= 0.146 layout tree** (`layouts/baseof.html`, `layouts/_partials`, `layouts/_shortcodes`), already used by this repo. HANDOFF paths `_default/baseof.html` and `partials/` map to these.
3. **Theme attribute bridge.** Design uses `[data-theme]`; hextra uses `html.dark` + `localStorage["color-theme"]`. The inline anti-flash script sets both `data-theme` and the `dark` class from the same stored value so hextra shortcodes follow the theme.
4. **One stylesheet `assets/css/pando.css`** (tokens + components) replaces `assets/css/custom.css`. Board inline styles become classes; no inline `style=""` soup in templates.
5. **Self-hosted fonts** under `static/fonts/` (Space Grotesk 400–700, JetBrains Mono 400/500, Shippori Mincho 500/700, Noto Serif SC 500 subset to 众). Google Fonts link removed.
6. **Search:** keep hextra FlexSearch behind the new ⌘K trigger in this epic. Pagefind migration is a follow-up decision (HANDOFF pending item 5).
7. **Subpath-safe URLs.** GitHub Pages serves under `/pando-docs/`; every asset and link goes through `relURL` / `relLangURL` / `.RelPermalink` (see KB `pando/fixes/pando-docs-subpath-asset-urls.md`).
8. **i18n:** all UI strings in `i18n/en.yaml` / `i18n/es.yaml`; language switch links to `.Translations` (falls back to the language home).

## Phases (stories)

- F1 Base and tokens: tokens, fonts, baseof, header, footer, theme.
- F2 Home: all sections of `Main.dc.html`, `data/features.yaml`.
- F3 Guides: section, list, single, shortcodes, seed content en/es.
- F4 Docs pages: 3-column docs layout, prose and shortcode re-skin.
- F5 Blog, SDK, taxonomy, 404 adapted to the new language.
- F6 QA and release: a11y, responsive, subpath build, CI, cleanup.

Order: F1 → F2 → F3 → F4 → F5 → F6. F2, F3, F4, F5 all depend on F1 only.

## Acceptance Criteria

- Home, guides list, guide single match the boards in light and dark at 1440px, and collapse correctly below 900px.
- Existing `content/` renders without content edits beyond the home `_index.md` files and the new `guides/` section.
- Every phase validated with `hugo serve` side by side with its board (screenshots light + dark, desktop + mobile).
- `hugo --gc --minify --baseURL https://host/pando-docs/` builds with no errors/warnings and no broken asset URLs.
- en and es parity on every new template and string.
- `prefers-reduced-motion` disables `.flow` and `.breathe`; gold text in light is always `#8F6310`.

## Notes

Out of scope (HANDOFF "pending before production", tracked as follow-ups): real screenshots replacing `[ SCREENSHOT · … ]` placeholders, real recorded videos, final guide titles/copy for steps 3–5, Pagefind.
CI pins Hugo 0.147.7 while local is 0.160.1: templates must work on both, or the pin is bumped in F6.
