---
id: PANDO-US-0119
type: story
title: "F3 Guides: new guides section, guides/list with track filter, guides/single with video, chapters, written steps, shortcodes and seed content"
status: in_review
priority: high
parent: PANDO-EP-0020
author: mcp
labels: [docs, pando-docs, design, hugo]
created: 2026-10-02T12:15:37Z
updated: 2026-10-02T12:37:13Z
started: 2026-10-02T12:37:13Z
---

## Description

New visual guides section: each guide is a video plus written steps explaining how to work with one area or feature of Pando. Spec: `_design/design/Guides.dc.html` (index) and `_design/design/Guide.dc.html` / `GuideDark.dc.html` (single). Depends on F1.

### Tasks

1. Content section `content/en/guides/` and `content/es/guides/` with `_index.md` and front matter contract from HANDOFF: `title`, `description`, `track` (surface | roots | soil), `level` (beginner | intermediate | advanced), `weight`, `featured`, `video { provider, id, subtitles }`, `chapters [{t, title}]`. Archetype `archetypes/guides.md`.
2. `data/tracks.yaml`: kanji (表 根 土), en/es label and description, thumbnail colour (`#0F2A20`, `#163828`, `#3A2E12`), depth.
3. `layouts/guides/list.html`: hero with 道 watermark, featured guide block (the page with `featured: true`), track filter, groups by `track` ordered by depth, card grid. Filter is minimal JS on `data-track` + `aria-pressed`; without JS all groups show.
4. `layouts/guides/single.html`: 3-column grid (280px sidebar, content max 820px, 260px TOC), breadcrumb, title, lead, meta chips, video block, chapter bar, numbered steps, helpful block, prev/next, TOC with step numbers, "watch this guide in" language box linking to `.Translations`.
5. Video block: poster with grove SVG and play button; click-to-load embed (no third-party request before interaction) for `provider: youtube`; chapter buttons seek the player; with no `video` the block is omitted and the page reads as a written guide.
6. Steps: each `##` of the Markdown body is a step. Numbering (`01`, `02`, …) via CSS counters on a heading render hook scoped to the guides section, so authors write plain Markdown.
7. Shortcodes `layouts/_shortcodes/under-surface.html` (dark callout with 根, i18n title) and `shot.html` (image with `alt`, or dashed placeholder when `src` is empty).
8. Sidebar partial shared with F4 (`_partials/docs/sidebar.html`): groups Getting started, Guides, Features (表 / 根 / 土 with counts), Reference; active item style from the board.
9. Seed content en + es: "Your first session with Pando" complete (5 steps from the board), plus stub pages for the other eight guides on the index board marked `draft`-free but with `video` empty, so the index is populated.
10. Helpful block: Yes / No buttons record nothing server-side (static site); they link to a prefilled GitHub issue/discussion. "Edit on GitHub" from `params.editURL`.
11. i18n for all strings (levels, track labels, "Video + text", TOC, prev/next, callout title).

## Acceptance Criteria

- `guides/list` matches `Guides.dc.html` and `guides/single` matches `Guide.dc.html` / `GuideDark.dc.html` at 1440px, light and dark.
- Below 1100px the TOC hides; below 900px the sidebar hides and the content is single column.
- Track filter works by keyboard, updates `aria-pressed`, and degrades to all groups without JS.
- A guide with no video renders correctly.
- Switching language from a guide lands on the same guide in the other language.
- Adding a guide needs only one Markdown file per language.
- Validation: `hugo serve`, screenshots vs both boards, en and es, light and dark.

## Notes

Real videos and final copy for steps 3–5 are a HANDOFF pending item, outside this story. The `pando-doc` skill should be updated with the guide front matter contract (tracked in F6).
