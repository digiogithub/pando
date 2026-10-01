---
id: PANDO-US-0112
type: story
title: "Projects view: click opens the project tab; status column, row actions and i18n"
status: backlog
priority: medium
parent: PANDO-EP-0019
author: mcp
labels: [projects, webui]
estimate: 3
created: 2026-10-01T19:24:43Z
updated: 2026-10-01T19:24:43Z
---

## Description

Rewire `components/projects/ProjectsView.tsx` to the new behaviour.

- `handleRowClick` → `projectTabsStore.openTab(id)` in both browser and desktop (desktop no longer spawns a window on click). `openProjectDesktop` stays as an explicit row action ("Open in new window", desktop only).
- Row shows: web state (`starting/running/error`) with port, ACP delegation state (existing `delegations` / `delegation_spawned` badges), `external` badge; actions: Open tab / Focus tab (when already open), Stop (stops web + ACP, confirms when delegations > 0 using existing `cancelled_delegations` toast), Open in new window, Rename, Delete.
- "Active project" concept (`activateProject`/`deactivateProject`, header Deactivate button): keep for the TUI/delegation semantics but relabel to "Delegation target" with a tooltip; do not conflate with tabs.
- Translate the view: table headers, badges, toasts, empty state, init wizard strings → `projects.*` keys in all locales (today hardcoded English).
- Keep `ProjectInitWizard` flow: opening a tab on an uninitialised project runs init then opens.

## Acceptance Criteria

- [ ] Clicking a stopped project ends with a focused tab showing its chat; clicking an open one focuses its tab.
- [ ] Stop from the list closes the tab (with confirmation) and updates within 1 s.
- [ ] No hardcoded English left in `components/projects/*` (i18n lint/grep).
