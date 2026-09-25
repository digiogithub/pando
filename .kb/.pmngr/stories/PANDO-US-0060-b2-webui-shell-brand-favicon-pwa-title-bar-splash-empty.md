---
id: PANDO-US-0060
type: story
title: "B2 WebUI shell brand: favicon/PWA, title bar, splash, empty chat, busy animation, pando theme palette"
status: done
parent: PANDO-EP-0011
milestone: PANDO-M-0003
author: mcp
labels: [brand, webui]
created: 2026-09-25T08:13:53Z
updated: 2026-09-25T08:26:29Z
closed: 2026-09-25T08:26:29Z
---

## Description
Replace the 木 text glyphs and the old icons with BrandMark and the brand assets:
- `index.html`: add the favicon and theme-color.
- PWA icons: update the Vite manifest and generate a maskable icon.
- Align the `pando` theme family and the gold accent with the Bosque, Marfil and Álamo colours.
- Use Space Grotesk as the display font for the splash and hero text.
- Busy animation: pulse the circuit nodes, and respect reduced motion.

## Acceptance Criteria
- `check:contrast`, typecheck, lint and build all pass.
- Screenshots taken in light and dark.
