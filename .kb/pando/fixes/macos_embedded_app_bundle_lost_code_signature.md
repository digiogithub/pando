---
created_at: 2026-10-23T08:05:19.017501936Z
updated_at: 2026-10-23T08:05:19.017501936Z
tags:
    - fix
    - desktop
    - macos
    - signing
    - embed
---
# macOS `pando desktop`: embedded Pando.app lost its code signature (2026-10-23)

## Symptom
v1.4.2 on macOS (`pando desktop` from the standalone CLI): Cmd+C / Cmd+V still dead and no menu bar icon, even though v1.4.2 ships the Edit menu and the NSStatusItem tray ([[pando/fixes/macos_desktop_edit_menu_and_tray.md]]). The terminal prints `sandbox_extension_issue_file_to_process failed for .../T/pando-desktop-*/Pando.app: 1 (Operation not permitted)`. That line turned out to be harmless noise: it also appears on a working launch.

## Diagnosis (Mac mini, macOS 27.0.1, released v1.4.2 binary)
- Better Stack: only server-side logs (the Wails child does not send telemetry); nothing desktop-specific.
- A locally built wrapper (same source) registered with LaunchServices, became frontmost, and an in-process synthetic Cmd+C copied a textarea into NSPasteboard (target `WailsWebView`). So the menu and tray code were fine.
- The released `pando-desktop` extracted to `$TMPDIR/pando-desktop-*/Pando.app` was **not registered** in LaunchServices (`lsappinfo list`), started no WebKit helpers, sat in `_dyld_start` (per `sample`) and was eventually `signal: killed`.
- `codesign --verify --deep --strict` on the extracted bundle: `invalid resource directory (directory or signature have been modified)`. `Contents/_CodeSignature/` was missing; the release binary contains no `_CodeSignature/CodeResources` string.

## Root cause
`internal/desktop/embed_binary_darwin.go` used `//go:embed bin/.keep bin/Pando.app/**`. Without the `all:` prefix, go:embed skips every file and directory whose name starts with `_` or `.`, so `Contents/_CodeSignature` was never embedded. The extracted bundle's resource seal was broken, and macOS would not register or activate it. Without activation there is no menu bar (so no Cmd+C/V) and no status item. Present since 2026-05-19 (`cc20284c8`).

## Change
- `internal/desktop/embed_binary_darwin.go`: `//go:embed bin/.keep all:bin/Pando.app`.
- New `internal/desktop/embed_binary_darwin_test.go`: when a real bundle is embedded, `Contents/_CodeSignature/CodeResources` must be present.

## Verification
Mac mini, clean clone + fix: ad-hoc signed Pando.app embedded via `scripts/embed_desktop_artifact.py`, full `go build .`, `pando desktop` with an isolated HOME. The extracted bundle passes `codesign --verify --deep --strict`, is registered as "Pando" (frontmost) and spawns WebKit GPU/WebContent helpers. Linux: `go build ./...`, `go test ./internal/desktop ./desktop`; darwin `go vet ./internal/desktop`.

## Notes
- The Mac mini wails CLI (`~/go/bin/wails`) is outdated ("package context without types"); CI installs its own.
- `.app`/`.pkg` installs are not affected: they launch the on-disk sibling `pando-desktop` (`siblingDesktopBinary`).
