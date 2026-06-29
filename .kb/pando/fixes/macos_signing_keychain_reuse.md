---
created_at: 2026-06-29T08:26:41.92796772Z
updated_at: 2026-06-29T08:26:41.92796772Z
tags:
    - fix
    - macos
    - build
    - signing
---
# Fix: macOS signing keychain "already exists" abort on release build

## Date
2026-06-29

## Symptom
Running `xc release-osx` (which calls `bash scripts/setup-macos-signing-keychain`)
aborted with:

```
security: SecKeychainCreate pando-build: A keychain with the same name already exists.
```

The build never reused the existing `pando-build` keychain; it always tried to
re-create it and `set -euo pipefail` killed the script on the failure.

## Root cause
`security create-keychain <name>` materializes the file as `<name>-db` under
`~/Library/Keychains` (e.g. `pando-build-db`). The existence check in
`scripts/setup-macos-signing-keychain` relied **only** on
`security show-keychain-info <path>`, which succeeds only for keychains present
in the current keychain **search list**. A keychain that exists on disk but was
never added to the search list (the normal state on a fresh shell/session) is
not detected, so `KEYCHAIN_EXISTS` stayed 0, the script fell through to
`security create-keychain`, and that failed because the file already existed.

## Change
File: `scripts/setup-macos-signing-keychain`

1. Broadened existence detection: now loops over every plausible on-disk
   spelling of the keychain file (`$KEYCHAIN_PATH`, `$KEYCHAIN_PATH_LEGACY`,
   `~/Library/Keychains/${BASE}-db`, `${BASE}.keychain-db`, `${BASE}.keychain`)
   and treats a present file (`-f`) OR a successful `show-keychain-info` as
   existing. Also keeps the bare-name `show-keychain-info` probe as a fallback.
2. Made `create-keychain` tolerant: its stderr/exit status are captured, and an
   "already exists" failure is treated as a reuse (log + continue) instead of
   aborting the whole release build. Any other failure still exits with the
   original status.

The rest of the script (unlock, list-keychains, import certs,
set-key-partition-list) is unchanged and runs against the now-detected keychain.

## Verification
- `bash -n scripts/setup-macos-signing-keychain` → syntax OK.
- Logic reviewed: on a host where `pando-build-db` already exists, the file
  probe now sets `KEYCHAIN_EXISTS=1` and the create step is skipped; even if a
  naming corner case slips through, the tolerant create swallows the
  "already exists" error rather than aborting.
- Cannot run end-to-end here (requires macOS + Developer ID certs); the user
  runs the real `xc release-osx` on their Mac.
