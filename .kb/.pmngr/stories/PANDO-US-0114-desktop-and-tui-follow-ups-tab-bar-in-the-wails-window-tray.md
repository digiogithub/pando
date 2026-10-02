---
id: PANDO-US-0114
type: story
title: "Desktop and TUI follow-ups: tab bar in the Wails window, tray entries, TUI instances browser shows web children"
status: done
priority: medium
parent: PANDO-EP-0019
author: mcp
labels: [projects, desktop, tui]
estimate: 3
created: 2026-10-01T19:24:43Z
updated: 2026-10-02T09:07:33Z
closed: 2026-10-02T09:07:33Z
---

## Description

- Desktop (`desktop/`, Wails v2, frameless): confirm nested iframe + WebSocket + downloads inside the webview on Linux (WebKitGTK), macOS (WKWebView) and Windows (WebView2); tray menu lists open project tabs (focus on click); closing the window to tray keeps children alive; Quit stops them (`Manager.Shutdown`). Main window title reflects the active tab.
- `handlers_projects_desktop.go`: `open-desktop` of a project that already has a web child should open a window pointing at the **child's** URL is not possible (loopback HTTPS self-signed) — keep spawning `pando desktop --cwd`, but warn when a web child exists (two instances, the desktop one becomes IPC secondary). Decide and document.
- TUI instances browser (`internal/tui/.../instances`): show `[web :port]` and `parent` for entries with `web_port`/`parent_instance_id`; `pando instances` CLI (if present) prints the same.
- `pando serve` log line and `pando app` banner mention when running as a project child.

## Acceptance Criteria

- [ ] Manual check matrix (Linux/macOS/Windows) recorded in the KB doc; screenshots via `cosmic-screenshot --interactive=false` on Linux.
- [ ] TUI renders new registry fields; old entries without them still render.
