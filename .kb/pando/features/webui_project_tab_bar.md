---
created_at: 2026-10-01T20:22:03.494209167Z
updated_at: 2026-10-01T20:22:03.494209167Z
tags:
    - feature
    - webui
    - projects
    - design
---
# Feature: bottom project tab bar (PANDO-US-0110)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-01. Consumes [[webui_project_tabs_store]].

## What changed
- `web-ui/src/components/layout/ProjectTabBar.tsx` + `ProjectTabBarControls.ts` (`useProjectTabBarController`, `handleProjectTabKeyboardShortcut`, `SHELL_MAIN_PANEL_ID`): home tab (brand + workspace name) and one tab per project (folder icon, state dot starting/running/error, delegation badge, close on hover/always on touch), overflow scroll with fade edges, middle-click close, context menu (Close, Close & stop, Restart, Open in new window on desktop, Reveal in Projects), close confirmation through `useDialogs()` with the stop choice remembered per session, toasts from store notices. `role=tablist/tab`, `aria-controls` -> `#SHELL_MAIN_PANEL_ID`.
- `src/styles/shell.css`: `.shell-projectbar*`, `--shell-projectbar-h: 32px`, mobile chip row.
- `MainLayout.tsx`: renders the bar below `StatusBar` (hidden with no project tabs, in child mode, and in simple mode unless a project tab is open), mounts `useProjectTabRouteSync()`, restores tabs and keeps the projects SSE connected in the main instance, shortcuts `Ctrl+Alt+1..9`, `Ctrl+Alt+W`, `Ctrl+Alt+Left/Right` (also listed in QuickMenu).
- i18n `projects.tabs.*` additions in all 7 locales.
- Tests: `ProjectTabBar.test.tsx` (Vitest); `e2e/project-tabs.spec.ts` written with the `PANDO_E2E_BASE_URL` skip convention (not run: needs a live `pando app`, covered in PANDO-US-0115).

## Verification
`bun run typecheck` clean, `bun run lint` 0 errors, `bun run test` 16 files / 87 tests, `bun run build` ok. No screenshots yet (PANDO-US-0115).