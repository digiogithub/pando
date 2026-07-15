---
created_at: 2026-07-15T21:43:57.250954037Z
updated_at: 2026-07-15T21:43:57.250954037Z
tags:
    - fix
    - macos
    - build
    - signing
    - notarization
    - bash
    - pando
---
# Fix: build-macos-app zip verify false "not signed with hardened runtime"

## Date
2026-07-15

## Symptom
`xc release-osx` on mac-mini-de-digio aborted at the new CLI-zip notarization step:

```
Notarizing loose CLI release zips...
Error: pando-darwin-arm64.zip binary is not signed with the hardened runtime.
```

...even though the binary inside the zip WAS correctly Developer-ID signed with
the hardened runtime (`codesign -dv` → `flags=0x10000(runtime)`).

## Root cause
`verify_zip_binary_signed()` in `scripts/build-macos-app` tested the signature
with a pipeline under `set -o pipefail`:

```bash
if ! codesign -dv --verbose=2 "$bin" 2>&1 | grep -q 'flags=.*runtime'; then
```

`grep -q` exits as soon as it finds the match and closes the pipe; `codesign`
then gets SIGPIPE (exit 141). With `pipefail`, the pipeline reports that 141, so
`! <non-zero>` is true and the code wrongly concludes "not signed". A manual test
that used `grep -c` (which drains all input, so codesign exits 0) did not
reproduce it — masking the bug during initial verification.

Reproduced on the mac under `set -euo pipefail`: old form → false "unsigned";
new form → correct "signed".

## Fix
Capture codesign output to a variable and match with a bash glob (no pipe, no
early close):

```bash
local cs_out
cs_out="$(codesign -dv --verbose=2 "$bin" 2>&1 || true)"
if [[ "$cs_out" != *"(runtime)"* ]]; then
    ... error ...
fi
```

## Files
- `scripts/build-macos-app` — `verify_zip_binary_signed()`.

## Verification
- `bash -n scripts/build-macos-app` → OK.
- On mac, replicated both forms against the real signed zip binary under
  `set -euo pipefail`: buggy form = FALSE NEGATIVE, new form = correct "signed".
- Checked no other `| grep -q` pipelines remain in build-macos-app /
  notarize-desktop-wrapper.

## Follow-up
Fix is local — must be committed + pushed; the mac pulls before `xc release-osx`.
The already-built zips are correctly signed, so only the fix needs to land.

## Related
[[macos_notarize_cli_release_zips]] [[macos_notarize_app_and_desktop_wrapper]]
[[macos_signed_binary_killed_not_notarized]]
