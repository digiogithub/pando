---
id: PANDO-US-0059
type: story
title: "B1 Brand foundation: BrandMark component, brand tokens, brand-sync script"
status: done
parent: PANDO-EP-0011
milestone: PANDO-M-0003
author: mcp
labels: [brand, webui]
created: 2026-09-25T08:13:53Z
updated: 2026-09-25T08:16:31Z
closed: 2026-09-25T08:16:31Z
---

## Description
Build the shared foundation that the other stories depend on:
- `web-ui/src/components/brand/BrandMark.tsx`: inline SVG for the pando, remembrances and mesnada marks. Strokes use `currentColor` and nodes use `var(--brand-node)`.
- Add brand tokens to `tokens.css`.
- Add `scripts/brand-sync.sh` to copy the derived assets.

## Acceptance Criteria
- The marks render in light and dark.
- typecheck passes.
