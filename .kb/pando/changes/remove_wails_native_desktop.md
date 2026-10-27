---
created_at: 2026-10-27T21:30:00Z
updated_at: 2026-10-27T21:30:00Z
tags:
    - change
    - desktop
    - nativeui
    - ci
---
# Native desktop P5: Wails removed (2026-10-27)

Phase P5 of `.kb/pando/plans/native_desktop_mygo.md`. `pando desktop` now runs only the native MyGo UI (`internal/nativeui`). The Wails WebView wrapper and everything that built, embedded, signed or packaged it are gone.

## Go
- Deleted `desktop/`: the Wails main, tray, platform/decorations, frontend, `wails.json` and `build/`.
- Deleted the Wails launcher, embed and bindings from `internal/desktop`:
  - `app.go`, `notifications.go` (beeep SSE notifications), `launcher.go`, `embed*.go` and `bin/`;
  - `displaycheck.go` and `libcheck.go` (diagnostics for the wrapper subprocess), with their tests.
- Kept `internal/desktop/workdir.go`, which is toolkit-free: `DefaultWorkingDir`, `LaunchedFromApp`, `ImportLoginShellPath` and `SpawnInstance`. Its callers are `cmd/desktop.go` and `internal/api/handlers_projects_desktop.go`. The package now has a package doc comment.
- `cmd/desktop.go`:
  - The hidden `--webview` flag and its whole code path are gone, including the design auto-open windows.
  - The help text now gives the Linux runtime requirements: GTK3 always; WebKitGTK only for the design preview and extension panels, loaded at runtime with no headers needed. It also describes the terminal library.
- `go.mod`: `go mod tidy` dropped `wailsapp/*`, `energye/systray`, `gen2brain/beeep` and their indirect dependencies. Go stays at 1.27.3.
- Comment and test cleanups:
  - comments in `internal/config/config.go` (`DesktopConfig`), `internal/instanceregistry/entry.go` and `internal/design/preview/preview.go`;
  - the preview test origin.

## Build, CI and scripts
- `Makefile`:
  - removed the targets `desktop-deps`, `desktop-ui`, `desktop-build`, `desktop-dev`, `desktop-embed`, `desktop-package` and `desktop-clean`;
  - removed the `WAILS_*` variables and the wrapper signing/notarization variables (`MACOS_SIGN_IDENTITY`, `MACOS_ENTITLEMENTS`, `NOTARY_PROFILE`, `MACOS_NOTARIZE_WRAPPER`);
  - `embed-stubs` now only stubs `internal/api/webui/dist`;
  - `MACOS_CODESIGN_WRAPPER` is kept.
- Deleted:
  - `scripts/notarize-desktop-wrapper`, `scripts/embed_desktop_artifact.py`;
  - `nfpm.yaml` (it packaged the Wails `pando-desktop` binary);
  - `.github/workflows/desktop-build.yml`;
  - the Wails entries in `.gitignore`.
- `release.yml`:
  - no wails install and no `make desktop-embed` on either the Linux or the macOS job;
  - Linux apt installs only `upx-ucl zip`, with no GTK/WebKit `-dev` packages.
  - Go still comes from `go-version-file: go.mod`.
- `build-matrix.yml`: no GTK/WebKit install; `go vet` runs without the `webkit2_41` tag.
- New `scripts/fetch-ghostty-vt <goos>-<goarch> <dir>`: downloads libghostty-vt using the URL and SHA-256 from MyGo's `plugins/terminal/mygo-plugin.json`, read from the module version in `go.mod`, and verifies the checksum.
- `scripts/build-macos-app`: no longer builds the wails wrapper. It fetches `libghostty-vt.dylib` into `Pando.app/Contents/MacOS` and signs it (hardened runtime) before signing the bundle.
- `scripts/install.sh`: the runtime dependency step now reads "native desktop", drops the apt `-dev` packages, and renames its function to `ensure_desktop_runtime_dependencies`.
- `scripts/bump-version.sh` and `scripts/brand-sync.sh` no longer touch `desktop/`.

## Terminal native library decision
MyGo looks for libghostty-vt in this order:
1. `$MYGO_GHOSTTY_VT`;
2. the app resources (`Contents/Resources`);
3. the executable's directory;
4. the user cache, downloaded on first use with a SHA-256 check, but only when the app is not packaged.

On macOS, `Pando.app` counts as packaged (the NSBundle path ends in `.app`), so it never downloads the library. The bundle must therefore ship it, which build-macos-app now does.

Linux and Windows zips are unpackaged and download the library on first use; the bare darwin CLI does the same. The help text documents this.

Known gap: the bare darwin CLI is signed with the hardened runtime, so library validation will probably refuse the downloaded dylib, which is not signed by our team. The options are to use Pando.app (the .pkg), or to add `com.apple.security.cs.disable-library-validation`. No decision has been taken yet.

## Web UI
- Removed the Wails-only code:
  - `wailsjs/` and `.env.desktop`;
  - the `services/desktop*.ts` files and `wailsBindings.ts`;
  - `DesktopWindowControls`/`DesktopFrameBar` and `desktop-window.css`;
  - `pando-client` `services/host.ts`, `initDesktopMode` and `openProjectDesktop`;
  - the `build:desktop` script;
  - the "open project in new window" actions.
- New `src/utils/openExternal.ts` (`window.open`).
- The "Minimise and close to tray" General row is kept. The `/api/v1/projects/{id}/open-desktop` endpoint is kept because the native client uses it.

## Verification
- `go build ./...`, `go vet ./...` and `go test ./cmd/ ./internal/api/ ./internal/desktop/... ./internal/design/preview/ ./internal/config/` all pass.
- `make help` and `make build` pass; `make build` includes the bun embedded web-ui build.
- `pando desktop --help` shows the new text, and `--webview` is now an unknown flag.
- `pando desktop` launched with an isolated HOME and `PANDO_DESKTOP_MULTI=1`, and a native capture was taken.
- Web UI: `tsc --noEmit`, vitest (27 files, 130 tests) and eslint on the changed files all pass.
- `scripts/fetch-ghostty-vt darwin-arm64` downloaded and verified the arm64 Mach-O dylib.
- The workflow YAML parses; actionlint is not installed.
- The release pipeline was not run; macOS signing and notarization of the bundled dylib are untested.
