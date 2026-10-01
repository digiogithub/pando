---
id: PANDO-US-0106
type: story
title: "Child mode: recursion guard and `project-child` startup mode for nested instances"
status: backlog
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, backend, webui]
estimate: 3
created: 2026-10-01T19:23:08Z
updated: 2026-10-01T19:23:08Z
---

## Description

A web child is a full Pando instance; it must not become a parent itself and its UI must behave as an embedded workspace.

- `internal/app/app.go`: when `PANDO_PARENT_INSTANCE` is set, do not create a `ProjectManager` that can spawn (`app.ProjectManager` stays non-nil but with `spawnDisabled`), skip `SeedFromGlobal` auto-registration side effects that would re-register the parent, keep delegation routing to *external* peers over IPC (the parent may still be a warm target through `DelegateExternal`).
- `api.ServerConfig.StartupMode = "project-child"` when the env var is set (serve/app/desktop commands); `GET /api/v1/server/info` (or the existing version/health payload used by `serverStore`) returns `startup_mode`, `parent_instance_id`, `project_id`, `project_name`, `public_base_path`.
- `/api/v1/projects/*` and `/api/v1/instances/*` return 409 `not_available_in_child` on a child.
- Child disables listener rebinding to `0.0.0.0` (`handlers_external_access.go`) — the parent's external-access toggle covers it.
- WebUI child mode (`serverStore.startupMode === 'project-child'`): hide `nav.projects`, `nav.instances`, the external-access footer toggle, the setup wizard first-run flow and the project tab bar; show `project_name` in the title bar; `Ctrl+P/Ctrl+O` shortcuts still work inside the iframe.

## Acceptance Criteria

- [ ] Starting `pando serve` with `PANDO_PARENT_INSTANCE=x` never spawns `pando acp`/`pando serve` children (test with `OpenWeb`/`Activate` on the child's manager returning `ErrChildInstance`).
- [ ] Server info reports child mode; WebUI hides the listed navigation when set.
- [ ] Tests in `internal/app` (or `internal/api`) and a Vitest/unit test for the nav filter.
