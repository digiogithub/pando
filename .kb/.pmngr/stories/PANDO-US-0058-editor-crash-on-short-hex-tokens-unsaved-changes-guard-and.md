---
id: PANDO-US-0058
type: story
title: Editor crash on short-hex tokens; unsaved-changes guard and Esc in settings
status: done
priority: high
parent: PANDO-EP-0010
milestone: PANDO-M-0003
author: mcp
labels: [webui, settings, bug]
estimate: 5
created: 2026-09-25T07:18:02Z
updated: 2026-09-25T07:29:18Z
started: 2026-09-25T07:18:33Z
closed: 2026-09-25T07:29:18Z
---

## Description

1. **Bug:** opening a file in the code editor crashes with the React error boundary and the message "Illegal value for token color: #fff".
   - Cause: in the production CSS, Lightning CSS minifies `--bg:#ffffff` to `#fff`, but Monaco only accepts 6- or 8-digit hex.
   - Fix: add `web-ui/src/lib/cssColor.ts` (`toHexColor`, `cssVarHex`) and use it in `components/editor/monacoTheme.ts` and `components/chat/DiffViewer.tsx`.
2. **Feature:** in settings, pressing Esc returns to the chat.
   - Leaving a settings panel with unsaved changes asks the user to Save, Discard or Cancel.
   - This applies to Esc, switching category, any in-app navigation away from settings, and closing or reloading the tab.

## Acceptance Criteria

- [ ] Opening a file in /editor works in both light and dark mode, in the production build.
- [ ] Esc in settings with no pending changes navigates to the chat.
- [ ] With pending changes, Esc, a category switch or a route change shows the dialog. Save persists and continues; Discard resets and continues; Cancel stays on the page.
- [ ] `beforeunload` warns while there are unsaved changes.
- [ ] typecheck, lint and build pass.
