---
created_at: 2026-10-27T14:56:30.300975227Z
updated_at: 2026-10-27T14:56:30.300975227Z
tags:
    - feature
    - desktop
    - wails
    - macos
    - webui
---
# Desktop: optional minimise/close to tray, native macOS title bar, Dock reopen

## Date
2026-10-27

## Reports
- macOS: depending on the macOS release the window showed the native or the web title bar (frameless NSWindow renders inconsistently).
- macOS: minimising hid the window to the tray (tray was live, so `MinimiseToTray` hid it) but the menu bar icon was not visible, and clicking the Dock icon did not bring the window back (Wails v2 AppDelegate has no `applicationShouldHandleReopen`).
- Request: make "minimise and close go to tray" an option in Settings > General; default behaviour otherwise.

## Changes
### Setting
- `internal/config/config.go`: `Config.Desktop DesktopConfig` (`toml:"Desktop"`), field `TrayOnMinimizeClose bool`; `UpdateDesktopTrayOnMinimizeClose` persists to the GLOBAL config (`updateGlobalCfgFile`) — app preference, not per project.
- `internal/api/handlers_settings.go`: `desktop_tray_on_minimize_close` in GET response and PUT request.
- WebUI: `SettingsConfig.desktop_tray_on_minimize_close` (types + store default false), switch in `GeneralSettings.tsx` after Nerd Fonts, i18n keys `settings.general.desktopTrayOnMinimizeClose[Description]` in en/es/pt/fr/de/ja/zh.

### Desktop Go (`internal/desktop/app.go`)
- `frameless` = `GOOS != "darwin"`, exposed via `Frameless()`; DOM-ready script sets `window.__PANDO_DESKTOP_SHELL__ = { frameless: <bool> }`.
- `trayOnMinimizeClose` (pushed by the WebUI through binding `SetTrayOnMinimizeClose`), `useTray()` = setting && tray live; binding `TrayOnMinimizeClose()`.
- `MinimiseToTray`: hide only when `useTray()`, else normal minimise. `CloseWindow`: hide when `useTray()`, else `QuitApp`.
- `BeforeClose` (Wails `OnBeforeClose`): every close (native close button, Alt+F4, Cmd+Q, Dock Quit) lands there; hides + vetoes only when `useTray()` and not `quitting`. `QuitApp` and `MarkQuitting` set `quitting`; menu Quit now calls `QuitApp`.

### Desktop main (`desktop/`)
- `main.go`: `Frameless: app.Frameless()`, `Mac: TitleBarDefault()`, `OnBeforeClose: app.BeforeClose`, `installPlatformHooks(app)` in OnStartup.
- `platform_darwin.go/.m`: adds to Wails' NSApp delegate class at runtime: `applicationShouldHandleReopen:hasVisibleWindows:` (unhide, deminiaturize, makeKeyAndOrderFront, activate) and wraps `applicationShouldTerminate:` to call `pandoAppWillTerminate` -> `MarkQuitting` before Wails' "Q" message. `platform_other.go` no-op.
- `tray_darwin.m`: `statusItem.visible = YES`.

### WebUI shell (`web-ui/src/services/desktopWindow.ts`)
- `useDesktopShell()` = shell present; new `useDesktopFrameless()`; `DesktopWindowControls`/`DesktopFrameBar` render only when frameless (not on macOS); header double-click maximise only when frameless.
- `useTrayAvailable` replaced by `useTrayOnMinimizeClose` (labels). `useDesktopTrayPreference` (called from `Header`, top-level only) reads `/api/v1/settings` once and follows `settingsStore` `original` changes, pushing the value to Go.

## Verification
- `go test ./internal/desktop/ ./internal/config/ ./internal/api/` pass; new tests `TestTrayOnMinimizeCloseNeedsSettingAndTray`, `TestBeforeCloseQuitsUnlessTrayBehaviourIsOn`, `TestDomReadyScriptFramelessFlagFollowsPlatform`.
- Mac mini (macOS 27.0.1) over `ssh mac-mini-de-digio`: `go vet ./desktop/ ./internal/desktop/`, `go test ./internal/desktop/`, `go build ./desktop/` OK (repo restored to origin/main after).
- WebUI: tsc, eslint, vitest (131 tests) pass.
- NOT verified at runtime on macOS GUI (TCC/locked screen) nor Windows. Menu bar icon invisibility root cause not confirmed (possibly notch overflow / macOS menu bar item settings); with the default off, minimise now goes to the Dock so it no longer strands the window.

Related: [[desktop_frameless_titlebar_tray]], [[macos_desktop_edit_menu_and_tray]]
