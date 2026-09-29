---
created_at: 2026-09-29T15:40:27.850128581Z
updated_at: 2026-09-29T15:40:27.850128581Z
---
# Change: stop publishing the zipped macOS .pkg (2026-09-29)

## What
`scripts/build-macos-app` no longer runs `zip` on the final installer, so `pando-<VERSION>-darwin-<arch>.pkg.zip` is no longer produced. The release still publishes the signed, notarized and stapled `.pkg`, the CLI zips `pando-darwin-<arch>.zip`, and the `Pando-<arch>.app.zip` bundles.

The header comment listing the outputs was updated to explain why the `.pkg` is not zipped.

## Why
A flat `.pkg` is already xar-compressed, so the zip was the same size and only duplicated the asset. Measured on the v1.1.0 release: arm64 41.3 MB `.pkg` vs 40.8 MB `.pkg.zip`, and x64 45.4 MB vs 45.0 MB.

Nothing consumes it:
- `pando update` has no `.pkg` handling.
- There is no Makefile or workflow reference.
- `release.yml` uploaded it only through the `dist/*.zip` glob, so the workflow needed no change.

## Verification
- `bash -n scripts/build-macos-app` passes.
- grep shows no remaining `.pkg.zip` producer or consumer in the code.
- The effect will only be visible on the next tagged release. v1.1.1 was built before this change and still carries the `.pkg.zip` assets.

Related: [[pando/fixes/macos_desktop_signing_fix.md]], [[pando/changes/macos_notarize_app_and_desktop_wrapper.md]]
