---
id: PANDO-US-0107
type: story
title: "Projects API: open/close web instance endpoints, `web_*` fields and SSE events"
status: done
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, api]
estimate: 5
created: 2026-10-01T19:23:08Z
updated: 2026-10-01T20:53:03Z
closed: 2026-10-01T20:53:03Z
---

## Description

Surface the web instance lifecycle over REST and the existing SSE stream so the WebUI tab bar can be driven and restored.

- `POST /api/v1/projects/{id}/web/open` → `Manager.OpenWeb`; 200 `{status: "opened"|"already_open", project_id, web_url: "/api/v1/projects/{id}/web/", web_port}`; 409 `project_needs_init` (same contract as activate), 409 `child_instance` when the server itself is a child, 502 `child_startup_failed` with stderr tail.
- `POST /api/v1/projects/{id}/web/close` → `Manager.CloseWeb`; returns `cancelled_delegations` like `stop`.
- `GET /api/v1/projects/web` → list of open web instances `{project_id, name, path, web_port, pid, state, started_at, delegations}` (used to restore tabs on reload).
- `projectResponse` gains `web_state` (`starting|running|error|stopped`), `web_port`, `web_url`; `enrichRuntime` fills them from the manager.
- `handleProjectEvents` maps `EvWebStarted/EvWebStopped/EvWebError` to SSE events `web_started`, `web_stopped`, `web_error` carrying `project_id`, `web_port`, `error`.
- Decide semantics of existing endpoints: `activate` keeps spawning the ACP child for delegation; `stop` stops both kinds; `open-desktop` unchanged. Document in the handler comments.
- OpenAPI/docs: update `docs/` API page for projects.

## Acceptance Criteria

- [ ] Handler tests for open/close/list with a fake manager (pattern of `handlers_projects_desktop_test.go`).
- [ ] SSE test asserting the three new event names.
- [ ] `web_url` always relative to the parent origin (works behind basic auth / external access).
