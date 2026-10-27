# Native desktop P1-shell: tray, close-to-tray, notifications, single instance, overlays

Date: 2026-10-27. Workspace `/www/MCP/Pando/pando-native-desktop` (jj, not committed). Plan [[native_desktop_mygo]], foundation [[native_desktop_p0_foundation]].

## What changed
- `internal/nativeui/shell`: `controller.go` (Controller: tray via `mygo.NewTray` with Show/Settings/project workspaces/Quit, hide-to-tray on close/minimise only when the setting is on and the tray icon is live, `Quitting` flag set in `OnBeforeQuit`, focus tracking, `Notify` via `mygo.NewNotification`, window recreate), `shell.go` (`Options.OnReady/Tray/OnSecondInstance`, `AcquireSingleInstance(key)`, last-window-closed quits on every OS, Dock activate -> Show), `assets/tray.png` (copy of desktop/tray/icon.png).
- `internal/nativeui/app`: `shellstate.go` (Host interface, `Shell` state, registries for overlays, banners, header slots, commands), `chrome.go` (global shortcuts, tab shortcuts, sidebar state), `header.go` (header slots: simple chat, docs, theme, settings; persona slot left to the chat agent), `prefs.go` (apply/save UI prefs, SetTheme/SetLanguage/SetSidebarCollapsed/ToggleSimpleChat, ReportError), `services.go` (StartServices: prefs, tray flag via /config/events, notification relay, window title + tray projects sync per frame). `layout.go`: sidebar state from Shell, banners, overlays, header slots, theme toggle moved to header (embedded roots unchanged). `context.go`: `shell *Shell`.
- `internal/nativeui/app/overlays` (new): quick menu (views, commands, recent, Copilot account commands), model switcher (grouped by provider, Auto first, badges, session-scoped PUT /models/active), config-init banner, update notice.
- `internal/nativeui/client/shell.go` (new): UI prefs, desktop settings, config/notification streams, config init, provider auth commands, `DescribeError/IsAuthError`; `settings.go` VersionStatus gets `ReleaseURL`.
- `internal/api/handlers_ui_prefs.go`: UIPrefs gets `theme`, `themeMode`, `themeAccent`, `sidebarCollapsed` (merge semantics, validation) + test.
- `cmd/desktop.go`: single instance lock keyed by cwd before server/DB start; `--simple` passed to `nativeui.Options.Simple`.
- Kit: icons book-open, circle-arrow-up, command (+ `icons_shell.go` constants).

## Design notes
- Single-instance key is the cwd because `desktop.SpawnInstance` starts `pando desktop --cwd <project>` for per-project windows; a global lock would break them. MyGo derives the lock from the app name, so the name is set to `Pando desktop <cwd>` only around `RequestSingleInstanceLock`. `PANDO_DESKTOP_MULTI=1` bypasses it.
- Tray "open projects" = project tabs of the `ShellExt` (projects area), refreshed each frame only when the list changes.
- Quit semantics: tray off -> closing the window quits on all OS (previously macOS kept the app alive); tray on and live -> hide; explicit quit (tray, menu, Cmd+Q) always quits.
- Notifications: only `agent` and `tool` sources, only while the window is unfocused (as internal/desktop/notifications.go). macOS shows them only for the packaged app.

## Verification
- `go test -race ./internal/nativeui/app ./internal/nativeui/app/overlays ./internal/nativeui/client ./internal/nativeui/shell`, `go test ./internal/api -run UIPrefs`: OK.
- `CGO_ENABLED=0 GOOS=darwin|windows go vet` of shell, app, overlays, client, kit: OK.
