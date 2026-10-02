---
created_at: 2026-10-02T12:15:57.760591611Z
updated_at: 2026-10-02T12:15:57.760591611Z
tags:
    - plan
    - pando-docs
    - hugo
    - design
    - docs
---
# Plan: pando-docs redesign "Beneath the surface" (PANDO-EP-0020)

**Date:** 2026-10-02
**Status:** Planned, nothing implemented yet
**Tracker:** gintrack epic PANDO-EP-0020, stories PANDO-US-0117 … PANDO-US-0122
**Target repo:** `/www/MCP/Pando/pando-docs` (Hugo + hextra v0.12.2, en/es), not the Pando app.

Builds on [[pando-docs-brand-v1]], [[pando-docs-brand-v1-styleguide]], [[brand-v1-identity]] and the subpath fix [[pando-docs-subpath-asset-urls]]. Site conventions: [[hugo-cms-for-pando-docs]].

## Specification

`pando-docs/_design/HANDOFF.md` plus `_design/design/{Main,MainDark,Guides,Guide,GuideDark,Moodboard}.dc.html`. The `.dc.html` files are design-canvas exports (inline styles, `{{hole}}`, `<sc-for>`, `<sc-if>`): exact visual spec, not servable code. Note: HANDOFF lives at `_design/HANDOFF.md` (not `_design/pando-docs-redesign/`).

Concept: the site is a cross-section of the grove. Strata 表 Surface (0 m, 12 features), 根 Roots (−1 m, 11: 木 Pando, 本 Remembrances, 众 Mesnada), 土 Soil (−3 m, 15). Deeper = darker; gold only on nodes and flows; slow `stroke-dashoffset` flow animation.

## Hard requirement from the user (2026-10-02)

The Pando, Remembrances and Mesnada icons/marks must be the **current official ones** from `pando/assets/pando-brand-v1/` (already synced in part to `pando-docs/static/images/brand/`). The marks drawn inside the `.dc.html` boards are an unofficial reinterpretation and must not be used. Board geometry is kept only for size and placement. Applies to header, footer, Roots pillar cards and anywhere else a mark appears.

## Architecture decisions

1. Keep hextra as a Hugo module but own every layout. Content depends on hextra shortcodes (91 `callout`, 26 `card`, 6 `cards`, 6 `asciinema`, `youtube`, `icon`), render hooks and FlexSearch; dropping it would force a content migration.
2. Layout tree is the Hugo >= 0.146 one already used here: `layouts/baseof.html`, `layouts/_partials`, `layouts/_shortcodes` (HANDOFF's `_default/` and `partials/` map to these).
3. Theme bridge: design uses `[data-theme]`, hextra uses `html.dark` + `localStorage["color-theme"]`. Inline anti-flash script sets both from one stored value; light default, OS preference when nothing stored.
4. Single `assets/css/pando.css` (tokens + components) replaces `assets/css/custom.css`; board inline styles become classes.
5. Fonts self-hosted in `static/fonts/` (Space Grotesk, JetBrains Mono, Shippori Mincho, Noto Serif SC subset for 众).
6. Search stays on hextra FlexSearch behind the new ⌘K trigger; Pagefind is a follow-up.
7. All URLs subpath-safe (`relURL`, `relLangURL`, `.RelPermalink`) because GitHub Pages serves under `/pando-docs/`.
8. All UI strings in `i18n/{en,es}.yaml`; language switch links to `.Translations`.

## Phases

| Story | Phase | Scope |
|---|---|---|
| PANDO-US-0117 | F1 Base and tokens | tokens, fonts, `baseof`, head, header (3 variants), footer, theme, official marks, JS basics |
| PANDO-US-0118 | F2 Home | `home.html` + `_partials/home/*`, grove SVG, `data/features.yaml` (38 features, slug to existing pages), install tabs |
| PANDO-US-0119 | F3 Guides | `content/*/guides`, `guides/list.html`, `guides/single.html`, `data/tracks.yaml`, `under-surface` and `shot` shortcodes, seed guides |
| PANDO-US-0120 | F4 Docs pages | `docs/single.html`, `docs/list.html`, prose, code blocks, hextra shortcode re-skin, mobile drawer |
| PANDO-US-0121 | F5 Blog / SDK / taxonomy / 404 | no board; designed by extension of guides list + guide article |
| PANDO-US-0122 | F6 QA and release | a11y, responsive, subpath build, CI Hugo pin (0.147.7 vs local 0.160.1), cleanup, `pando-doc` skill, KB |

Order F1 → F2 → F3 → F4 → F5 → F6; F2–F5 depend only on F1 (F4 reuses the sidebar/TOC partials built in F3).

Every phase is validated with `hugo serve` side by side with its board, light + dark, 1440px and 390px, en and es.

## Content policy

Keep `content/` as is. Only migrations: home `_index.md` en/es (hextra hero shortcodes removed, layout moves to templates) and the new `guides/` section.

## Out of scope / follow-ups

Real screenshots for `[ SCREENSHOT · … ]` placeholders, recorded videos, final copy of guide steps 3–5, definitive content of Go / Binaries / From source tabs, Pagefind.
