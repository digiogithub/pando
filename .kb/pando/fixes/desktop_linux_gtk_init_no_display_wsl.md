---
created_at: 2026-10-02T12:03:44.032123647Z
updated_at: 2026-10-02T12:03:44.032123647Z
tags:
    - fix
    - desktop
    - wails
    - linux
    - wsl
    - gtk
---
# Fix: `pando desktop` panics with "failed to init GTK" (WSL / no display) (2026-10-02)

Related: [[desktop_linux_missing_libs_wails_2_16]], [[desktop_frameless_titlebar_tray]].

## Report
`pando desktop` v1.2.6 on Linux inside WSL: `panic: failed to init GTK` from Wails `internal/frontend/desktop/linux/frontend.go:182`, followed by `desktop process exited: exit status 2` and the cobra usage text.

## Root cause
- Wails panics when `gtk_init_check` returns false, i.e. GTK cannot open any display. The libraries are fine (the loader passed).
- Wails v2.16.0 forces `GDK_BACKEND=x11` when `GDK_BACKEND` is unset and `XDG_SESSION_TYPE` is unset, `unspecified` or `x11`. With `XDG_SESSION_TYPE` unset (usual under WSL) GTK is tied to X11 and needs a working `DISPLAY`, even when a Wayland socket exists.
- Likely reporter environments (not confirmed): no WSLg (WSL1, old Windows 10, `guiApplications=false`), `DISPLAY` dropped by ssh/sudo/tmux, or a broken `/tmp/.X11-unix`.

## Changes
- `internal/desktop/displaycheck.go` (new): `NoDisplayError`, `displayEnv`, `noDisplayHelp`, local `isWSL`/`isWSLProcVersion` (not reusing `internal/sandbox.IsWSL` because the desktop wrapper imports `internal/desktop` and must not pull in the config stack).
  - `displayEnv`: neither `DISPLAY` nor `WAYLAND_DISPLAY` set -> no display; only `WAYLAND_DISPLAY` set -> pass `GDK_BACKEND=wayland` to the wrapper; an explicit `GDK_BACKEND` is always respected (no pre-check, no override).
- `internal/desktop/launcher.go`: `runDesktop` and `startDesktop` run the pre-check on Linux and return `NoDisplayError` before starting the wrapper; otherwise set `cmd.Env`.
- `internal/desktop/libcheck.go`: `diagnoseLaunchFailure` maps stderr containing `failed to init GTK` to `NoDisplayError` (covers a set but unusable `DISPLAY`).
- `cmd/desktop.go`: `cmd.SilenceUsage = true` at the start of `runDesktopMode`; WSL section in the long help.
- Tests: `internal/desktop/displaycheck_test.go` (new); `libcheck_test.go` sets `DISPLAY` so the pre-check does not pre-empt the loader test on headless CI.

## Verification
- `go build ./...`, `go vet ./internal/desktop ./cmd`, `go test ./internal/desktop` OK.
- Real wrapper (`go build -tags webkit2_41,production,desktop ./desktop`) on COSMIC Wayland:
  - no `DISPLAY`/`WAYLAND_DISPLAY`: `panic: failed to init GTK` (reproduces the report).
  - `WAYLAND_DISPLAY` only, `XDG_SESSION_TYPE` unset: same panic (Wails forces x11).
  - same plus `GDK_BACKEND=wayland`: window starts.
- Not verified on a real WSL machine.
