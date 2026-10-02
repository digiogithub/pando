---
id: PANDO-US-0120
type: story
title: "F4 Docs pages: 3-column docs layout (sidebar, prose, TOC), prose typography and hextra shortcode re-skin"
status: in_review
priority: medium
parent: PANDO-EP-0020
author: mcp
labels: [docs, pando-docs, design, hugo]
created: 2026-10-02T12:15:37Z
updated: 2026-10-02T12:37:13Z
started: 2026-10-02T12:37:13Z
---

## Description

Apply the guide layout to the existing documentation (`content/*/docs/**`, ~50 pages per language) without editing content. HANDOFF: "`docs/single.html` — same 3-column layout as the guide, without the video block". Depends on F1; reuses the sidebar and TOC partials from F3.

### Tasks

1. `layouts/docs/single.html` and `layouts/docs/list.html` overriding hextra: sidebar (shared partial, tree from the section pages ordered by `weight`, features grouped by stratum using `data/features.yaml`), article (breadcrumb, title, description lead, body, prev/next pager), right TOC from `.Fragments`.
2. Prose stylesheet (`.prose` in `pando.css`): headings scale, paragraphs 17px/1.7, links in `--acc`, lists, tables, blockquotes, `hr`, inline code, images, heading anchors.
3. Code blocks: `--code` background, radius 14, JetBrains Mono, hextra copy button restyled; Chroma classes mapped to a palette legible on `#0F2A20` / `#06130D`.
4. Re-skin hextra shortcodes through tokens: `callout` (91 uses), `card` / `cards`, `tabs`, `steps`, `details`, `badge`, `asciinema`, `youtube`, GitHub-style alerts.
5. `docs/features/_index.md` list page: features presented by stratum (表 / 根 / 土) from `data/features.yaml`, linking to each page.
6. `docs/brand.md`: verify the brand page (logos, `.brand-family`, `asset-url`) still renders correctly on the new tokens.
7. Mobile: sidebar as a drawer opened from the header menu button; TOC collapsed into a disclosure above the article.
8. Search results panel (FlexSearch) styled as the ⌘K overlay.

## Acceptance Criteria

- Every existing docs page builds and renders with no content change; spot-check at least: `getting-started`, `configuration/_index`, `features/desktop-app`, `features/llm-proxy`, `acp`, `mcp`, `brand`.
- Layout matches `Guide.dc.html` minus the video block, light and dark.
- Sidebar highlights the current page and keeps its scroll position; TOC highlights the current heading.
- No hextra default colours leak (no blue primary, no grey sidebars).
- WCAG AA contrast for body, links and code in both themes.
- Validation: `hugo serve`, compare against the Guide board; screenshot set of the spot-check pages in en and es.
