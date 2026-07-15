---
created_at: 2026-07-15T18:28:11.770449934Z
updated_at: 2026-07-15T18:28:11.770449934Z
tags:
    - fix
    - macos
    - build
    - signing
    - notarization
    - gatekeeper
    - distribution
    - pando
---
# Diagnosis: macOS signed release binary killed on distribution (not notarized)

## Date
2026-07-15

## Symptom
`dist/pando-darwin-arm64` (release, signed) runs fine on the build mac, but once
**distributed** (download/copy/AirDrop/unzip from browser) the process is
**killed** on launch. The **unsigned** binary runs locally too, but on
distribution shows only a soft Gatekeeper block dialog (not a hard kill).

## Investigation (via .ai/scripts/osx-connection.sh -> mac-mini-de-digio)
- `file`: Mach-O thin arm64, 129M, **not UPX-packed** (Makefile skips UPX for darwin).
- `codesign -dv`: valid. Developer ID Application: DIGIO Soluciones Digitales
  S.L.N.E. (LRE923483J). Hardened runtime `flags=0x10000`. Timestamped.
  No entitlements. `codesign --verify --strict` = valid, satisfies DR.
- Local run: `dist/pando-darwin-arm64 --version` -> EXIT=0 (works — freshly built
  file has no `com.apple.quarantine` xattr, so Gatekeeper never assesses it).
- **`xcrun stapler validate` -> "does not have a ticket stapled to it"** -> NOT NOTARIZED.
- Reproduction: copied binary, `xattr -w com.apple.quarantine "0083;0;Safari;"`,
  ran it -> hung/blocked by Gatekeeper (2-min timeout). Confirms the distributed kill.

## Root cause
Binary is **Developer ID signed + hardened runtime but NOT notarized**. Since
macOS 10.15, Gatekeeper requires notarization for Developer-ID software. Any
distributed copy carries `com.apple.quarantine`; an unnotarized hardened-runtime
Developer-ID binary under quarantine is rejected — and for a CLI Mach-O this
manifests as the process being killed (harsher than the plain-unsigned soft
dialog, which explains the "signed=killed vs unsigned=warning" difference the
user observed).

## Where it originates in the pipeline
- `scripts/build-macos-app` DOES `notarize_and_staple` — but only the **.pkg**
  (function at ~line 152, called ~line 400). The `.pkg` distribution path works.
- `Makefile release-darwin-arm64` builds the loose binary and signs it via
  `scripts/codesign-macos` (`codesign --force -o runtime --timestamp` only —
  no notarization). So the standalone `dist/pando-darwin-<arch>` / `.zip`
  artifacts are signed-but-not-notarized and get killed on distribution.

## Fix options
A. **Distribute the notarized .pkg** (recommended, offline-safe). `bash
   scripts/build-macos-app` already produces `pando-<VERSION>-darwin-arm64.pkg`
   notarized+stapled. Ship that, not the bare binary.
B. **Notarize the loose CLI zip** if a bare binary must be shipped:
   `xcrun notarytool submit dist/pando-darwin-arm64.zip --keychain-profile
   "pando-notary" --wait`. Apple registers the CDHash; quarantined copies then
   pass via Gatekeeper's ONLINE check. Caveat: you CANNOT `stapler staple` a bare
   Mach-O or a .zip (only .app/.pkg/.dmg), so first run needs network. For
   offline, wrap in a stapled .dmg or .pkg (option A).

## End-user interim unblock (not a distribution fix)
`xattr -dr com.apple.quarantine ./pando-darwin-arm64`

## Status
Diagnosis only — no code changed yet. Pending user decision: automate option B
(add notarize_and_staple to the darwin zip release flow in Makefile/scripts) or
document the .pkg as the official distribution channel.

## Related
[[macos_desktop_signing_fix]] [[macos_temporary_signing_keychain]]
[[macos_signing_keychain_reuse]]
