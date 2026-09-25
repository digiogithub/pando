---
id: PANDO-EP-0011
type: epic
title: "Pando v1 brand identity: new logo, palette and section marks across WebUI, Desktop and TUI"
status: done
priority: high
milestone: PANDO-M-0003
author: mcp
labels: [brand, webui, desktop, tui]
created: 2026-09-25T08:13:36Z
updated: 2026-09-25T08:30:36Z
closed: 2026-09-25T08:30:36Z
---

## Description
Adopt the Pando v1 styleguide in `assets/pando-brand-v1/`:
- Symbol 木 drawn as monoline strokes with three circuit nodes.
- Palette: Bosque `#0F2A20`, Marfil `#F4F1E8`, Álamo `#E9B949`, Álamo oscuro `#C68A17`.
- Space Grotesk for the wordmark and display text; JetBrains Mono for mono text.
- Remembrances (本) and Mesnada (众) get their own marks and a small identity of their own:
  - their settings sections;
  - the Orchestrator view, which is linked to Mesnada;
  - the standalone Mesnada UI.

## Acceptance Criteria
- WebUI:
  - A themeable `BrandMark` component (strokes use the fg token, nodes use a brand-node token) replaces the 木 text glyphs in the title bar, empty chat, splash and busy animation.
  - Favicon, PWA icons, the manifest and theme-color come from the brand assets.
  - The `pando` theme family is aligned with the brand palette, and `check:contrast` passes.
- Remembrances and Mesnada settings, and the Orchestrator view, show section headers with their own marks.
  - The sidebar and settings nav use those marks too.
- Desktop: app icon, .ico and .icns come from the brand assets. Linux installer icon and README logo are updated.
- The standalone Mesnada UI (`internal/mesnada/ui`) uses the Mesnada favicon and logo.
- TUI: the `pando` theme is aligned with the palette, and the sections show the 本 and 众 glyphs.
- Verification: typecheck, lint, build and go tests pass.

## Notes
Source of truth stays in `assets/pando-brand-v1/`; `scripts/brand-sync.sh` copies derived files.
