---
created_at: 2026-10-02T13:08:35.525563622Z
updated_at: 2026-10-02T13:08:35.525563622Z
tags:
    - changes
    - installer
    - release
    - pando-docs
    - scripts
---
# Unified installer `scripts/install.sh`, release checksums, docs site points to release binaries (2026-10-02)

Status: implemented, uncommitted in both repos (`pando` and `pando-docs`). Related: [[feature-release-pipeline-ci-actions]], [[pando-docs-redesign-beneath-the-surface]].

## Motivation

The docs home page told visitors to `go install`, which is impractical for end users (needs a Go toolchain, no desktop wrapper). Releases already ship signed binaries: `.pkg` for macOS, zips for Linux and Windows. The install scripts were reviewed against the real release assets (v1.2.7: `pando-linux-{x64,arm64}.zip`, `pando-darwin-{x64,arm64}.zip`, `pando-windows-x64.zip`, `pando-<tag>-darwin-{x64,arm64}.pkg`, `Pando-{x64,arm64}.app.zip`).

## Review findings

- Root `install`: stale leftover from the opencode fork (APP=opencode, `opencode-<os>-<arch>.tar.gz`, opencode-ai API). Those assets never existed for Pando: the script could not work.
- `scripts/install-linux.sh`: asset names correct, but Linux only; always tried to install GTK/WebKitGTK with sudo and aborted the whole install when sudo or a package was missing (blocks servers, containers, CI); no version pin; latest version via the GitHub API only (60 req/h unauthenticated); no integrity check; `curl -s --progress-bar` hid the progress bar; a failed icon download aborted after the binary was already installed; no fish PATH support.
- `scripts/install-windows.ps1`: on ARM64 it requested `pando-windows-arm64.zip`, which no release ships, so it always failed there; no TLS 1.2 opt-in for Windows PowerShell 5.1; no integrity or signature check.
- `.goreleaser.yml` is also stale (tar.gz naming) and unused by `release.yml`. Left untouched.

## Changes in `pando`

- `scripts/install.sh` (new): Linux + macOS. Options `--version`, `--dir`, `--no-desktop`, `--cli-only`, `--force`, `--help`, each with an env var (`PANDO_VERSION`, `PANDO_INSTALL_DIR`, `PANDO_NO_DESKTOP`, `PANDO_CLI_ONLY`, `PANDO_FORCE`). Latest tag resolved from the `/releases/latest` redirect, API as fallback. SHA-256 verified against the release's `SHA256SUMS` (warning, not failure, when the release has none). Desktop dependency and menu-entry steps are non-fatal. macOS: downloads the `.pkg`, `pkgutil --check-signature`, `sudo installer`; `--cli-only` installs `pando-darwin-<arch>.zip` to the install dir. Works as root without sudo. fish PATH support.
- `scripts/install-linux.sh` and root `install`: now thin shims that run the sibling `install.sh` from a checkout or fetch it from `main` when piped. Published `curl .../install-linux.sh | bash` instructions keep working.
- `scripts/install-windows.ps1`: ARM64 falls back to the x64 zip, TLS 1.2, SHA256SUMS check, Authenticode check (fails on an invalid signature, warns on unsigned), accepts `-Version 1.2.7` without the `v`.
- `.github/workflows/release.yml`: new step generates `dist/SHA256SUMS` (zips and pkgs), published with the release; release notes mention it and the install one-liner.
- `README.md`, `docs/installation.md`: release-first instructions, script options table.

## Changes in `pando-docs`

- Home: hero CTA is now "Download" and the command box links to the latest release; the install block has macOS / Linux / Windows tabs with direct download links (`releases/latest/download/<asset>`; macOS links to the release page because the `.pkg` name carries the version), preselected by the visitor's platform (`data-tabs-platform` in `assets/js/core/pando.js`). No `go install` on the home page.
- `content/{en,es}/docs/features/installers.md` rewritten; `getting-started` reordered (release, script, Go, source); `guides/first-session` step 1 and `guides/install` outline updated.

## Verification

- `bash -n` on the three shell scripts.
- `install.sh` run in an isolated HOME on Linux x64 against the real v1.2.7 release: fresh install piped through stdin, rerun (already installed), pin to 1.2.6 via env vars, nonexistent version (clear 404 error), `--help`, unknown flag, root `install` shim.
- `verify_checksum` unit-tested with a stubbed fetch: match, `*name` format, mismatch (exit 1), unlisted file, no sums file.
- Docs site: production build clean; download links return 200; platform preselection and links checked with Playwright.

Not verified: macOS paths (`.pkg` and `--cli-only`), the Linux desktop-dependency branch (needs sudo), `install-windows.ps1` (no PowerShell on this machine), the new release workflow step (runs only on a tag).

## Ordering constraint

`install.sh` must be on `main` before the docs that reference its raw URL are deployed, and in the same push as the shims (they fetch it). `SHA256SUMS` appears from the next release; until then the scripts warn and continue.
