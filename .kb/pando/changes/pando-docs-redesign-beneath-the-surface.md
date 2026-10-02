---
created_at: 2026-10-02T12:36:57.87613678Z
updated_at: 2026-10-02T12:36:57.87613678Z
tags:
    - changes
    - pando-docs
    - hugo
    - hextra
    - design
    - docs
---
# pando-docs redesign "Beneath the surface": implementation (2026-10-02)

Implements epic PANDO-EP-0020 (stories PANDO-US-0117 … 0122) in `/www/MCP/Pando/pando-docs`. Plan: [[pando-docs-redesign-beneath-the-surface]]. Builds on [[pando-docs-brand-v1]] and respects [[pando-docs-subpath-asset-urls]]. Site conventions: [[hugo-cms-for-pando-docs]].

Status: implemented, in review, uncommitted (jj working copy). Not pushed.

## Motivation

New visual language for the public docs site and a new video-guides section, specified by `pando-docs/_design/HANDOFF.md` and the `_design/design/*.dc.html` boards (spec only, never served).

## User decisions

- Keep hextra as a Hugo module; never edit `_vendor/`; every change is an override in the project.
- Pando / Remembrances / Mesnada marks are the official ones from `pando/assets/pando-brand-v1/`. The marks drawn in the boards are an unofficial reinterpretation and are not used; board geometry only gives size and placement.

## What changed

**F1 base and tokens**
- `assets/css/pando/00-tokens.css` (tokens, `@font-face`), `10-base.css` (utilities, chips, buttons, segmented, marks, motion), `20-chrome.css` (header, search, language, drawer, footer). Bundled in file-name order to `css/pando.css` by `layouts/_partials/custom/head-end.html`. `assets/css/custom.css` reduced to hextra primary-hue variables.
- Fonts self-hosted in `static/fonts/` (Space Grotesk, JetBrains Mono latin; Shippori Mincho and Noto Serif SC as glyph subsets generated with the Google Fonts `text=` parameter). No request to Google Fonts.
- `layouts/baseof.html`, `_partials/header.html` (variants home / section / docs), `_partials/footer.html`, `_partials/search.html` (same FlexSearch hooks, new look), `_partials/svg/mark-{pando,remembrances,mesnada}.html` (official geometry, token colours).
- hextra JS overrides: `assets/js/head/theme.js` (sets `html.dark` and `data-theme`, storage in try/catch, OS preference when nothing stored), `assets/js/core/theme.js` (single toggle), `assets/js/core/menu.js` (mobile drawer, language dropdown). New `assets/js/core/pando.js` (copy, tabs, guides filter, click-to-load video).
- `hugo.toml`: `menus.main` (docs, guides, features, sdk, blog), `menus.footer_*`, `params.version`, `params.github`, `params.editURL`, `params.feedback`.

**F2 home**: `layouts/home.html` + `_partials/home/{hero,depth-index,surface,roots,soil,guides-teaser,install}.html`, `_partials/svg/grove.html`, `assets/css/pando/30-home.css`. `data/features.yaml` holds the features by stratum; `_partials/features/{chip,count}.html` render chips and counts and fail the build on a slug with no page (a page missing in a language falls back to the default-language page). `content/{en,es}/_index.md` reduced to front matter.

**F3 guides**: `content/{en,es}/guides/` (one complete guide `first-session`, eight `planned: true` outlines), `data/tracks.yaml`, `archetypes/guides.md`, `layouts/guides/{list,single}.html`, `_partials/guides/thumb.html`, `_partials/svg/grove-poster.html`, shortcodes `under-surface` and `shot`, `assets/css/pando/45-guides.css`. Steps are numbered by CSS counters on `h2`.

**F4 docs**: `_partials/docs/{shell,sidebar,toc,pager,crumbs}.html`; `layouts/single.html`, `list.html`, `docs/single.html`, `docs/list.html` all render the shell. Sidebar groups: Getting started, Guides, Features (three `<details>` by stratum from `data/features.yaml`), Reference. `assets/css/pando/40-docs.css`, `50-prose.css` (prose, code blocks, Chroma palette, tables, hextra callout/cards/tabs re-skin, mermaid). Features index shows the strata before its prose.

**F5 blog and others** (no board, built from the guides index components): `layouts/blog/{list,single}.html`, `term.html`, `taxonomy.html`, `404.html`, `_partials/blog/{card,hero}.html`, `assets/css/pando/60-blog.css`.

**F6**: CI Hugo pin 0.147.7 → 0.160.1 in `.github/workflows/pages.yaml` (templates were only verified on 0.160.1, the local version). `pando-docs/AGENTS.md` gained a "Design system" section; `pando-doc` skill updated with features.yaml upkeep and the guide contract.

## Deviations from the boards

- Brand marks: official assets (user decision).
- Surface count is 13, not 12: `project-workspaces` was added after the brief; counts are computed.
- Main nav and search are shown on every header variant (the guide boards omit them); mobile menu drawer is new (boards have no mobile nav).
- Spanish hero headline is set at 92px instead of 112px so "Muchos agentes." fits its column.
- Video block with empty `video.id` shows a "video coming soon" poster; chapters link to the written steps.
- "Helpful? Yes" only acknowledges locally (static site); "No" opens a prefilled GitHub issue.

## Gotchas

- `hugo serve` does not pick up newly created layout directories; restart it.
- hextra `flexsearch.js` selects the search box by `clientHeight > 0` and throws when none qualifies, so the closed mobile drawer is `visibility: hidden`, not `display: none`.
- hextra `core/menu.js` dereferences `.hextra-hamburger-menu` without a guard; it is replaced by our `menu.js`.
- Chroma: `pre.chroma` must keep its background; only inner spans are reset.
- `Pages.Next` returns the previous item of the collection (guide pager uses Next as "previous").

## Verification

- `hugo --gc --minify --baseURL https://madeindigio.github.io/pando-docs/`: builds, 0 broken internal links, 0 root-absolute URLs, no Google Fonts reference (script crawl of the output).
- Headless Chrome screenshots compared with the boards: home (en light, es dark, 390px), guides index, guide single (light/dark, 390px), docs pages (light/dark), blog index and post.
- Playwright (playwright-core + system Chrome): theme toggle and persistence, language dropdown and Escape, install tabs, FlexSearch results, track filter, language switch landing on the translated guide, mobile drawer, no horizontal scroll at 390px on seven pages, no page errors.
- Not done: axe accessibility run, build on the old CI Hugo version, tablet widths, real video playback (no video id yet).

## Open items

Real screenshots, recorded videos, content for the eight planned guides, seven feature pages missing in Spanish (caveman-mode, config-discovery, learning-mode, lsp-auto-activation, mcp-authentication, pando-setup-tool, superpowers-mode), Pagefind, Hugo deprecation warnings (`site.Data`, `site.Languages`, `site.Sites`, also emitted by hextra).
