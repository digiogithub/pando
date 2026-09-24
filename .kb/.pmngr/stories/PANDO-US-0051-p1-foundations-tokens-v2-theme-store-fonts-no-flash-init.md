---
id: PANDO-US-0051
type: story
title: "P1 Foundations: tokens v2, theme store, fonts, no-flash init"
status: done
priority: high
parent: PANDO-EP-0010
milestone: PANDO-M-0003
author: mcp
labels: [webui, design, theme]
estimate: 5
created: 2026-09-24T21:01:11Z
updated: 2026-09-24T21:17:37Z
started: 2026-09-24T21:01:56Z
closed: 2026-09-24T21:17:37Z
---

## Description

Rewrite `web-ui/src/styles/tokens.css` as semantic tokens v2:
- surfaces: bg, shell, raised, card, input, overlay
- text: fg, muted, faint
- borders
- accent, accent-soft and focus-ring
- status colours, each with a soft variant
- radii, shadows, type scale, motion

Keep the old variable names as aliases for now. Map the tokens into the Tailwind 4 `@theme`.

Define four families (pando, paper, slate, forest), each with a light and a dark variant, plus accent presets.

Replace `hooks/useTheme.ts` with a zustand store (mode light/dark/system, family, accent) that persists to localStorage and to the backend config field `theme`. Legacy ids map as follows: claude and clay go to paper, starbucks goes to forest.

Add an inline init script in `index.html` so the theme applies before first paint. Keep the `meta theme-color` in sync, and make the Wails window background follow the mode.

Bundle Inter Variable and JetBrains Mono through `@fontsource-variable/*`.

Remove the `.pando-mascot-watermark` CSS.

Add a contrast validator script, `web-ui/scripts/check-contrast.mjs`.

## Acceptance Criteria

- [ ] Header and Settings share one theme state.
- [ ] Reloading causes no flash of the wrong theme.
- [ ] The contrast script passes for every family and mode.
- [ ] typecheck and build pass.
