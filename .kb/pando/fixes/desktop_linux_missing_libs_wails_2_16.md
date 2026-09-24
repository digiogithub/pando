---
created_at: 2026-09-24T20:51:44.01579241Z
updated_at: 2026-09-24T20:51:44.01579241Z
tags:
    - fix
    - desktop
    - wails
    - linux
    - webkitgtk
---
# Desktop (Linux): missing-library help, Wails v2.16.0, opaque window background (2026-09-24)

## Context
- Report: `pando desktop` (release v0.720.2) shows an empty/blank window; old distros fail loading GTK/WebKit libs.
- Diagnosis of the blank window on the reporter's machine (Pop!_OS 24.04, WebKitGTK 2.52.6, heavy load, user-modified GPU rendering setup): a minimal Python WebKitGTK 4.1 page with no Pando involved was ALSO blank (`load-changed` never fired, `WebKitWebProcess` spinning at 100% CPU). So the blank window is a system WebKitGTK/GPU issue, not Pando code and not the host command sandbox (commit 4e3a1492a doesn't touch the launcher). Parked by the user.

## Wails version review
- Was on v2.12.0. Latest v2 = v2.16.0 (2026-09-14). v2.13–v2.16 contain no WebKitGTK rendering fixes (only a Calloc leak fix on macOS/Linux, Windows WebView2 bootstrapper fix, macOS autoplay option).
- v3 is still beta (v3.0.0-beta.25, nightly). Default Linux backend is GTK4 + webkitgtk-6.0 (GTK 4.14+ baseline) which drops Ubuntu 22.04; build tag `gtk3` keeps GTK3 + webkit2gtk-4.1. Full API rewrite of `desktop/`. Decision: do NOT migrate until 3.0 stable; use `gtk3` tag if/when migrating to keep 22.04.
- v3 beta.5 fixed "explicit opaque background color for Linux WebKit windows before URL load" — same class of bug as ours (below).

## Changes
1. Wails bumped to v2.16.0: `go.mod`/`go.sum` (also golang.org/x/text v0.39.0), `WAILS_VERSION` in `.github/workflows/release.yml`; CLI pinned `@v2.16.0` instead of `@latest` in `Makefile` (`desktop-deps`) and `.github/workflows/desktop-build.yml` (previously lib and CLI versions could drift).
2. `desktop/main.go`: `BackgroundColour` alpha `A: 1` -> `A: 255` (range is 0-255; A:1 made the window almost fully transparent before the first paint).
3. New `internal/desktop/libcheck.go`:
   - `runDesktop` (launcher.go) tees wrapper stderr into a 16 KiB `boundedBuffer`; on non-zero exit on Linux calls `diagnoseLaunchFailure`.
   - `parseLoaderErrors` matches ld.so `error while loading shared libraries: X: cannot open shared object file` and ``version `GLIBC_2.xx' not found``.
   - `MissingLibrariesError{Libraries, Glibc, Help, Err}` (unwraps to the exec error); message includes distro-specific install command from `/etc/os-release` (`parseOSRelease`, `installCommand`): Ubuntu(-like) 22.04 `sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0`; 24.04+/26.04 `... libgtk-3-0t64`; Debian apt; Fedora dnf `webkit2gtk4.1 gtk3`; Arch pacman; openSUSE zypper. Always lists the Ubuntu LTS table and suggests `pando app`.
   - Too-old systems (glibc error or Ubuntu < 22.04): says requires glibc 2.34+/WebKitGTK 4.1 (Ubuntu 22.04+/Debian 12+), suggests upgrade or `pando app`.
   - Pre-flight `ldd` was rejected: the release wrapper is UPX-packed so ldd reports "not a dynamic executable"; stderr parsing works regardless.
4. `cmd/desktop.go` long help: Linux requirements per Ubuntu LTS + blank-window workarounds (`WEBKIT_DISABLE_DMABUF_RENDERER=1`, `WEBKIT_DISABLE_COMPOSITING_MODE=1`, or `pando app`).
5. `scripts/install-linux.sh`: `warn_ubuntu_runtime_hint` printed with the "ensure GTK/WebKitGTK" warnings.

## Verification
- `go build ./...` OK; `go build -tags webkit2_41,production,desktop ./desktop` OK with Wails v2.16.0.
- `go test ./internal/desktop` (new `libcheck_test.go`: parser, install commands incl. Pop!_OS 24.04 via ID_LIKE, too-old help, error unwrap, bounded buffer, end-to-end `runDesktop` with a fake wrapper script emitting a loader error + exit 127) OK.
- `go test ./internal/api ./cmd` OK; `bash -n scripts/install-linux.sh` OK.
- Go unit tests live next to the package (Go convention); tests/ holds Python tests.
- Not verified: blank-window fix on the reporter machine (system WebKitGTK itself fails there).
