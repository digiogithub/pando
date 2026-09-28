---
created_at: 2026-09-28T22:16:33.461320058Z
updated_at: 2026-09-28T22:16:33.461320058Z
tags:
    - feature
    - desktop
    - wails
    - webui
---
# Feature: frameless desktop window, WebUI title bar controls, system tray

## Date
2026-09-29

## What
The Wails v2 desktop wrapper (`desktop/`) is now frameless. The WebUI draws the window chrome: drag region (`--wails-draggable: drag`, already on `.shell-titlebar`), double-click to maximise, and three buttons (minimise to tray, maximise/restore, close). A system tray icon (Linux SNI over D-Bus, Windows notification area) offers left-click restore plus a menu: Show Pando, Settings, Quit.

## Key discovery: the Wails runtime was never present on the real UI
`OnDomReady` navigates the webview from the embedded loading page to the Pando server origin (`http://localhost:PORT`). Wails only injects `/wails/ipc.js` + `/wails/runtime.js` into pages served by its own asset server, and every JS->Go message (bindings, `drag`, `runtime:ready`) is origin-checked against the start URL (`wails://wails`) + `BindingsAllowedOrigins`. Result before this change: on the Pando page `window.go` / `window.runtime` did not exist and messages would have been blocked — so `SetWindowFocused`, `OpenInBrowser`, `SaveDownload`, `WindowSetBackgroundColour` and drag regions were all dead in the desktop shell. `CSSDragProperty` was also the template leftover `widows`/`1`.

Fix (runtime bridge):
1. Loading page (Wails-served) fetches `/wails/ipc.js` + `/wails/runtime.js` and hands them to Go via binding `RegisterRuntimeBridge` (first registration wins, CompareAndSwap) before navigating.
2. Every later `OnDomReady` inlines that bundle into the Pando page (inlined, not eval'd, so CSP cannot block it), sets `window.__PANDO_DESKTOP_SHELL__ = {frameless:true}`, installs focus/blur tracking and fires `pando:desktop-shell`.
3. `options.App.BindingsAllowedOrigins = originOf(--url)` so the Pando origin may talk to Go.
4. `CSSDragProperty: "--wails-draggable"`, `CSSDragValue: "drag"`.

Note `isDesktop` (`'go' in window` at module load) is still false in the shell because injection happens after load; window-chrome code uses the reactive `useDesktopShell()` instead. auth/desktop-config paths unchanged.

## Wayland native title bar (COSMIC/KDE)
GTK3 on Wayland announces **server-side** decorations for an undecorated window via the KDE server-decoration protocol, so COSMIC/KDE drew a native title bar over the frameless window. `desktop/decorations_linux.go` (cgo, gtk+-3.0) queues `gdk_wayland_window_announce_csd` on every undecorated Wayland toplevel via `g_idle_add`, called from `OnDomReady` (window mapped by then). Confirmed by the user on COSMIC.

## Tray
- Library: `github.com/energye/systray` v1.0.3 (getlantern fork without GTK; Linux = D-Bus SNI, pure Go; Windows = win32, pure Go). Run with `systray.Run` in a goroutine with `runtime.LockOSThread` (Windows needs the notify window and message loop on one thread; Linux loop is D-Bus only, does not touch GTK).
- Linux: `trayHostAvailable()` checks `org.kde.StatusNotifierWatcher.IsStatusNotifierHostRegistered`; without a host (stock GNOME) the tray is skipped and minimise falls back to taskbar minimise (never hide a window with no way back).
- macOS: stub (`tray_darwin.go`). energye's darwin code replaces the NSApplication delegate that Wails owns; needs a dedicated NSStatusItem implementation. Minimise goes to the Dock.
- Menu handlers run `go ...` to leave the tray loop (Quit -> OnShutdown -> tray.Stop must not run on the loop it stops).
- Icons: `desktop/tray/icon.png` (64px, from build/appicon.png) and `desktop/tray/icon.ico`.

## Files
- `desktop/main.go`: Frameless, BindingsAllowedOrigins, drag CSS props, OnStartup/OnShutdown start/stop tray, OnDomReady calls `suppressServerDecorations()`; `originOf()`.
- `desktop/tray.go`, `tray_systray.go` (!darwin), `tray_darwin.go`, `tray_host_linux.go`, `tray_host_other.go`, `decorations_linux.go`, `decorations_other.go`, `main_test.go`.
- `internal/desktop/app.go`: `domReadyScript()`, `RegisterRuntimeBridge`, `ShowWindow`, `MinimiseToTray`, `TrayAvailable`/`SetTrayAvailable`, `OpenSettings` (+ `navigateInApp` -> `pando:desktop-navigate` event, full navigation fallback), `QuitApp`; `app_test.go`.
- WebUI: `src/services/desktopWindow.ts` (useDesktopShell, minimise/maximise/close, useWindowMaximised, useTrayAvailable, useDesktopNavigation, onTitleBarDoubleClick, useProvidesWindowTitleBar/useHasWindowTitleBar), `src/components/layout/DesktopWindowControls.tsx` (controls + `DesktopFrameBar` standalone 32px bar for simple chat/editor/splash/login), `src/styles/desktop-window.css`, Header (controls + dblclick), MainLayout (`useProvidesWindowTitleBar`), App.tsx (`DesktopFrameBar`, `DesktopNavigationBridge`), i18n `shell.minimiseToTray|minimise|restoreWindow|maximiseWindow|closeWindow|windowControls` in 7 locales.
- go.mod: `github.com/energye/systray v1.0.3`.

## Verification
- `go test ./internal/desktop/ ./desktop/` pass; `GOOS=windows go vet ./desktop` ok; `tsc -p tsconfig.app.json` + eslint clean.
- Live on Linux/COSMIC Wayland: wrapper built with `-tags desktop,production,webkit2_41` against `pando serve` + vite dev. Instrumented build logged: runtime captured (412 + 15309 bytes), bridge injected on Pando origin, `TrayAvailable` binding calls from the Pando origin reached Go, focus events flowed. SNI item registered (`Title "Pando"`), dbusmenu layout Show Pando / Settings / Quit, Settings triggered via `com.canonical.dbusmenu.Event`. User confirmed custom title bar visible and native bar gone after the CSD fix.
- Not verified: Windows and macOS runtime (no machines); macOS tray not implemented.

Related: [[project_desktop_wails_plan]], [[fix_reindex_macos_clipboard_model_confirm]], [[webui_redesign_p3_app_shell]]
