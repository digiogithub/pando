---
id: PANDO-EP-0010
type: epic
title: "WebUI native redesign: design system, themes, shell and chat"
status: done
priority: high
milestone: PANDO-M-0003
author: mcp
labels: [webui, design, theme]
created: 2026-09-24T21:00:36Z
updated: 2026-09-24T22:10:13Z
started: 2026-09-24T21:01:52Z
closed: 2026-09-24T22:10:13Z
---

## Description

Redesign the WebUI (`web-ui/`, React 19 + Vite + Tailwind 4 + zustand) so it looks and feels like a native desktop app. The references are Claude Desktop and Zeron (https://github.com/zeronsh/zeron): neutral surfaces, one restrained accent, a centered reading column, no avatars, a pill-shaped composer, compact tool-activity rows and a slim title bar.

Problems today:
- The default theme uses 0px radii and heavy gold.
- A mascot watermark sits behind the chat.
- There are about 1,840 ad-hoc inline style objects.
- FontAwesome icons are used in 53 files.
- `useTheme` is per-component state, so the Header toggle and the Settings picker drift apart.

What the epic delivers:
- Semantic tokens v2.
- A theme model built from family × mode (light/dark/system) × accent.
- Bundled Inter and JetBrains Mono fonts.
- Lucide icons.
- A `components/ui` primitives library.
- Migration of every view to the new tokens and primitives.

Full design decisions: KB `pando/plans/webui_native_redesign_plan.md`.

## Acceptance Criteria

- [ ] The chat has no mascot watermark. An empty chat shows a centered greeting and composer.
- [ ] One title-bar button switches between light and dark. The choice persists and applies before first paint, with no flash.
- [ ] The colour scheme can be changed in Settings → Appearance: 4 families plus accent presets. Legacy theme ids migrate automatically.
- [ ] All views use tokens and primitives. No FontAwesome remains.
- [ ] Monaco, xterm and highlight.js follow the active theme.
- [ ] Text contrast is at least 4.5:1 for primary text and at least 3:1 for muted text in every family and mode, checked by a script.
- [ ] typecheck, lint, build and build:embedded pass. The desktop window background matches the mode.

## Notes

Order: P1 and P2 (foundation) first. P3 to P6 then run in parallel on disjoint folders. P7 (QA and cleanup) comes last.
