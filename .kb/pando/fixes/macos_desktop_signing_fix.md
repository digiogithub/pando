---
created_at: 2026-06-18T08:11:46.028144033Z
updated_at: 2026-06-18T08:11:46.028144033Z
tags:
    - fix
    - macos
    - build
    - signing
    - desktop
    - wails
    - pkg
    - notarization
    - pando
---
# macOS desktop build & signing fix (2026-06-18)

## Problem

On macOS, launching Pando in desktop mode (`pando desktop`, or opening `Pando.app`)
caused the desktop window process to be killed by Gatekeeper / the hardened runtime
immediately ("el binario en modo desktop es cerrado por OSX directamente").

## Root cause

NOT the universal binary / `lipo` step. The cause was **signing-after-embedding**:

1. `wails build` (via `make desktop-build`) produces the desktop wrapper
   (`desktop/build/bin/pando-desktop`, or `Pando.app` on macOS) **unsigned**
   (Wails ad-hoc at most).
2. `scripts/embed_desktop_artifact.py` copies that **unsigned** wrapper into
   `internal/desktop/bin/` for `go:embed`
   (`internal/desktop/embed_binary_darwin.go` → `DesktopBundle embed.FS`,
   `embed_binary.go` → `DesktopBinary []byte`).
3. `make release-darwin-amd64 / arm64` compiles `pando` with the wrapper already
   embedded as raw bytes, then `codesign-digio` signs **only the OUTER Mach-O**
   (`dist/pando-darwin-*`). Signing the outer binary does NOT sign the embedded
   wrapper bytes — to macOS they are just data inside `__DATA`.
4. At runtime, `internal/desktop/launcher.go` → `Launch()` → `launchAppBundle()`
   extracts the embedded `Pando.app` to a temp dir and `exec`s its Mach-O.
   Because the parent (`pando-bin`) runs with hardened runtime (`-o runtime`)
   and the extracted child has no valid signature, macOS kills the child.

The `lipo`-into-universal + re-sign of the outer binary in `scripts/build-macos-app`
did not help: it only re-signs the outer Mach-O, never the embedded wrapper.

Key principle: **a code signature must be applied to the file at (or before) its
final on-disk location — never embedded as raw bytes after signing the container.**

## Fix (hybrid approach, per user decision)

Two distinct build paths are kept:

### 1. Embedded standalone CLI build (no pkg) — sign-before-embed, no notarization
`make desktop-embed` (Makefile) now signs the wrapper with the Developer ID
Application identity (`MACOS_SIGN_IDENTITY`, default `4749EC5719E91D7ADFE5FDB4CB546057A8CFB9AD`),
`codesign -o runtime --timestamp`, **without notarization**, BEFORE embedding:

- If `desktop/build/bin/Pando.app` exists → `codesign --deep --force -o runtime --timestamp`.
- Elif `desktop/build/bin/pando-desktop` exists → `codesign --force -o runtime --timestamp`.
- Guarded by `uname == Darwin`; failures are warnings (non-fatal on Linux/CI).

The Mach-O signature lives inside the file itself (`LC_CODE_SIGNATURE` in
`__LINKEDIT`), so it survives `go:embed` → byte-for-byte extraction. A wrapper
signed with the same Team ID as the parent passes library validation under the
hardened runtime even without notarization (the freshly written temp file has no
`com.apple.quarantine` xattr, so the Gatekeeper quarantine gate does not trigger).

### 2. Packaged .app / .pkg — per-arch, wrapper as a real signed file
`scripts/build-macos-app` rewritten:

- **Per-architecture**: produces two installers,
  `pando-<VERSION>-darwin-arm64.pkg` and `pando-<VERSION>-darwin-x64.pkg`
  (plus `.pkg.zip` and `Pando-arm64.app` / `Pando-x64.app`).
  The universal `pando-osx` output was removed.
