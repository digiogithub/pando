---
created_at: 2026-10-27T19:51:39.910046228Z
updated_at: 2026-10-27T21:33:41.634899045Z
tags:
    - plan
    - desktop
    - mygo
    - native-ui
---
# Plan: native desktop client on MyGo (replaces Wails)

Date: 2026-10-27. Status: **P0-P5 implemented** in jj workspace `native-desktop` (`/www/MCP/Pando/pando-native-desktop`), 3 local commits on main v1.4.6: P0 foundation, P1-P4 areas, integration + P5 Wails removal. Not merged to main, not pushed, release pipeline not run. Analysis: [[native_desktop_mygo_replace_wails]].

## Result
- `internal/nativeui`: `client` (REST+SSE, reattach, derived clients for child projects), `kit` (theme tokens, icons, components, WebPane), `shell` (window, menus, tray, close-to-tray, notifications, single instance, Dock reopen), `app` (router, layout, overlays, prefs) + areas chat, simple-chat, settings (23 sections), editor, terminal (libghostty-vt), agentvcs, orchestrator, logs, evaluator, instances, projects (native workspace tabs), setup, design (webview preview), extensions (webview host page `GET /api/v1/extensions/host`). ~35k LOC Go.
- `pando desktop` runs the native UI on the main goroutine; `--webview`/Wails removed (desktop/, internal/desktop launcher+embed, wailsapp deps, Makefile desktop-* targets, desktop-build.yml, Wails CI steps, Wails-only web-ui code). `internal/desktop` keeps workdir/SpawnInstance.
- Go 1.27.3 required (`GOTOOLCHAIN=go1.27.3`), mygo v0.7.1 pinned.
- macOS `Pando.app` ships signed `libghostty-vt.dylib` (`scripts/fetch-ghostty-vt`); other targets download it on first terminal use.
- Web fix: orchestrator create-task now sends `prompt`.

## Verification
go build/vet ./... clean; go test -race internal/nativeui/... pass; cmd/api/config tests pass; cross vet darwin/windows CGO_ENABLED=0; web tsc + vitest 133 pass; real app on Linux Wayland/X11 with screenshots of every area; Mac mini build + run (locked screen captures).

## Open decisions / risks
- Merge to main + release (pipeline untested: macOS signing/notarization with dylib).
- Bare macOS CLI terminal: hardened runtime likely refuses downloaded dylib; option `com.apple.security.cs.disable-library-validation` (security trade-off, not applied).
- Manual checks pending: real IME dead keys, tray/hide-to-tray on real desktops, Windows runtime.
- Gaps: LSP over HTTP missing, React extension panels browser-only, some English-only texts, tab reorder.
