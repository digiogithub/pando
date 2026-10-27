---
created_at: 2026-10-27T22:50:00Z
updated_at: 2026-10-27T22:50:00Z
tags:
    - change
    - desktop
    - nativeui
    - ci
    - macos-signing
---
# Native desktop: libghostty-vt embedded in the pando binary (2026-10-27)

`pando` is now one self-contained binary on every platform (CLI zip, Pando.app, .pkg): the native desktop terminal's library `libghostty-vt` (MyGo `plugins/terminal`, loaded with purego) is embedded with go:embed and extracted at start-up. On macOS it is signed with the same Developer ID as pando **before** compiling, so hardened-runtime library validation accepts it without `disable-library-validation`.

## Go
- New package `internal/nativeui/natives`:
  - `embed_<goos>_<goarch>.go` (darwin/linux/windows x amd64/arm64) embed `all:<goos>-<goarch>`; each dir has a committed `.keep` placeholder so builds without the fetched lib compile. `embed_other.go` (explicit build constraint) has an empty FS. Real library files are gitignored (`internal/nativeui/natives/*/*`, `!.keep`).
  - `Install()`: when `$MYGO_GHOSTTY_VT` is unset (or was set by another pando, tracked by `$PANDO_NATIVES_GHOSTTY_VT`) and a lib is embedded, writes it to `os.UserCacheDir()/pando/natives/<sha256[:16]>/<name>` (temp file + fsync + chmod 0755 + rename, re-verified by SHA-256, rewritten on mismatch, skipped when already identical) and sets `$MYGO_GHOSTTY_VT`. No embedded lib -> no-op, MyGo keeps its own lookup (Resources, exe dir, download to `~/.cache/mygo/natives`).
  - MyGo reads `MYGO_GHOSTTY_VT` lazily in `terminal.Load()` (`internal/library.Find`, sync.Once on first terminal use), so setting it in `nativeui.Run` before `shell.Run` is early enough.
  - Tests `natives_test.go`: content-addressed write + perms, idempotent (mtime unchanged), hash-mismatch rewrite, no-embedded fallback, user env respected, real binary install (inherited pando env overridden).
- `internal/nativeui/nativeui.go` `Run` calls `natives.Install()` right after creating the app context (warn on error, fallback to MyGo).

## Build
- Makefile: `NATIVES_DIR`, `NATIVES_TARGETS`, `NATIVES_SIGN` (auto|required|off), `HOST_NATIVES`; targets `natives-<goos>-<goarch>` (fetch via scripts/fetch-ghostty-vt into the embed dir; darwin: sign with `$(MACOS_CODESIGN_WRAPPER)` when SIGNING_IDENTITY/MACOS_SIGN_IDENTITY + codesign exist, then `codesign --verify --strict`; `required` deletes the unsigned dylib and fails), `natives-host` (never fails: warns offline), `natives`, `natives-clean`. `build` depends on `natives-host`; each `release-*` depends on its natives; `release-darwin-*` set `NATIVES_SIGN=required` (target-specific var propagates to the prerequisite).
- `scripts/fetch-ghostty-vt`: keeps a verified pristine copy in `$PANDO_NATIVES_CACHE` (default `<user cache>/pando-build/natives/<sha256>/`) so rebuilds work offline; copies it atomically to the dest. Doc comments updated for go:embed.
- `scripts/codesign-macos`: `.dylib` targets are signed without entitlements; empty-array expansion made bash-3.2/`set -u` safe.
- `scripts/build-macos-app`: no longer fetches/copies/signs a dylib into `Contents/MacOS` (bundle holds only pando-bin + wrapper); `build_arch_package` lost its goarch arg.

## CI
- `release.yml`: linux-windows job runs `make natives-linux-amd64 natives-linux-arm64 natives-windows-amd64` before the release build; macOS job runs `make natives-darwin-arm64 natives-darwin-amd64 NATIVES_SIGN=required` after the signing keychain step and before `make release-darwin-*`. Header comment explains natives. Windows DLL is embedded unsigned (only pando.exe is Authenticode-signed) — signing the DLL would need Trusted Signing on the ubuntu job before `go build`: follow-up.
- `build-matrix.yml`: `make natives-linux-amd64` before vet/build; `go test ./internal/nativeui/natives` added.
- YAML validated with python `yaml.safe_load`.

## Docs
README `## Tasks` (build/release/release-osx comments: natives fetched + signed by make release-darwin-*, no Wails refs), `docs/release-pipeline.md` (new section "One binary: the embedded terminal library", diagram), `docs/installation.md`, `internal/nativeui/README.md` (package table).

## Verification
- Linux: `go build ./...`, `go vet ./internal/nativeui/... ./cmd/...`, `go test ./internal/nativeui/...` (all ok; cross `go vet` of natives for windows/darwin/freebsd ok). `make build` fetched linux-amd64 lib and embedded it (binary 150 MB). Offline simulation (`HTTPS_PROXY` dead, empty cache) -> warning, exit 0. `NATIVES_SIGN=required` on Linux for darwin -> fails and removes the unsigned dylib.
- `./pando desktop` with isolated HOME, PANDO_DESKTOP_MULTI=1, capture of /terminal: shell prompt rendered; lib extracted to `$HOME/.cache/pando/natives/9dcd8af0de67778a/libghostty-vt.so` (sha matches manifest), `~/.cache/mygo` absent.
- Mac mini (temporary git worktree, removed afterwards): `make natives-darwin-arm64 NATIVES_SIGN=required` signed the dylib with `Developer ID Application: DIGIO ... (LRE923483J)`, runtime flag, timestamp; `go build` + `scripts/codesign-macos` (hardened runtime + entitlements). Desktop run with isolated HOME: terminal works; extracted dylib byte-identical to the embedded one, `codesign --verify --strict` valid, same Team ID; xattrs only `com.apple.provenance` (no `com.apple.quarantine`); `~/Library/Caches/mygo` absent. Negative control: same hardened pando with `MYGO_GHOSTTY_VT` = upstream ad-hoc dylib -> "mapping process and mapped file (non-platform) have different Team IDs" (proves library validation is active and that signing the dylib is required).

## Not verified
- Full CI run / notarization of a binary that embeds the dylib (expected fine: notarization scans the outer Mach-O; the dylib is only data until extracted). Gatekeeper behaviour on a quarantined, downloaded CLI: the extracted file is written by pando so it is not quarantined.
- darwin-amd64 build, Windows/Linux-arm64 runtime extraction.
- The terminal area's error text still says "downloads libghostty-vt on first use"; accurate only for binaries without the embedded lib.
