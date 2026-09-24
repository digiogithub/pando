---
id: PANDO-US-0053
type: story
title: "P3 App shell: title bar, sidebar, status bar, theme toggle button"
status: done
priority: high
parent: PANDO-EP-0010
milestone: PANDO-M-0003
author: mcp
labels: [webui, design]
estimate: 5
created: 2026-09-24T21:01:11Z
updated: 2026-09-24T21:27:09Z
started: 2026-09-24T21:19:14Z
closed: 2026-09-24T21:27:09Z
---

## Description

Restyle `components/layout/*` (MainLayout, Sidebar, Header, StatusBar, ExternalAccessToggle) in the style of Claude Desktop and Zeron:
- **Header**: a slim title bar. It works as the Wails drag region, leaves an inset for the macOS traffic lights, and has the sidebar toggle.
- **Theme toggle**: a one-click sun/moon button with a tooltip, bound to the shortcut Ctrl/Cmd+Shift+L.
- **Sidebar**: collapsible, in the shell surface. It holds a New chat button, search, a recents list with subtle hover and selection, and navigation plus settings at the bottom.
- **Status bar**: minimal, faint text.
- **Mobile**: keep the mobile layout working.

Use lucide icons and the primitives from P2.

## Acceptance Criteria

- [ ] The shell looks native in both light and dark mode.
- [ ] The toggle button switches mode instantly and the choice persists.
- [ ] The mobile layout is not broken.
- [ ] typecheck passes.
