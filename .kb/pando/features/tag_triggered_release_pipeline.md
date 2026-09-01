---
created_at: 2026-09-01T21:11:57.826290809Z
updated_at: 2026-09-01T21:11:57.826290809Z
tags:
    - feature
    - ci
    - github-actions
    - release
    - macos
    - signing
    - notarization
    - pando
---
# Feature: tag-triggered release pipeline with reusable signing actions

## Date
2026-09-01

## Motivation
Releases were manual: `xc release` on Linux for the cross builds, then
`xc release-osx` over SSH on mac-mini-de-digio for the signed + notarized macOS
artifacts (it sources `~/DIGIO_Software_Signing_Keys/kvagerc` and calls
`scripts/setup-macos-signing-keychain`), then `scp` of the zips back. Nothing
published the GitHub release. The same Developer ID signing is also wanted for
other Go and Rust repositories, so the signing steps had to become reusable
rather than pando-specific.

## What was built

### New repository `digiogithub/ci-actions` (public, tag `v1`)
Language-agnostic composite actions, layered so a consuming repo only supplies
inputs:

- `macos-signing-keychain` — unpacks a base64 `.tar.gz` of the Developer ID
  certificates into `RUNNER_TEMP`, optionally sources an env file from inside
  the bundle (`kvagerc`), masks every password it reads, creates an ephemeral
  keychain, imports both `.p12`, sets the key partition list, stores a
  `notarytool` profile, deletes the raw certificates, and exports
  `SIGNING_IDENTITY`, `PKG_SIGN_IDENTITY`, `MACOS_SIGN_KEYCHAIN_PATH`,
  `MACOS_SIGN_KEYCHAIN_PASSWORD`, `KEYSTORE_PASS`, `NOTARY_PROFILE` — exactly
  the variables the existing Makefile and `scripts/` already read, so no build
  script had to change.
- `macos-codesign` — hardened runtime + timestamp + entitlements, globs.
- `macos-notarize` — staples `.app`/`.pkg`/`.dmg`, submit-only for bare Mach-O
  and `.zip`.
- `macos-keychain-cleanup` — `if: always()` teardown.
- `setup-zig` — pinned Zig install (cross C toolchain for cgo).
- `go-cross-build` — one GOOS/GOARCH target per step, optional UPX/archive.
- `github-release` — create/update the release, notes generated against the
  previous tag (`--notes-start-tag`) plus an optional commit list; refuses to
  publish when no artifact matches.
- `.github/workflows/release-go.yml` — `workflow_call` pipeline that wires all
  of the above for a plain Go repo (target list as JSON input).

### `pando/.github/workflows/release.yml`
Triggered by `v*` tags (plus `workflow_dispatch` with a `tag` input to re-cut a
release without moving the tag). Three jobs:
- `linux-windows` (ubuntu): bun web-ui, wails `desktop-embed`, then
  `make release-linux-amd64 release-linux-arm64 release-windows-amd64` with zig
  as the cross C toolchain and UPX. Windows is unsigned for now.
- `macos` (macos-latest, arm64, 120 min timeout): keychain action, then exactly
  the `release-osx` sequence — `make desktop-embed` (signs + notarizes the
  embedded wails wrapper), `make release-darwin-arm64 release-darwin-amd64`,
  `scripts/build-macos-app` (notarizes the CLI zips, builds/signs/notarizes/
  staples the `.app` bundles and `.pkg` installers), `stapler validate` as a
  hard gate, then `ditto` of the `.app` bundles into release assets.
- `release`: downloads both artifact sets and publishes with `github-release`.

### Secret
One repository secret `MACOS_SIGNING_BUNDLE` on `digiogithub/pando`:
`ssh mac-mini-de-digio 'tar czf - -C ~ DIGIO_Software_Signing_Keys | base64' | gh secret set MACOS_SIGNING_BUNDLE --repo digiogithub/pando`
(27 KB, under the 48 KB limit; the value never lands on disk or in history).
The bundle carries both `.p12` and `kvagerc`, so the same secret is enough for
any other repository.

## Files touched
- new: `.github/workflows/release.yml`, `docs/release-pipeline.md`
- modified: `README.md` (note on the `release` / `release-osx` xc tasks),
  `scripts/setup-macos-signing-keychain` (guard against re-adding the keychain
  to the user search list on every run — mac-mini-de-digio had accumulated 22
  duplicate entries)
- new repo: `digiogithub/ci-actions` @ `v1`

## Gotchas found
- **LibreSSL**: deriving the signing identity from the `.p12` with
  `openssl pkcs12 -legacy` only works when Homebrew's OpenSSL 3 is first on
  PATH; Apple's `/usr/bin/openssl` (LibreSSL 3.3) fails with
  `Expecting: TRUSTED CERTIFICATE`. The action imports first and reads the
  identities back with `security find-identity -v <keychain>`.
- The installer certificate in `DIGIO_Software_Signing_Keys` is named
  `certificado.p12`, not `*developer*id*installer*.p12`; auto-discovery alone
  would miss it. `kvagerc` names it explicitly and the action re-anchors those
  absolute developer-machine paths onto the runner by basename.
- The Makefile requires `zig` to be present for the non-native darwin arch even
  though it compiles it with `clang -arch`, so the macOS job installs zig too.
- `MACOS_SIGN_IDENTITY`, `NOTARY_PROFILE` and `MACOS_SIGN_KEYCHAIN_PATH` are
  `?=` variables in the Makefile, so the environment exported by the action
  overrides the hardcoded defaults.

## Verification
- `actionlint` clean on both workflows; every embedded `run:` block passes
  `bash -n`; all action YAML parses.
- The rewritten keychain script was run for real over SSH on mac-mini-de-digio
  (macOS 26.5) against a copy of the bundle extracted to a foreign path: both
  identities resolved (Developer ID Application + Installer) and a
  `codesign --force -o runtime --timestamp` signature validated with
  `Authority=Developer ID Application`. The temporary keychain was deleted.
- Not yet exercised end to end on a runner: that needs the workflow committed
  and a `v*` tag pushed. A tag-triggered workflow runs the file contained in the
  tagged commit, so the workflow must be pushed before the tag is created.

## Related
[[macos_notarize_app_and_desktop_wrapper]] [[macos_notarize_cli_release_zips]]
[[macos_temporary_signing_keychain]] [[macos_desktop_signing_fix]]
[[macos_signed_binary_killed_not_notarized]]
