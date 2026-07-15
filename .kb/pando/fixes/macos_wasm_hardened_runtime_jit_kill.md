---
created_at: 2026-07-15T22:33:02.511885348Z
updated_at: 2026-07-15T22:33:02.511885348Z
tags:
    - fix
    - macos
    - build
    - signing
    - notarization
    - hardened-runtime
    - wasm
    - wazero
    - jit
    - entitlements
    - pando
---
# Fix: macOS signed+notarized pando "Killed" — hardened runtime blocks wazero JIT

## Date
2026-07-16

## Symptom
On mac-mini-de-digio (macOS 26.5.2, arm64), the released pando is **killed** at
launch, from every distribution form (loose binary, zip, pkg-installed
`/usr/local/bin/pando`, `/Applications/Pando.app`), even though all are
Developer-ID signed, notarized, and the pkg/app are stapled.

Key tell: `pando -v` / `--version` / `--help` exit 0, but `pando`, `pando serve`,
`pando desktop` and `pando convert <pdf>` die. So it is NOT Gatekeeper /
quarantine / notarization (all verified good: `spctl` = "Notarized Developer ID
accepted", `stapler validate` = worked).

## Root cause
The binary embeds the **wazero** WASM runtime with the **wazevo compiler**
backend (`github.com/tetratelabs/wazero/internal/engine/wazevo`) to run
PDFium / markitdown WASM for KB document conversion. The compiler writes native
arm64 code and maps it executable (MAP_JIT / mprotect RW->RX).

Every Mach-O was signed with the **hardened runtime** (`codesign -o runtime`,
`flags=0x10000`) and **zero entitlements** (`codesign -d --entitlements -` was
empty). Under the hardened runtime, mapping writable memory executable is
blocked by AMFI and the kernel **SIGKILLs** the process ("Killed: 9") the
instant any WASM module is instantiated. `pando -v` never touches WASM, so it
survives; the full app (TUI/serve/desktop) and `pando convert <pdf>` instantiate
PDFium WASM at startup and die.

## Proof (on mac-mini via osx-shell)
- `pando convert min.pdf` (real PDF, forces PDFium WASM) with the shipped
  hardened-runtime, no-entitlement binary -> **exit 137 (SIGKILL)**.
- Same binary re-signed ad-hoc with `com.apple.security.cs.allow-jit` +
  `allow-unsigned-executable-memory` -> **exit 0**, prints "Hello WASM".
- Control: re-signed ad-hoc `-o runtime` WITHOUT the entitlements -> **exit 137**.

## Fix
Sign every Mach-O with a hardened-runtime entitlements plist granting JIT /
executable memory. New file `scripts/pando.entitlements`:
- `com.apple.security.cs.allow-jit`
- `com.apple.security.cs.allow-unsigned-executable-memory`

These are allowed for Developer ID + notarization, so signing with them still
notarizes.

### Files changed
- `scripts/pando.entitlements` — NEW plist (the two entitlements + rationale).
- `scripts/codesign-macos` — resolves `ENTITLEMENTS` (`$SCRIPT_DIR/pando.entitlements`,
  overridable via `MACOS_ENTITLEMENTS`); adds `--entitlements` to the `codesign`
  call (warns if the plist is missing). Signs the loose CLI binary / release zip.
- `scripts/build-macos-app` — adds `ENTITLEMENTS` var; passes `--entitlements`
  to the three sign steps: `pando-bin`, `pando-desktop`, and the `--deep` sign of
  `Pando.app` (the `--deep` re-sign would otherwise strip the grant from the
  nested Mach-Os).
- `Makefile` — new `MACOS_ENTITLEMENTS` var; adds `--entitlements` to the two
  `desktop-embed` signing calls (embedded Pando.app / pando-desktop wrapper).

## Verification
- Mechanism proven end-to-end on the mac (137 -> 0 with the entitlement, control
  stays 137).
- `bash -n` clean on both scripts; `make -n desktop-embed` parses.
- Full Developer-ID re-sign of the SHIPPED artifacts still requires the build's
  keychain (an ad-hoc SSH `codesign --sign "Developer ID..."` returned
  `errSecInternalComponent` because the login keychain was locked in the
  non-TTY session — not a script bug).

## Rollout
The already-built dist artifacts are broken and must be **rebuilt** — re-run
`xc release-osx` on the mac. That flow pulls the repo first, so this fix must be
**committed and pushed** before rebuilding (same caveat as the earlier zip-verify
fix). No code change, only signing.

## Related
[[macos_signed_binary_killed_not_notarized]] [[macos_zip_verify_sigpipe_false_negative]]
[[macos_notarize_cli_release_zips]]
