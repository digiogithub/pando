---
created_at: 2026-10-01T20:41:21.665953815Z
updated_at: 2026-10-01T20:41:21.665953815Z
tags:
    - feature
    - webui
    - projects
---
# Feature: project workspace route with keep-alive iframes (PANDO-US-0111)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-01. Builds on [[webui_project_tab_bar]], [[project_web_reverse_proxy]].

## What changed
- Route `projects/:id/workspace` (`App.tsx`) -> `components/projects/ProjectWorkspace.tsx`: overlays only (starting spinner with path, error detail + Restart/Close, stopped -> Reopen, unknown project -> back to Projects).
- `components/layout/ProjectFrameHost.tsx`, mounted once in `MainLayout` (main instance only) over the content area: one `<iframe src={tab.webUrl}>` per open running tab (`allow="clipboard-read; clipboard-write"`, no sandbox). Frames stay mounted until the tab closes; inactive frames get `hidden` + `inert`; all hidden when the route is not a workspace route.
- `lib/projectFrameBridge.ts`: same-origin `postMessage` bridge validating `event.origin` and that `event.source` is one of our frames. Child -> parent: `pando:title`, `pando:busy`, `pando:notification`, `pando:shortcut`. Parent -> child: `pando:focus`, `pando:theme`, `pando:language` (sent on load and on change). Child side `hooks/useProjectChildBridge.ts` (only in child mode inside a frame) posts title/busy/notifications/shortcuts and applies theme/language/UI size without persisting over the child's namespaced prefs; focus targets the chat input (`ChatInput.tsx`).
- Header shows the active child's session title (`headerSections.ts`); tab bar shows busy spinner; `useAgentBusy`, `useTheme`, `uiScale` gained non-persisting apply paths.
- i18n `projects.workspace.*` in 7 locales.

## Verification
`bun run typecheck` clean, `bun run lint` 0 errors, `bun run test` 18 files / 92 tests (new `projectFrameBridge.test.ts`, `ProjectFrameHost.test.tsx`), `bun run build` ok. E2E frame scenario added to `e2e/project-tabs.spec.ts` (skips without `PANDO_E2E_BASE_URL`).