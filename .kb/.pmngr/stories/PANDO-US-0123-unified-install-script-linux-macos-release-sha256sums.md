---
id: PANDO-US-0123
type: story
title: Unified install script (Linux + macOS), release SHA256SUMS, Windows installer fixes; docs home links to release binaries
status: in_review
priority: high
author: mcp
labels: [installer, release, docs, pando-docs]
created: 2026-10-02T13:08:35Z
updated: 2026-10-02T13:08:35Z
started: 2026-10-02T13:08:35Z
---

## Description

The docs home page sent visitors to `go install`. Releases ship signed binaries, so the site now links to the latest GitHub release, and the install scripts were reviewed against the real release assets and improved.

Full write-up: KB `pando/changes/installer-unified-install-sh.md`.

### Findings

- Root `install` was a broken leftover from the opencode fork (wrong app name, archive names and API).
- `scripts/install-linux.sh`: Linux only, mandatory sudo/GTK step that aborted the install, no version pin, API-only version lookup, no integrity check.
- `scripts/install-windows.ps1`: always failed on ARM64 (asked for a zip that is not published).

### Done

- `scripts/install.sh`: Linux and macOS, options `--version`, `--dir`, `--no-desktop`, `--cli-only`, `--force`; SHA-256 check against `SHA256SUMS`; macOS `.pkg` install with signature check; non-fatal desktop steps.
- `scripts/install-linux.sh` and `install` forward to `install.sh`.
- `scripts/install-windows.ps1`: ARM64 uses x64, TLS 1.2, checksum and Authenticode checks.
- `release.yml` publishes `SHA256SUMS`.
- `README.md`, `docs/installation.md`, docs site home, installers page, getting started and guides updated (en/es).

## Acceptance Criteria

- [x] Home page has no `go install`; it links to the latest release with per-platform downloads.
- [x] `install.sh` installs the latest and a pinned release on Linux x64 in a clean HOME.
- [x] Checksum mismatch aborts; a release without `SHA256SUMS` installs with a warning.
- [ ] macOS `.pkg` and `--cli-only` paths run on a Mac.
- [ ] `install-windows.ps1` run on Windows x64 and ARM64.
- [ ] Next tagged release publishes `SHA256SUMS` and the scripts verify against it.

## Notes

`install.sh` must reach `main` before the docs site that references its raw URL is deployed. `.goreleaser.yml` is stale and unused; not touched here.