- The desktop wrapper is built per-arch with
  `wails build -platform darwin/<goarch> -clean -o pando-desktop` and shipped as a
  **real file** at `Pando.app/Contents/MacOS/pando-desktop`, next to `pando-bin`.
  It is NO LONGER extracted from the embed at runtime.
- Bundle layout: `Contents/MacOS/{pando-bin, pando-desktop, pando}` where `pando`
  is the CFBundleExecutable shell wrapper (`exec "$SELF/pando-bin" desktop`).
- **Inside-out signing**: `run_codesign -o runtime` on `pando-bin`, then on
  `pando-desktop`, then `codesign --deep -o runtime` on the whole `Pando.app`.
- `.pkg` built with `pkgbuild` (app + `/usr/local/bin/pando` launcher components) +
  `productbuild` + `productsign` (`PKG_SIGN_IDENTITY`, Developer ID Installer) +
  `notarytool` submit/staple (`NOTARY_PROFILE=pando-notary`) per pkg.
- Verification after signing: `codesign --verify --deep --strict`, `spctl`,
  `pkgutil --check-signature`.

### 3. Runtime — internal/desktop/launcher.go
- New `siblingDesktopBinary()`: returns a `pando-desktop` (or `pando-desktop.exe`)
  found next to `os.Executable()` (resolving symlinks). In a packaged install,
  `os.Executable()` is `/Applications/Pando.app/Contents/MacOS/pando-bin`, so the
  sibling is the signed & notarized wrapper.
- New `runDesktop(binPath, pandoURL, simpleMode)` helper centralizes the
  `exec.Command(... --url --simple)` + exit-code handling.
- `Launch()` resolution order:
  1. on-disk sibling wrapper (packaged install) — runs in place, preserving
     signature/notarization (no temp-dir extraction);
  2. macOS embedded `Pando.app` bundle → extract to temp dir;
  3. raw `embedBin` bytes → extract to temp dir.
- `launchAppBundle()` simplified to call `runDesktop()`.

### 4. README.md
- Install section: download the per-arch `.pkg`
  (`pando-<version>-darwin-arm64.pkg` / `-x64.pkg`).
- Signing paragraph rewritten to describe both paths (sign-before-embed for
  standalone, real-file-in-bundle for packaged), inside-out signing and
  notarization.
- `release-osx` task command reordered to `security unlock-keychain` and export
  `KEYSTORE_PASS` **before** `xc build`, because `make desktop-embed` now needs an
  unlocked keychain to sign the embedded wrapper (and `build-macos-app` reads
  `KEYSTORE_PASS` too).

## Files touched
- `internal/desktop/launcher.go`
- `Makefile` (new `MACOS_SIGN_IDENTITY` var; signing step in `desktop-embed`)
- `scripts/build-macos-app` (full rewrite: per-arch, wrapper as real file)
- `README.md` (install + signing paragraph + `release-osx` task order)

## Verification status
- Compiles on the Linux dev box: `go build ./internal/desktop/`, `go build ./cmd/`,
  `bash -n scripts/build-macos-app` all pass; no lingering `pando-osx` references.
- Actual codesign / productsign / notarytool can only be validated on macOS
  (mac-mini-de-digio). Watch: confirm `wails build -platform darwin/amd64` builds
  the x64 wrapper correctly on Apple Silicon; if cross-arch is flaky, fall back to
  building a universal wrapper once and `lipo -thin <arch>` per bundle.

## Build orchestration (xc tasks → Makefile → scripts)
- `xc build` → `build-desktop` (`make desktop-build` + `make desktop-embed`,
  now signs wrapper) → `make build`.
- `xc release-osx <KEYSTORE_PASS>` → unlock keychain → `xc build` →
  `make release-darwin-arm64` + `release-darwin-amd64` → `bash scripts/build-macos-app`.

## Notes / future cleanup
- The packaged `pando-bin` still carries the (now signed) embedded wrapper (~10 MB
  dead weight), since the same release CLI binary is reused. The on-disk sibling
  takes precedence at runtime. Could be stripped later with a build tag that
  disables the embed for packaged builds.
