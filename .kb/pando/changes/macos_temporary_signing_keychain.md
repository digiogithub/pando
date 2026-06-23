---
created_at: 2026-06-22T22:11:15.392056045Z
updated_at: 2026-06-22T22:13:21.319665532Z
tags:
    - macos
    - build
    - signing
    - ci
    - keychain
    - pando
---
# macOS temporary signing keychain for unattended builds

## What changed
- Added `scripts/setup-macos-signing-keychain`, a reusable macOS helper that creates a temporary keychain, imports the Developer ID Application and Developer ID Installer `.p12` files from `~/DIGIO_Software_Signing_Keys` (or `SIGNING_KEYS_DIR`), configures `set-key-partition-list`, and can optionally store a `notarytool` profile in that temporary keychain.
- Added `scripts/codesign-macos` so Makefile-driven Darwin CLI builds sign binaries explicitly with `codesign --keychain <temp-keychain>` instead of relying on a preconfigured `codesign-digio` helper or the unlocked login keychain.
- Updated `scripts/build-macos-app` to require `MACOS_SIGN_KEYCHAIN_PATH`, unlock only that temporary keychain, pass `--keychain` to both `codesign`, `productsign`, and `xcrun notarytool submit`, and remove the old login-keychain unlock / relock flow.
- Updated `Makefile` to point Darwin binary signing at `scripts/codesign-macos` and added variables for `MACOS_SIGN_KEYCHAIN_PATH` / wrapper location.
- Updated `README.md` release instructions to describe the new unattended SSH/CI flow based on `scripts/setup-macos-signing-keychain`, including a quick smoke test that verifies imported identities in the temporary keychain.
- Adjusted `scripts/setup-macos-signing-keychain` to support passwordless `.p12` files and to use the actual path layout created by macOS for `security create-keychain` (`~/Library/Keychains/<name>-db`).

## Files and symbols touched
- `Makefile`
- `scripts/build-macos-app`
- `scripts/setup-macos-signing-keychain`
- `scripts/codesign-macos`
- `README.md`

## Why
The previous macOS release flow depended on unlocking the user login keychain in a remote interactive session, and `productsign` / `codesign` could still fail in SSH sessions with `errSecInteractionNotAllowed`. The new flow isolates signing material in a temporary build keychain populated from the signing keys already stored under the user home, making the pipeline usable in unattended SSH/CI runs.

## Verification
- Local shell syntax checks passed for:
  - `bash -n scripts/setup-macos-signing-keychain`
  - `bash -n scripts/codesign-macos`
  - `bash -n scripts/build-macos-app`
- Remote validation on the macOS build host succeeded using `.ai/scripts/osx-connection.sh`:
  - confirmed `~/DIGIO_Software_Signing_Keys` exists and contains `developerID_application.p12`
  - created the temporary signing keychain with `KEYSTORE_PASS=pando-build`
  - imported both Developer ID identities using passwordless `.p12` inputs (`developerID_application.p12` and `certificado.p12` as installer bundle override)
  - verified identities with `security find-identity -v -p basic /Users/digio/Library/Keychains/pando-build-db`, which returned both the Developer ID Application and Developer ID Installer identities
- Remote artifact signing checks:
  - `make release-darwin-arm64` succeeded when run with `PATH` including `/opt/homebrew/bin`, `SIGNING_IDENTITY`, `PKG_SIGN_IDENTITY`, `MACOS_SIGN_KEYCHAIN_PATH=/Users/digio/Library/Keychains/pando-build-db`, `MACOS_INSTALLER_CERT_PATH=$HOME/DIGIO_Software_Signing_Keys/certificado.p12`, and `MACOS_CODESIGN_WRAPPER=$PWD/scripts/codesign-macos`
  - `make release-darwin-amd64` also succeeded with the same environment
  - `bash scripts/build-macos-app` succeeded through bundle creation, `codesign`, app verification, installer creation, and `productsign`
- Remaining blocker:
  - notarization failed because the temporary keychain does not yet contain the `notarytool` credentials profile `pando-notary` (`Error: No Keychain password item found for profile: pando-notary`)
  - final end-to-end automated packaging now only requires storing notary credentials in the temporary keychain (or providing Apple credentials so the script can do it with `NOTARYTOOL_STORE_CREDENTIALS=1`).
