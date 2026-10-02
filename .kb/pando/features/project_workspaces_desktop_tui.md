---
created_at: 2026-10-02T10:52:36+02:00
updated_at: 2026-10-02T10:52:36+02:00
tags:
  - feature
  - desktop
  - tui
  - projects
---
# Feature: project workspaces desktop + TUI follow-ups (PANDO-US-0114)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-02.

## What changed
- **WebUI shell (Part A UX fix)**: `MainLayout.tsx`, `Header.tsx`, `DesktopWindowControls.tsx`, `desktopWindow.ts`, `shell.css`, plus Vitest coverage. On `/projects/:id/workspace` the parent shell now keeps only the title bar, network banner and project tab bar; the parent sidebar, status bar and config-init banner are hidden so the child frame fills the shell body. Embedded project children render a compact header variant without the brand mark or duplicated desktop window controls, while keeping the sidebar toggle, title and actions.
- **Desktop tray/title sync**: the desktop wrapper now mirrors the parent WebUI shell state from the browser into Go (`internal/desktop/app.go`, `desktop/tray*.go`). The tray menu lists the currently open project workspace tabs and focuses a tab on click; the desktop window title follows the active tab title. The close button now closes to tray when a tray icon is available, so child workspaces stay alive until the user chooses Quit.
- **Desktop project open warning**: `internal/api/handlers_projects_desktop.go` keeps spawning `pando desktop --cwd <path>` even when a project web child already exists, but now returns `warning: "web_child_running"`; `web-ui/packages/pando-client/src/stores/projectStore.ts` turns that into a warning toast with i18n in all 7 locales.
- **TUI / CLI instance surfaces**: `internal/tui/components/instances/view.go` now shows `web :<port>` and `child <parent>` metadata for registry entries carrying `web_port` / `parent_instance_id`; `cmd/ipc.go` prints the same in the instance list columns.
- **Serve child startup output**: `cmd/serve.go` now prints when the server is running as a project child, including the project name and public base path.

## Why
- The live run showed double chrome: both the parent shell and the embedded child UI rendered full navigation/status surfaces at once.
- Desktop users needed tray visibility for workspace tabs plus clearer behavior when a project already had a running child workspace.
- TUI/CLI instance listings needed the new registry fields surfaced so child web instances are identifiable outside the browser shell.

## Verification
- WebUI automated: workspace-route shell reduction test, embedded child header test, existing build/lint/typecheck/test suite.
- Go automated: API handler warning test, desktop app shell-state tests, TUI rendering test, IPC formatting test, touched-package build/test/vet.
- Manual desktop/webview matrix below remains **not verified** from this environment.

## Manual check matrix
| Platform | Nested same-origin iframe | PTY websocket inside frame | Downloads from frame | Drag region above frame | Tray entries | Status |
|---|---|---|---|---|---|---|
| Linux WebKitGTK | not verified | not verified | not verified | not verified | not verified | not verified |
| macOS WKWebView | not verified | not verified | not verified | not verified | not verified | not verified |
| Windows WebView2 | not verified | not verified | not verified | not verified | not verified | not verified |
