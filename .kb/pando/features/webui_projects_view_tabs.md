---
created_at: 2026-10-01T20:53:03.971445508Z
updated_at: 2026-10-01T20:53:03.971445508Z
tags:
    - feature
    - webui
    - projects
    - i18n
---
# Feature: Projects view opens project tabs (PANDO-US-0112)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-01. Uses [[webui_project_tabs_store]], [[webui_project_tab_bar]].

## What changed
- `web-ui/src/components/projects/ProjectsView.tsx`: row click -> `projectTabsStore.openTab(id)` in browser and desktop (focuses an already open tab; desktop no longer opens a window on click). Rows show web state (starting / running :port / error), delegation badges and `external`. Actions: Open/Focus tab, Stop (closes the tab; `useDialogs()` confirmation when delegations are in flight; `cancelled_delegations` toast kept), Open in new window (desktop only), Delete. Header "Deactivate" relabelled "Delegation target" with an explanatory tooltip.
- Init flow: uninitialised project -> `ProjectInitWizard` -> store retries `openTab` after init.
- i18n: whole view + init wizard + `projectStore` toasts moved to `projects.*` keys (toasts carry keys via `toastStore`, package stays framework-agnostic); shared `ConfirmDialog`, `PromptDialog`, `Toast`, `useDialogs` translate keys.
- Guard test `projects-i18n.guard.test.ts` (no hardcoded English JSX text / title / aria-label / placeholder in `components/projects/*.tsx`), component tests `ProjectsView.test.tsx`.

## Verification
`bun run typecheck` clean, `bun run lint` 0 errors (6 pre-existing warnings), `bun run test` 20 files / 98 tests, `bun run build` ok.