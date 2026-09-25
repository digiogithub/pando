---
id: PANDO-US-0062
type: story
title: B4 Desktop, packaging, Mesnada standalone UI, README
status: done
parent: PANDO-EP-0011
milestone: PANDO-M-0003
author: mcp
labels: [brand, desktop]
created: 2026-09-25T08:13:54Z
updated: 2026-09-25T08:25:04Z
closed: 2026-09-25T08:25:04Z
---

## Description
Replace the icons and logos outside the WebUI with the brand assets:
- `desktop/build/appicon.png`, plus the Windows `.ico` and macOS `.icns` for Wails.
- The Linux `.desktop` icon, and the icon URL in `scripts/install-linux.sh`.
- The favicons and logo in `internal/mesnada/ui`.
- The README logo, using `<picture>` for light and dark.

## Acceptance Criteria
- `go build ./...` passes.
- The Mesnada UI serves its assets.
