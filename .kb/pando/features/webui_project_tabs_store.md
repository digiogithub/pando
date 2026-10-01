---
created_at: 2026-10-01T20:07:23.138846127Z
updated_at: 2026-10-01T20:07:23.138846127Z
tags:
    - feature
    - webui
    - projects
---
# Feature: `projectTabsStore` for project workspace tabs (PANDO-US-0109)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-01. Builds on [[webui_storage_namespace_base_path]].

## What changed
- `web-ui/packages/pando-client/src/stores/projectTabsStore.ts` (new): state `tabs: ProjectTab[]`, `activeTabId: 'main' | projectId`, `order`, `lastMainRoute`; actions `openTab(projectId)` (optimistic `starting`, dedupe of concurrent opens, `POST /api/v1/projects/{id}/web/open`, `project_needs_init` handed to `projectStore.initDialogProject` and retried after init), `closeTab(projectId, {stop})` (`POST .../web/close` when stopping), `focusTab`, `reorder`, `restore()` (`GET /api/v1/projects/web` merged with the persisted order), `applyEvent(ev)` (`web_started|web_stopped|web_error|delegation_changed|status_changed`), `restartTab`. Persisted with `localBrowserStorage` key `pando_project_tabs`. Returns typed result codes + i18n keys instead of importing i18next into the package. Inert in child mode.
- `services/api.ts`: `isProjectChildMode()` (API base path matches `/api/v1/projects/<id>/web`).
- `projectStore.connectEvents`: listens to the new SSE events and forwards to registered listeners (no store import cycle); `initProject(id, {activateAfter})` returns success so `ProjectsView` closes the wizard only on success.
- Types: `ProjectTab`, `ProjectWebInstance`, `Project.web_state/web_port/web_url`.
- `web-ui/src/hooks/useProjectTabRouteSync.ts` (+ `projectWorkspacePath(id)`): route <-> active tab sync. Rewritten by the lead after review: the original two effects contradicted each other (tab click -> effect A refocused `main` while effect B navigated -> ping-pong). Now the route wins on navigation/mount and only a store-side focus change moves the URL, reading the live store value to survive StrictMode double effects. Not mounted yet (PANDO-US-0110/0111).
- i18n `projects.tabs.*` in en/es/fr/de/pt/ja/zh.

## Verification
`bun run typecheck` clean, `bun run lint` 0 errors (6 pre-existing warnings), `bun run test` 13 files / 78 tests passed (11 new store tests), `bun run build` ok.