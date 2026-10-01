---
id: PANDO-EP-0019
type: epic
title: "Project workspaces in the WebUI: open a project in a background Pando instance and drive it from a bottom tab bar in the unified WebUI"
status: in_progress
priority: high
author: mcp
labels: [projects, webui, api, ipc, desktop, delegation]
created: 2026-10-01T19:22:00Z
updated: 2026-10-01T19:52:21Z
started: 2026-10-01T19:52:21Z
---

## Description

Today a Pando instance registers its working directory as a project (`internal/project`, global registry in `config`). The Projects view in the WebUI (`web-ui/src/components/projects/ProjectsView.tsx`) lists them, and "activating" one spawns a **headless `pando acp` child** (`project.Manager.spawnChild`, `internal/project/manager.go:196`) that is only reachable through delegation tools (`mesnada_spawn_agent` with `project`, `Manager.WarmDelegate`). In the browser a row click just toggles start/stop; in the desktop app it opens a separate `pando desktop` window (`handlers_projects_desktop.go`). There is no way to *use* the child instance from the WebUI: no sessions, no chat, no settings.

Goal: clicking a project in the Projects view launches (or reuses) a **background `pando serve` instance** working in that project's directory with that project's configuration, and the WebUI shows a **bottom tab bar, below the current status bar/footer**, with one tab per opened project. Selecting a tab shows that instance's full WebUI (chat, sessions, new session, settings, terminal, files…) inside the unified interface, exactly as if the user had opened that instance's own URL, while the main instance keeps its own tab. Closing a tab optionally stops the child.

## Current state (analysis, 2026-10-01)

Backend
- `project.Manager` (`internal/project/manager.go`) spawns `pando acp` over stdio (`acpsdk.ClientSideConnection`), tracks `instances map[projectID]*Instance`, publishes `ManagerEvent` (`switched`, `status_changed`, `init_required`, `delegation_changed`) consumed by `GET /api/v1/projects/events` (SSE). `Instance` (`internal/project/instance.go`) is ACP-only: conn, delegation slots, idle GC (`delegation_gc.go`).
- `cmd/serve.go` always listens **HTTPS with a self-signed cert** (`tlsutil.EnsureCert` under the project `.pando/`), picks a free port (`chooseAvailablePort`, `cmd/app.go:30`), bootstraps IPC (`ipcruntime.Bootstrap` → primary/secondary by `.pando/ipc.lock`), announces to `instanceregistry` (`Entry` has no HTTP port field) and starts the ZMQ bus + bridge handlers when primary.
- API auth (`internal/api/server.go`): per-process random token (`X-Pando-Token` header or `?token=`), `GET /api/v1/token` served freely only on loopback binds or behind basic auth. PTY terminal is a WebSocket (`handlers_terminal_pty.go`, origin-checked). Many endpoints are SSE (fetch-based or `EventSource`).
- `internal/api/handlers_instances.go` already proxies a *subset* of remote control over ZMQ RPC (`session.list`, `message.list`, `message.send`, `session.interrupt`, PUB stream as SSE) — not the full REST surface.
- No recursion guard: a child `pando serve` would create its own `ProjectManager` and could spawn grandchildren.
- `projects` table has `acp_pid`, `acp_port`; nothing for an HTTP port. An old plan (KB "Projects Web Server Mode", 2026-05) sketched `WebUIPort` + `setBaseURL` switching but was never implemented.

WebUI (`web-ui/`, React 19 + react-router + zustand, shared package `packages/pando-client`)
- One **module-singleton API client** (`packages/pando-client/src/services/api.ts`: `baseURL` from `window.__PANDO_API_BASE__`, token in `localStorage['pando_token']`), one singleton per store (`sessionStore`, `settingsStore` ×7, `terminalStore`, `editorStore`, `fileChangesStore`, `designStore`, `notificationsStore`, `serverStore`…). Nothing is keyed by instance.
- Inconsistencies: `hooks/useChat.ts:553` calls `createSSEStream('/api/v1/chat/stream')` relative to origin; `useChat.ts:602` and `components/instances/RemoteSessionView.tsx:88` read `window.__PANDO_API_BASE__` directly instead of `getBaseURL()`.
- Shell: `components/layout/MainLayout.tsx` → `.shell` grid: `Header` (`.shell-titlebar`), banners, `.shell-body` (Sidebar + `main.shell-main > Outlet`), `StatusBar` (`footer.shell-statusbar`, `--shell-statusbar-h: 26px`, hidden in simple chat mode). Styles in `src/styles/shell.css` (tokens v2, EP-0010). `layoutStore` holds only UI flags.
- `projectStore` (`packages/pando-client/src/stores/projectStore.ts`): `activateProject`, `stopProject`, `openProjectDesktop`, `deactivateProject`, `initProject`, `connectEvents` (SSE with backoff). `Project` type has no URL/port.
- i18n: i18next, `src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json`; Projects view strings are mostly hardcoded English.
- Desktop (Wails, `desktop/`) loads the same WebUI from the server origin; native dialogs are dead (`useDialogs()`), see memory `fix_webui_native_dialogs_wails`.

## Architecture decision

**Child = full `pando serve` on loopback; the parent reverse-proxies it; the tab hosts the child's own embedded WebUI in an iframe served through that proxy.**

