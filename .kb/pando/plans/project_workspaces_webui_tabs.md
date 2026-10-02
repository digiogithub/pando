---
created_at: 2026-10-01T19:25:19.199705599Z
updated_at: 2026-10-01T19:25:19.199705599Z
tags:
    - plan
    - analysis
    - projects
    - webui
    - api
---
# Plan: Project workspaces in the WebUI (bottom tab bar per project instance)

**Date:** 2026-10-01 · **Tracker:** gintrack epic PANDO-EP-0019, stories PANDO-US-0103 … PANDO-US-0115 · **Status:** implemented (stories 0103-0115 shipped, including limits, proxy/UI flows, delegation reuse, QA, and docs)

## Goal
Clicking a project in the WebUI Projects view launches/reuses a background `pando serve` in that project's directory and the WebUI shows a bottom tab bar (under the status bar) with one tab per opened project; each tab shows the full WebUI of that instance (chat, sessions, settings, terminal…) inside the unified interface.

## Current state (verified in code)
- `internal/project/manager.go` spawns a headless `pando acp` child per activated project (`spawnChild`, stdio ACP); reachable only through delegation (`WarmDelegate`, `DelegateExternal`). Projects view click = start/stop toggle (browser) or new `pando desktop` window (desktop, `handlers_projects_desktop.go`).
- `cmd/serve.go`: always HTTPS self-signed (`tlsutil.EnsureCert`), free port via `chooseAvailablePort` (`cmd/app.go:30`), IPC primary/secondary via `.pando/ipc.lock`, `instanceregistry.Entry` has no HTTP port field. Per-process token, `GET /api/v1/token` open on loopback binds. PTY = WebSocket. No recursion guard in `internal/app/app.go` (every instance builds a `ProjectManager`).
- `internal/api/handlers_instances.go`: partial remote control over ZMQ RPC only.
- WebUI: single API client singleton (`web-ui/packages/pando-client/src/services/api.ts`, `window.__PANDO_API_BASE__`, `localStorage['pando_token']`), ~30 singleton zustand stores, none keyed by instance. Base-URL bypasses: `hooks/useChat.ts:553` (relative `/api/v1/chat/stream`), `useChat.ts:602`, `components/instances/RemoteSessionView.tsx:88`. Shell: `MainLayout.tsx` (`.shell` grid, `StatusBar` = `footer.shell-statusbar`, `shell.css`). react-router routes in `src/App.tsx`. i18n i18next, 7 locales; Projects view mostly hardcoded English.

## Decision
Child = full `pando serve --host 127.0.0.1` with env `PANDO_PARENT_INSTANCE`, `PANDO_PROJECT_ID`, `PANDO_PUBLIC_BASE=/api/v1/projects/<id>/web`; parent reverse-proxies `ANY /api/v1/projects/{id}/web/*` (pinned TLS, child token injected server-side, SSE flush, WebSocket upgrade); the tab hosts the child's own embedded WebUI in a keep-alive same-origin iframe. Child runs in `project-child` startup mode (no nested spawning, hides Projects/Instances nav). Browser storage keys namespaced by API base so the iframe does not clobber the parent's token/language. Delegation to a project with a running web child reuses it over IPC (`DelegateExternal`) instead of spawning a second `pando acp`.

Rejected: switching `setBaseURL()` of the single client (cross-origin self-signed HTTPS, store reset, token clash); full multi-instance store refactor (too large; iframe approach keeps it possible later).

## Stories (order)
1. US-0103 ProjectManager `WebInstance` spawn/lifecycle/DB/re-adoption
2. US-0104 TLS pinning + token handshake + `instanceregistry` `web_port`/`parent_instance_id`
3. US-0105 Reverse proxy + UI bootstrap rewrite (`PublicBasePath`)
4. US-0106 Child mode guard (`project-child`)
5. US-0107 API `web/open`, `web/close`, `GET /projects/web`, `web_*` fields, SSE `web_started|web_stopped|web_error`
6. US-0108 pando-client storage namespacing + base-URL fixes + router basename
7. US-0109 `projectTabsStore`
8. US-0110 `ProjectTabBar` under status bar
9. US-0111 `/projects/:id/workspace` keep-alive iframe + postMessage bridge
10. US-0112 Projects view rewiring + i18n
11. US-0113 Delegation reuse over IPC, GC exclusion
12. US-0114 Desktop/TUI follow-ups
13. US-0115 QA, security review, docs

Supersedes [[project_projects_webserver_plan]] (2026-05 sketch). Related: [[webui_redesign_p3_app_shell]], [[external_access_footer_toggle]], [[webui_basic_auth]].