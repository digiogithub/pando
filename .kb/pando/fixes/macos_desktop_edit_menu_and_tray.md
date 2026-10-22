---
created_at: 2026-10-22T13:07:47.538332922Z
updated_at: 2026-10-22T13:07:59.91045435Z
tags:
    - fix
    - desktop
    - macos
    - wails
    - tray
---
# macOS desktop: Edit menu lost (Cmd+C/V dead) + menu bar tray icon (2026-10-22)

## Symptom
Wails desktop on macOS: no Edit menu, so Cmd+C / Cmd+V do not work in the webview; no icon next to the clock (Linux has a tray icon).

## Root cause
1. `desktop/main.go` passed `Menu: appMenu()` (AppMenu + EditMenu + WindowMenu, added by the fix in [[pando/fixes/reindex_endpoint_macos_clipboard_model_confirm.md]]), but `internal/desktop/app.go` `App.Startup` called `runtime.MenuSetApplicationMenu(ctx, a.buildMenu())`, replacing it at runtime with a menu that only had the "Pando" submenu. On macOS WKWebView only receives the clipboard shortcuts through Edit-menu roles, so they died.
2. The macOS tray was always a stub (`tray_darwin.go` called `SetTrayAvailable(false)`): energye/systray's darwin backend replaces the NSApplication delegate owned by Wails.

## Change
- `internal/desktop/app.go`: new `App.Menu()` = `buildMenu()` + (darwin only) `menu.EditMenu()` + `menu.WindowMenu()`. `Startup` no longer sets the application menu. Linux/Windows keep exactly the previous Pando menu.
- `desktop/main.go`: `Menu: app.Menu()`. Deleted `desktop/menu_darwin.go`, `desktop/menu_other.go`.
- `desktop/tray_darwin.go` + new `desktop/tray_darwin.m` (cgo, `-x objective-c -fobjc-arc`, Cocoa): `NSStatusItem` created via `dispatch_async(main_queue)` on Wails' run loop, icon `tray/icon.png` at 18pt. Menu (Show Pando, Settings, Project workspaces list with "(current)", Quit) serialised from Go as `kind\tid\ttitle` lines; clicks call exported `pandoTrayItemClicked(tag)` which activates the app and runs the action in a goroutine (Wails runtime calls must not run on the main thread). `pandoTrayReady` sets `SetTrayAvailable(true)` so minimise hides to the menu bar. Rebuilt on `UpdateShellState`.
- "Simple Mode" shortcut moved from Cmd/Ctrl+M to Cmd/Ctrl+Shift+M (all platforms) so Cmd+M stays the standard macOS Minimize from the Window menu.
- `internal/desktop/app_test.go`: `TestMenuKeepsEditRolesOnMacOS`.

## Verification
- Linux: `go test ./internal/desktop ./desktop`, `GOOS=windows go vet` OK.
- macOS 27 (Mac mini, clean clone of 3b22d0d + patch in /tmp): `go vet` OK, `go build -tags desktop,production ./desktop` OK (plain `go build` needs `CGO_LDFLAGS=-framework UniformTypeIdentifiers` for Wails' UTType; `wails build` adds it). Binary launched and ran Startup without crash; menu/status item not inspected (System Events AppleEvents time out over ssh, TCC). Needs manual check on a Mac.
