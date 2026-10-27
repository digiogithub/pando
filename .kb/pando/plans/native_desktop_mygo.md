---
created_at: 2026-10-27T19:51:39.176145893Z
updated_at: 2026-10-27T19:51:39.176145893Z
tags:
    - plan
    - desktop
    - mygo
    - native-ui
---
# Plan: native desktop client on MyGo (replaces Wails)

Date: 2026-10-27. Status: in progress. Analysis: [[native_desktop_mygo_replace_wails]].
Workspace: jj workspace `native-desktop` at `/www/MCP/Pando/pando-native-desktop` (parent: main v1.4.6). Do not touch `/www/MCP/Pando/pando` from this work. No commits/pushes by agents; jj snapshots automatically. Never `git stash`.

## Toolchain
`go.mod` -> `go 1.27.3` (MyGo requirement). Run with `GOTOOLCHAIN=go1.27.3 ~/.g/go/bin/go`. Dependency `github.com/egoist/mygo v0.7.1` pinned exactly. Embedded build artefacts need stubs: `make embed-stubs`.

## Architecture
- `internal/nativeui/client` — Go Pando API client (REST + SSE): token (`GET /api/v1/token`, `X-Pando-Token`, `X-Pando-Client: desktop`), local CA TLS, SSE parser, `POST /api/v1/chat/stream` with run_seq reattach (`/sessions/{id}/stream`), event streams (`/sessions/events`, `/notifications/stream`, `/config/events`, `/projects/events`), typed DTOs mirroring `web-ui/packages/pando-client/src/types`. Protocol lessons ported from `pando-assistant/crates/pando-client`. No dependency on mygo.
- `internal/nativeui/kit` — Pando component library + theme (web design tokens light/dark ported to `ui.Theme`), icons (embedded lucide SVG subset), components built on MyGo bases. Only `kit`, `app/...` and `shell` import MyGo.
- `internal/nativeui/app/...` — state + views per area: `chat`, `settings`, `files`, `terminal`, `agentvcs`, `orchestrator`, `logs`, `evaluator`, `instances`, `projects`, `setup`, `design`, `extensions`; router/layout in `app`.
- `internal/nativeui/shell` — `mygo.App` bootstrap, windows, native menus (Edit roles), tray, notifications, Dock reopen, minimise/close-to-tray (`Desktop.TrayOnMinimizeClose`), single instance.
- `cmd/desktop.go` — `pando desktop` starts the API server as today, then runs the native UI on the main goroutine.
- i18n: web-ui locale JSON copied into `internal/nativeui/i18n/locales` (test keeps them in sync).
- Web-only features (extension ES-module panels, design preview iframe) use MyGo embedded webview.

## Phases
- P0 foundation + spike: deps, client, kit theme + base components, shell window, chat MVP streaming against real server; measurements (1000 msgs, IME, Linux X11/Wayland; darwin/windows compile with CGO_ENABLED=0 for nativeui packages; Mac mini via `ssh mac-mini-de-digio`).
- P1 chat parity (tool cards, diffs, permissions/questions, plan/goal/todos, model picker, persona, slash + @, attachments, steering/cancel, sessions list, simple chat) + platform shell (tray, menus, notifications, dock, close-to-tray).
- P2 settings: 23 sections, schema-driven where possible.
- P3 files tree + code editor, terminal (PTY WebSocket), agentvcs/snapshots, orchestrator + cron, logs, evaluator, instances, projects/workspace tabs, setup wizard.
- P4 design studio + extensions via embedded webview.
- P5 remove Wails: `desktop/`, wails embed/launcher in `internal/desktop`, Makefile desktop-* targets, Wails-only web-ui code, CI workflows (desktop-build/release/build-matrix) adapted.

## Stop rule
Only stop for a blocker needing a drastic change to Pando core.
