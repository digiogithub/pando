---
id: PANDO-US-0118
type: story
title: "F2 Home: hero grove, depth index, Surface / Roots / Soil strata, guides teaser, install block, data/features.yaml"
status: backlog
priority: high
parent: PANDO-EP-0020
author: mcp
labels: [docs, pando-docs, design, hugo]
created: 2026-10-02T12:15:37Z
updated: 2026-10-02T12:15:37Z
---

## Description

Rebuild the home page from `_design/design/Main.dc.html` (dark reference: `MainDark.dc.html`). Depends on F1.

### Tasks

1. `layouts/home.html` composing partials in `layouts/_partials/home/`: `hero.html`, `depth-index.html`, `surface.html`, `ground-line.html`, `roots.html`, `soil.html`, `guides-teaser.html`, `install.html`.
2. `layouts/_partials/svg/grove.html`: hero grove + root network SVG verbatim from the board (`role="img"`, translated `aria-label`, `.flow` / `.breathe` classes, SVG text labels from i18n). Decorative SVGs (`ground-line`, roots background, flow line) `aria-hidden`.
3. `data/features.yaml`: the 38 features with `stratum` (surface | roots | soil), `group` (pando | remembrances | mesnada | models | protocols | hands | extend | trust | operations), `slug` to the existing page under `docs/features/`, and en/es labels. Map every entry to a real existing page (e.g. Document Conversion → `markitdown`, Local LLM Proxy → `llm-proxy`, Thinking & Reasoning Effort → `reasoning-modes`); the build must fail on a slug with no page. Chips, counts in the depth index (12 / 11 / 15) and docs sidebar counts are all derived from this file. Non-link chips (OpenAI, MCP, Lua, …) live in the same file with no slug.
4. Roots pillar cards use the **official** Pando / Remembrances / Mesnada marks (see F1), not the board drawings.
5. Install block: tabs Go / Binaries / From source as an accessible tablist (arrow keys, `aria-selected`), content taken from `docs/getting-started`; copy button on the hero command.
6. Guides teaser lists the three featured guides from the `guides` section (falls back to static cards until F3 lands).
7. `content/en/_index.md` and `content/es/_index.md` reduced to front matter (title, description, hero strings that are editorial); all hextra hero/feature-card shortcodes removed from them. This is the only content migration in this phase.
8. Copy in `i18n/*.yaml`; Spanish copy written for every home string (board only has English for the home).
9. Responsive: `.stack-m` collapse, `.h1` 60px / `.h2` 40px under 900px, vertical 万物流転 hidden on mobile.

## Acceptance Criteria

- Side by side with `Main.dc.html` at 1440px: section order, spacing, type scale and colours match in light and dark; only the brand marks differ (official assets).
- Feature counts are computed, not hardcoded; every linked chip resolves to an existing page in en and es.
- Animations stop under `prefers-reduced-motion`.
- No horizontal scroll at 390px.
- `[ SCREENSHOT · PANDO DESKTOP 1.x ]` placeholder kept as a styled placeholder component accepting a real image later.
- Validation: `hugo serve`, full-page screenshots vs `Main` and `MainDark` boards, en and es.
