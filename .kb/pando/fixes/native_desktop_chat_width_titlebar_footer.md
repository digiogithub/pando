---
created_at: 2026-10-27T22:22:29.279905128Z
updated_at: 2026-10-27T22:22:29.279905128Z
tags:
    - fix
    - desktop
    - native-ui
    - mygo
---
# Native desktop: centered chat column, sessions toggle, listen footer, custom title bar

Date: 2026-10-27. Status: implemented, uncommitted in main working copy.

## Changes
- **Chat reading column** (`internal/nativeui/app/chat/view_transcript.go`, `view_panels.go`, `composer.go`, `chat.go`): transcript items, composer, goal/plan banners and modified-files bar centered in a 788px column (740 text + 2x24 gutters, like web `.chat-column`); user bubbles capped at 592px (80%). Virtualized list stays full width (scrollbar at window edge). Applies to chat and simple chat.
- **Sessions pane toggle** (`chat/sessions.go`): `panel-left` button next to "Sessions" collapses the pane; header action reopens it. State in memory (`ch.sessHidden`), not in UI prefs.
- **Footer status bar** (`app/footer.go`, registered via `app.RegisterStatusItem`): port of web ExternalAccessToggle — GET/PUT `/api/v1/config/api-server/external-access`, hidden when endpoint missing, bind fixed by `--host`, or project child; needsCredentials warning opens `/settings/webui-access`; shows Pando version. `kit.IconGlobe`.
- **Custom title bar on Linux/Windows** (`shell/shell.go`, `shell/controller.go`, `app/windowcontrols.go`, `app/layout.go` header, `kit/windowcontrols.go`, `kit/icons/minus.svg`): `shell.CustomFrame()` true when not darwin. Linux `Frameless: true`; Windows `TitleBarHidden` + `HideWindowButtons` (keeps DWM shadow/snap). Native menu only on macOS. Header is drag region (double-click maximise) with minimise / maximise-restore / close (44px buttons, close hover #c42b1c), honouring Desktop.TrayOnMinimizeClose via controller `Minimize/CloseWindow/ToggleMaximize`. Ctrl+Q quits, F11 full screen; "Open in Browser" moved to quick menu. macOS unchanged (native bar + menu).
- Dev aid: `PANDO_NATIVE_SIZE=WxH` initial window size.

## Verification
go build ./..., go vet ./internal/nativeui/... (also darwin/arm64 and windows CGO_ENABLED=0), go test ./internal/nativeui/... pass; new tests chat/layout_test.go, app/footer_test.go, app/windowcontrols_test.go, shell/controller_test.go. COSMIC Wayland screenshot: no compositor title bar, own controls visible; X11 (GDK_BACKEND=x11) starts. Headless 2400px render shows centered column.
Not verified: clicking real window buttons, drag, edge resize (COSMIC tiled the window), Windows runtime, macOS run.
