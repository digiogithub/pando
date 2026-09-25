---
id: PANDO-US-0063
type: story
title: "B5 TUI brand: pando theme palette, Remembrances/Mesnada glyphs"
status: done
parent: PANDO-EP-0011
milestone: PANDO-M-0003
author: mcp
labels: [brand, tui]
created: 2026-09-25T08:13:54Z
updated: 2026-09-25T08:26:29Z
closed: 2026-09-25T08:26:29Z
---

## Description
Bring the brand into the TUI:
- Align the `pando` TUI theme (`internal/tui/theme/opencode.go`) with the Bosque, Marfil and Álamo colours.
- Show the 本 glyph in the Remembrances sections and the 众 glyph in the Mesnada and Orchestrator sections, with an ASCII fallback for non-Nerd-Font terminals.

## Acceptance Criteria
- `go test ./internal/tui/...` passes.
