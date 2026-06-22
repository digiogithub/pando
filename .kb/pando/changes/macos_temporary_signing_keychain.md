---
created_at: 2026-06-22T21:53:40.811778113Z
updated_at: 2026-06-22T21:55:12.323408027Z
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

## Files and symbols touched
- `Makefile`
- `scripts/build-macos-app`
- `scripts/setup-macos-signing-keychain`
- `scripts/codesign-macos`
- `README.md`

## Why
The previous macOS release flow depended on unlocking the user login keychain in a remote interactive session, and `productsign` / `codesign` could still fail in SSH sessions with `errSecInteractionNotAllowed`. The new flow isolates signing material in a temporary build keychain populated from the signing keys already stored under the user home, making the pipeline usable in unattended SSH/CI runs.

## Verification
- Shell syntax checks passed for:
  - `bash -n scripts/setup-macos-signing-keychain`
  - `bash -n scripts/codesign-macos`
  - `bash -n scripts/build-macos-app`
- Static review confirmed the new flow uses `--keychain` consistently for `codesign`, `productsign`, and `notarytool`.
- Remote validation through `osx-shell` could not execute commands because the MCP tool currently returns `Pseudo-terminal will not be allocated because stdin is not a terminal.` before running the requested shell command.
- Full functional signing/notarization still needs validation on the macOS build host with real certificates and Apple credentials once the remote shell tool issue is resolved.
