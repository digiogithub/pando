---
created_at: 2026-07-15T21:18:20.88153824Z
updated_at: 2026-07-15T21:22:56.06810284Z
tags:
    - change
    - macos
    - build
    - signing
    - notarization
    - gatekeeper
    - desktop
    - wails
    - release
    - pando
---
# Change: notarize the .app bundle and the embedded desktop wrapper

## Date
2026-07-15

## Motivation
Two remaining Gatekeeper-kill gaps after notarizing the loose CLI zips
([[macos_notarize_cli_release_zips]]):

1. **.app not stapled** — `scripts/build-macos-app` signed `Pando-<arch>.app` but
   only notarized+stapled the outer `.pkg`. Confirmed on mac: `xcrun stapler
   validate Pando-arm64.app` → "does not have a ticket stapled". A directly
   distributed `.app`, or an offline first-launch of the installed app, fails.
2. **Embedded desktop wrapper not notarized** — `make desktop-embed` signed the
   wails wrapper "no notarization". From a standalone CLI binary, `pando desktop`
   extracts the embedded wrapper to a temp dir and execs it
   (`internal/desktop/launcher.go`); an unnotarized hardened-runtime wrapper is
   killed. Crash report proof (`pando-darwin-arm64-...ips`):
   `termination namespace=CODESIGNING indicator="Invalid Page"`,
   `signal "SIGKILL (Code Signature Invalid)"`, `codeSigningTrustLevel 0xFFFFFFFF`.

## What changed
### scripts/build-macos-app
- New `notarize_and_staple_app()`: a bare `.app` cannot be submitted directly, so
  it is zipped with `ditto -c -k --keepParent` for `notarytool submit`, then
  `xcrun stapler staple` targets the `.app` in place (bundles CAN be stapled).
- Called on `$final_app` right after signing, BEFORE `pkgbuild` — so the pkg
  embeds an already-stapled app. Covers the nested `pando-desktop` wrapper
  (launched in place from Contents/MacOS), making both offline-safe.
- Added `ditto` to the required-tools guard.

### scripts/notarize-desktop-wrapper (new)
- Notarizes the embedded wrapper before `go:embed`. `.app` target → notarize +
  staple (ticket lives inside the bundle, survives byte-for-byte embed/extract →
  offline validation of the extracted app). Bare `pando-desktop` Mach-O →
  submit-only (cannot be stapled → online Gatekeeper check).
- Guarded/non-fatal: skips (exit 0) when not macOS, or xcrun/ditto/NOTARY_PROFILE/
  MACOS_SIGN_KEYCHAIN_PATH missing — dev embed builds without creds are unaffected.

### Makefile
- New vars `NOTARY_PROFILE` (default empty) and `MACOS_NOTARIZE_WRAPPER`.
- `desktop-embed` calls the notarize script after codesign for both the `.app`
  and bare-binary variants, passing `NOTARY_PROFILE` + `MACOS_SIGN_KEYCHAIN_PATH`
  inline, non-fatal on failure. Comment block updated.

### README.md — `release-osx` xc task
- Updated the xcfile.dev `release-osx` task so the full pipeline notarizes
  everything. Added `export NOTARY_PROFILE=${NOTARY_PROFILE:-pando-notary}` and
  `export MACOS_SIGN_KEYCHAIN_PATH=${…:-$HOME/Library/Keychains/pando-build-db}`
  BEFORE `xc build`, so `make desktop-embed` notarizes the embedded wrapper (env
  flows to the make recipe). Added a final `xcrun stapler validate` loop over the
  `.app`/`.pkg` outputs. Task description notes the 10–30 min notary wait.
- The outer `release` task's `scp …/dist/*.zip` already captures the notarized CLI
  zips and the `.pkg.zip` (both end in `.zip`) — left unchanged.

## Inherent limitations
- Bare Mach-O (loose CLI zip, embedded bare wrapper) cannot be stapled → online
  Gatekeeper check only; offline first-run needs the stapled `.app`/`.pkg`.
- The embedded `.app` path DOES staple, so extracted copies are offline-safe.

## Verification
- `bash -n` on both scripts, `make -n desktop-embed` parse → OK.
- notarize-desktop-wrapper skip path on non-Darwin → exit 0 (non-fatal).
- NOT run end-to-end: mac-mini-de-digio is a separate clone without these edits;
  real validation needs commit + pull + a release run with network and the
  `pando-notary` keychain profile.

## Related
[[macos_notarize_cli_release_zips]] [[macos_signed_binary_killed_not_notarized]]
[[macos_desktop_signing_fix]] [[macos_temporary_signing_keychain]]