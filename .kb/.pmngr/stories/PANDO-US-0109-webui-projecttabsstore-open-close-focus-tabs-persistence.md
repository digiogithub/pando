---
id: PANDO-US-0109
type: story
title: "WebUI: `projectTabsStore` (open/close/focus tabs, persistence, SSE sync, reload restore)"
status: backlog
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, webui]
estimate: 5
created: 2026-10-01T19:24:42Z
updated: 2026-10-01T19:24:42Z
---

## Description

State for the bottom tab bar lives in a new zustand store `packages/pando-client/src/stores/projectTabsStore.ts`.

- State: `tabs: ProjectTab[]` (`{projectId, name, path, state: 'starting'|'running'|'error'|'stopped', webUrl, delegations, openedAt, error?}`), `activeTabId: 'main' | projectId`, `order`.
- Actions: `openTab(projectId)` → `POST /api/v1/projects/{id}/web/open` (handles `project_needs_init` by reusing `projectStore.initDialogProject` flow, then retries), optimistic `starting` state, focus on success; `closeTab(projectId, {stop: boolean})` → optional `POST .../web/close`; `focusTab(id)`; `reorder`; `restore()` → `GET /api/v1/projects/web` on app start merged with `localStorage` order (namespaced key `pando_project_tabs`); `applyEvent(ev)` for `web_started|web_stopped|web_error|delegation_changed|status_changed` coming from `projectStore.connectEvents` (extend its listener list; keep one EventSource).
- Navigation coupling: focusing a project tab navigates to `/projects/:id/workspace`; focusing `main` returns to the last main route (store `lastMainRoute`). Browser back/forward keeps tabs consistent (route → tab sync in a `useProjectTabRouteSync` hook).
- Child mode: store is inert (no restore, no tabs) when `serverStore.startupMode === 'project-child'`.
- Toasts on error through the existing notifications pattern; strings through i18n (`projects.tabs.*`).

## Acceptance Criteria

- [ ] Unit tests (Vitest) for open/close/restore/applyEvent with mocked `api`.
- [ ] Reloading the page restores the same tabs and active tab; a tab whose child died on the server shows `error` with a restart action.
- [ ] No duplicate tabs for one project; opening from two browser windows converges via SSE.
