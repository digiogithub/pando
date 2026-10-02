---
id: PANDO-US-0121
type: story
title: F5 Blog, SDK, taxonomy and 404 pages adapted to the new visual language (no board, redesign as guide)
status: backlog
priority: medium
parent: PANDO-EP-0020
author: mcp
labels: [docs, pando-docs, design, hugo]
created: 2026-10-02T12:15:37Z
updated: 2026-10-02T12:15:37Z
---

## Description

The boards do not include blog, SDK, tag pages or 404. Design them by extension of the system: section hero from `Guides.dc.html`, cards from the guides grid, article from `Guide.dc.html`. Depends on F1; article styles from F4.

### Tasks

1. `layouts/blog/list.html` (replaces the current hextra-based override): section hero with eyebrow, large title and a watermark kanji; latest post as a featured dark block (same structure as the featured guide); remaining posts as cards in a 3-column grid with date, tags as chips, summary; pagination (`params.blog.list.pagerSize`).
2. `layouts/blog/single.html`: centred article (max 820px) with breadcrumb, title, date, tags, prose from F4, right TOC, prev/next posts. No left docs sidebar.
3. `layouts/term.html` and `layouts/taxonomy.html`: same list design filtered by tag; tag cloud as chips.
4. SDK section (`content/*/sdk/`): `_index` as a card grid (TypeScript, Python, Java, .NET) matching the Soil "SDKs" strip; single pages on the docs layout from F4 with an SDK sidebar.
5. `layouts/404.html`: on-brand page (kanji, short copy en/es, links to home, docs, guides).
6. RSS and `llms.txt` outputs from hextra keep working.
7. i18n strings (read more, published on, tags, older/newer, 404 copy).

## Acceptance Criteria

- All 11 blog posts per language render with no content change, including the monthly roundups with `asciinema` / `youtube` embeds.
- Blog list, post, tag page, SDK index and 404 are visually consistent with home and guides: same tokens, radii, type scale, chips, header variant `section`.
- Light and dark, 1440px and 390px verified.
- Validation: `hugo serve`; since no board exists, a screenshot set is attached to the story for design review before closing.
