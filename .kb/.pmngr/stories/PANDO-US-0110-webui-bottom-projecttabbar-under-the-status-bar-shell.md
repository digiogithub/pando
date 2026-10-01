---
id: PANDO-US-0110
type: story
title: "WebUI: bottom `ProjectTabBar` under the status bar (shell integration, states, shortcuts, mobile, i18n)"
status: done
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, webui, design]
estimate: 5
created: 2026-10-01T19:24:43Z
updated: 2026-10-01T20:22:03Z
closed: 2026-10-01T20:22:03Z
---

## Description

Render the tabs as a new shell row **below** `footer.shell-statusbar` in `components/layout/MainLayout.tsx`.

- `components/layout/ProjectTabBar.tsx` + styles in `src/styles/shell.css` (`.shell-projectbar`, `--shell-projectbar-h: 32px`, tokens v2 only — no inline styles, follow EP-0010 primitives `Button/IconButton/Badge/Tooltip/Menu`). Grid of `.shell` gains the row; hidden when there are no project tabs (main only) and in simple chat mode unless a project tab is open; hidden entirely in child mode.
- Tab anatomy: home tab (main instance, brand mark + workspace name from `projectStore.workspace`), then one tab per project: folder icon, name, state dot (`starting` spinner, `running`, `error`), delegation badge (`delegations > 0`), close button (hover / always on touch). Active tab uses the `.shell-main` accent. Overflow: horizontal scroll with fade edges; middle-click closes; right-click/long-press context menu: Close, Close & stop instance, Restart, Open in new window (desktop only, reuses `open-desktop`), Reveal in Projects.
- Close behaviour: `useDialogs().confirm` (never `window.confirm`, Wails) asking whether to stop the background instance; remember choice per session.
- Keyboard: `Ctrl+Alt+1..9` focus tab N, `Ctrl+Alt+W` close current project tab, `Ctrl+Alt+Left/Right` cycle; register in the existing MainLayout shortcut handler and document in the QuickMenu.
- Mobile (≤768px): bar becomes a scrollable chip row; status bar + project bar must not exceed the safe-area; test with the mobile master-detail layouts.
- i18n keys under `projects.tabs.*` in all seven locale files; `aria-*` roles (`tablist`/`tab`).

## Acceptance Criteria

- [ ] Visual parity with the status bar in every theme (light/dark, high contrast) — screenshots attached to the PR.
- [ ] Tabs reflect SSE state changes within 1 s without reload.
- [ ] Playwright: open two projects, switch, close one with "stop", bar hides when last project tab closes.