1. **Spawn**: `ProjectManager` gains a second instance kind, `WebInstance` (`pando serve --host 127.0.0.1 --port <free> --cwd <path>`), separate from the ACP delegation child. Child env: `PANDO_PARENT_INSTANCE=<parent instanceID>`, `PANDO_PROJECT_ID=<id>`. Parent polls `/health` until ready (timeout), records `web_port`, PID, child token, and publishes `web_started` / `web_stopped` events. Port, PID and parent id are also announced in `instanceregistry.Entry` (new fields `WebPort`, `ParentInstanceID`) so TUI/other instances can see it and so the parent can re-adopt children after a restart.
2. **TLS/auth between parent and child**: the child keeps HTTPS; the parent passes its *own* cert/key (`--tls-cert/--tls-key`) so the proxy can pin the certificate, and reads the child token once from `GET /api/v1/token` over loopback (allowed on loopback binds). The child token never reaches the browser.
3. **Reverse proxy** in the parent API: `ANY /api/v1/projects/{id}/web/{path...}` → `https://127.0.0.1:<port>/{path}`. Must handle: streaming responses (SSE flush per event, no buffering), WebSocket upgrade (PTY), large uploads, `X-Pando-Token` injection (the parent's own token/basic-auth protects the proxy route), rewrite of the child's HTML bootstrap so `window.__PANDO_API_BASE__` = `/api/v1/projects/{id}/web` and the SPA router has that basename. This gives a single origin, single credential, works through basic auth / external access, and needs no cert acceptance in the browser.
4. **WebUI**: new `projectTabsStore` + `ProjectTabBar` rendered under `StatusBar` (`.shell-projectbar`), tab 0 = main instance (current app, routes unchanged), tab N = `/projects/:id/workspace` hosting a keep-alive `<iframe src="/api/v1/projects/{id}/web/">`. The embedded WebUI runs in **child mode** (`StartupMode: "project-child"` from the env guard): hides Projects/Instances nav and the project tab bar, shows the project name in the title bar, and namespaces every `localStorage` key by API base so the iframe (same origin as the parent) does not clobber `pando_token`, `pando_language`, `pando_sidebar_open`, `pando_theme`, etc.
5. **Delegation reuse**: when a web child is running for a project, `mesnada_spawn_agent` targeting that project routes over IPC to it (`Manager.DelegateExternal` path, which already exists for editor-launched peers) instead of spawning a second `pando acp` in the same directory. Idle GC never kills a user-opened web instance.
6. **Desktop**: the same tab bar works in the Wails window; "open in new window" stays as a secondary row action (`open-desktop`).

Rejected alternatives (recorded for reference): (a) `setBaseURL()` switching of the single client to the child's URL — self-signed HTTPS cross-origin, every store would need reset/rehydrate, token clash; (b) full multi-instance refactor of `packages/pando-client` (store factories keyed by instance, ~30 stores / 123 import sites) — right long-term, too large for this epic; the iframe host keeps the door open since the child UI is the same bundle.

## Acceptance Criteria

- [ ] Clicking a project in the Projects view (browser and desktop) starts a background `pando serve` child in that path with that project's config, or reuses a running one, and opens/focuses its tab in a bottom tab bar under the status bar.
- [ ] Each tab shows the full WebUI of its instance (chat, sessions, new session, settings, terminal, editor, snapshots…) working against that instance; switching tabs preserves each tab's state (keep-alive).
- [ ] The main instance keeps its own tab; closing a project tab asks whether to stop the child; stopping from the Projects view or from the child's exit updates the tab bar live (SSE).
- [ ] Open tabs survive a page reload (restored from the parent's list of running web children) and a parent restart re-adopts still-alive children from `instanceregistry`.
- [ ] A child never spawns its own project children; its UI hides Projects/Instances and the tab bar.
- [ ] Only the parent's credential is needed; the child token never reaches the browser; the proxy only targets loopback children owned by the parent; PTY WebSocket and SSE streams work through the proxy, including behind basic auth and external access.
- [ ] `mesnada_spawn_agent` to a project with a running web child reuses it over IPC (no duplicate `pando acp`), and the Projects view shows delegation counts for it.
- [ ] Go tests: manager lifecycle, proxy (SSE/WebSocket/auth/path rewrite), routes, child-mode guard. Playwright E2E: open project tab, send a chat message inside it, close tab. `go test ./internal/llm/agent ./internal/api ./internal/project` and `bun run build` pass.
- [ ] i18n keys for the new UI in all seven locales; KB summary document and docs site page written.

## Notes

- Memory/KB references: `project_projects_plan` (phases 1-6), `project_projects_webserver_plan` (superseded sketch), `feature_webui_projects_stop_instance`, `fix_warm_acp_pending_self_delegation`, `feature_external_access_footer_toggle`, `webui_phase1_layout`, `webui_redesign_p3_app_shell`, `fix_webui_native_dialogs_wails`.
- Risks: IPC lock contention when an ACP delegation child and a web child share a directory (one becomes secondary — story on delegation reuse removes the case); port exhaustion; child startup time (config load + DB migrations) → show "starting" state; iframe `localStorage` namespace must be done before anything else or the parent session is logged out.
- Suggested order: US backend spawn/lifecycle → TLS+token handshake → proxy → child mode guard → API/SSE surface → WebUI storage namespacing → tabs store + bar → iframe workspace → Projects view wiring → delegation reuse → desktop polish → QA/docs.
