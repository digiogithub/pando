---
id: PANDO-US-0122
type: story
title: "F6 QA and release: accessibility, responsive, subpath build, CI Hugo version, cleanup, skill and KB docs"
status: backlog
priority: medium
parent: PANDO-EP-0020
author: mcp
labels: [docs, pando-docs, design, hugo]
created: 2026-10-02T12:15:37Z
updated: 2026-10-02T12:15:37Z
---

## Description

Cross-cutting verification and release hardening after F1–F5.

### Tasks

1. Board comparison pass: full-page screenshots (Playwright + system Chrome) of home, guides list, guide single in light/dark at 1440px next to the rendered `.dc.html` boards; fix residual deltas.
2. Accessibility: keyboard walk of header, dropdown, tabs, filter, video; focus visible; `aria-*` on toggles; axe run with zero serious issues; contrast check of gold text in light (`#8F6310` only, `#C68A17` never as text).
3. Responsive: 390, 768, 900, 1100, 1440, 1920.
4. `prefers-reduced-motion` and no-JS checks (theme defaults to OS, filter shows all, video shows link).
5. Subpath build: `hugo --gc --minify --baseURL "https://madeindigio.github.io/pando-docs/"`; crawl `public/` for absolute `/…` URLs and broken links.
6. CI: `.github/workflows/pages.yaml` pins Hugo 0.147.7, local is 0.160.1. Build with the pinned version, or bump the pin and record why.
7. Cleanup: remove `assets/css/custom.css` leftovers and unused overrides; decide whether `_design/` stays in the repo and exclude it from the build either way.
8. Performance: font preload and subsetting, CSS size, no layout shift from fonts, lazy video.
9. Update the `pando-doc` skill (`.agents/skills/pando-doc/`) with: guide front matter contract, `under-surface` and `shot` shortcodes, `data/features.yaml` upkeep when a feature page is added.
10. KB: update `pando/skills/hugo-cms-for-pando-docs.md` and add the change summary for the epic.
11. Follow-up items filed for HANDOFF pendings: real screenshots, recorded videos, final guide copy, Pagefind.

## Acceptance Criteria

- Production build clean with the CI Hugo version, zero broken internal links in en and es.
- Checklists above recorded as a comment on this story with evidence.
- Skill and KB updated; follow-ups exist in the tracker.
