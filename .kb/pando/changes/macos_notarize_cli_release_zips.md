---
created_at: 2026-07-15T18:37:13.073798324Z
updated_at: 2026-07-15T18:37:13.073798324Z
tags:
    - change
    - macos
    - build
    - signing
    - notarization
    - gatekeeper
    - pando
---
# Change: notarize the loose CLI release zips in build-macos-app

## Date
2026-07-15

## Motivation
`dist/pando-darwin-<arch>.zip` (the bare CLI binaries produced by
`make release-darwin-*` via `scripts/codesign-macos`) were signed (Developer ID +
hardened runtime + timestamp) but NOT notarized. Once distributed, a copy carries
`com.apple.quarantine` and Gatekeeper kills the unnotarized hardened-runtime
binary. See [[macos_signed_binary_killed_not_notarized]] for the diagnosis.

## What changed
File: `scripts/build-macos-app`
- Split the old `notarize_and_staple()` into:
  - `notarize_submit()` — submit to Apple notary + `--wait`; returns 2 when
    skipped (no `NOTARY_PROFILE` / no `xcrun`) so callers skip the staple.
  - `notarize_and_staple()` — calls `notarize_submit` then `xcrun stapler
    staple`/`validate`. Used for the `.pkg` (unchanged behavior).
- Added `notarize_cli_zip()` — submit-only notarization for a `.zip` wrapping a
  bare CLI Mach-O. A bare Mach-O / `.zip` CANNOT carry a stapled ticket, so only
  Gatekeeper's ONLINE check applies afterward.
- Added `verify_zip_binary_signed()` — extracts the binary from the zip and
  requires a hardened-runtime signature (`codesign -dv` shows `flags=...runtime`)
  before submitting; notarytool rejects binaries lacking it.
- After `WORK_DIR` is created, a loop notarizes `pando-darwin-arm64.zip` and
  `pando-darwin-x64.zip` (submit-only). Placed after `WORK_DIR=$(mktemp -d)`
  because the probe extracts into `$WORK_DIR` (initial draft had the loop before
  WORK_DIR existed — fixed).
- Header comment updated to list the notarized zips and the online-check caveat.

## Inherent limitation
Notarizing a zip only registers the contained Mach-O's CDHash with Apple; you
cannot staple a ticket to a bare Mach-O or a `.zip`. A quarantined copy therefore
passes only on machines WITH network access (online Gatekeeper check). For
offline first-run, distribute the stapled `.pkg` (unchanged flow).

## Verification
- `bash -n scripts/build-macos-app` → OK (shellcheck not installed locally).
- Logic reviewed for `set -e` safety (`|| rc=$?` capture; `test && return` idiom
  is exempt from set -e).
- NOT executed end-to-end: real `xcrun notarytool submit` requires macOS +
  network + the `pando-notary` keychain profile; it uploads to Apple. Left for a
  real release run on mac-mini-de-digio.

## Related
[[macos_signed_binary_killed_not_notarized]] [[macos_temporary_signing_keychain]]
[[macos_desktop_signing_fix]] [[macos_signing_keychain_reuse]]
