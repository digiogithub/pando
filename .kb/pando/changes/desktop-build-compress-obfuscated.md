---
created_at: 2026-08-17T09:15:41.359324764Z
updated_at: 2026-08-17T09:17:58.290441181Z
tags:
    - fix
    - desktop
    - build
---
# Desktop build: UPX compress (non-macOS only), obfuscation removed

## Final state
- `desktop/wails.json`: `"obfuscated": false` (reverted).
- `Makefile`:
  - `WAILS_UPX_FLAG` var: `-upx` on non-Darwin, empty on Darwin.
  - `desktop-build` / `desktop-package`: `wails build ... $(WAILS_UPX_FLAG) [-clean] -o pando-desktop` — no `-obfuscated`.
  - `desktop-deps`: back to just installing `wails` CLI (garble install line removed).

## History
1. First pass: turned on both `-obfuscated` and `-upx` unconditionally (user reported `compress:false`/`obfuscated:false` in build logs).
2. Web research found UPX-compressed binaries are broken on macOS since Big Sur (invalid Mach-O header, process killed on launch, breaks codesign/notarization) — gated `-upx` off on Darwin via `WAILS_UPX_FLAG`, matching the pattern already used by `release-darwin-*` targets. Kept `-obfuscated` on all platforms at that point.
3. User decided obfuscation isn't worth it: adds ~40s to build time (58s vs 16s locally) for little practical gain — removed `-obfuscated` from both targets and reverted `wails.json` `obfuscated` back to `false`. `garble` install step removed from `desktop-deps` since nothing needs it anymore.

## Why (final)
- UPX: real macOS breakage, not just theoretical — kept disabled there, enabled elsewhere for smaller binaries.
- Obfuscation: user call — build-time cost isn't justified by the protection it gives for this project.

## Verification
- `make -n desktop-build desktop-package`: confirmed flags — `-tags webkit2_41 -upx -o pando-desktop` / `... -upx -clean -o pando-desktop`, no `-obfuscated`.
- `make desktop-build` on Linux: completes in ~16s (down from ~58s with obfuscation), Compiling/Compressing/Packaging all "Done".

## Files touched
- `desktop/wails.json`
- `Makefile` (WAILS_UPX_FLAG var, desktop-build, desktop-package, desktop-deps)
