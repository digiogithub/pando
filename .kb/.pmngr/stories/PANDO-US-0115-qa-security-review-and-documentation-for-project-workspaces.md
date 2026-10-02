---
id: PANDO-US-0115
type: story
title: QA, security review and documentation for project workspaces
status: done
priority: medium
parent: PANDO-EP-0019
author: mcp
labels: [projects, qa, docs, security]
estimate: 5
created: 2026-10-01T19:24:43Z
updated: 2026-10-02T09:38:52Z
closed: 2026-10-02T09:38:52Z
---

## Description

- Playwright E2E (`web-ui/e2e`, pattern from memory `reference_webui_e2e_playwright`: `pando app`, system Chrome, isolated HOME): register a temp project, click it, wait for the tab `running`, chat inside the frame, open the child terminal, reload and assert restore, close with stop, assert the process is gone (`GET /api/v1/projects/web` empty).
- Go: `go test ./internal/project ./internal/api ./internal/app ./internal/instanceregistry ./internal/llm/agent`; race detector on `internal/project`.
- Security review (`/security-review` on the branch) focused on: proxy path traversal (`..`, encoded slashes) to other loopback ports, header stripping, WebSocket origin, child token at rest (must be memory only), basic-auth bypass through the proxy, child never exposed on non-loopback, `PANDO_PARENT_INSTANCE` spoofing (a child pretending to be a parent gains nothing).
- Resource limits: cap on simultaneous web children (config `projects.maxWebInstances`, default 6) with a clear error; startup timeout configurable (`projects.webStartupTimeout`).
- Docs: `docs/` site page "Project workspaces" (screenshots, shortcuts, config keys, how delegation reuses the instance), API reference for the new endpoints, config reference. KB: `pando/features/project_workspaces_tabs.md` summary (what/why/files/verification) per CLAUDE.md, and update memory `project_projects_webserver_plan` as superseded by PANDO-EP-0019.

## Acceptance Criteria

- [ ] E2E suite green in CI (`digiogithub/ci-actions`), Go tests green with `-race` on `internal/project`.
- [ ] Security review findings triaged; no open critical/high.
- [ ] Docs and KB document merged in the same PR series.
