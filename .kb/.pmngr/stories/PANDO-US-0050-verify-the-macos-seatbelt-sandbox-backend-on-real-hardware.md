---
id: PANDO-US-0050
type: story
title: Verify the macOS Seatbelt sandbox backend on real hardware
status: done
priority: high
labels: [sandbox, macos, security]
estimate: 3
created: 2026-09-18T11:01:14Z
updated: 2026-10-14T20:03:37Z
closed: 2026-10-14T20:03:31Z
---

## Description

As a macOS user, I want the Seatbelt backend verified on real hardware, so that the default-on sandbox is known to work on macOS and not only through golden tests.

PANDO-EP-0009 shipped the macOS backend (`internal/sandbox/sbpl.go`, `wrapper_darwin.go`, `e2e_darwin_test.go`) but no test ran on a Mac during development. Constructs that need confirmation: `(remote ip "*:*")` / `(local ip "*:*")`, `(remote tcp "*:P")` guarded-port denies, `ipc-posix-shm*`, `process-info*`, `file-read-metadata`, last-match-wins for the specific write-action denies inside the workspace, ancestor rename guards, and `--` handling by `sandbox-exec`.

## Acceptance Criteria

- [x] `go test ./internal/sandbox -count=1 -v` passes on macOS 14/15/26 (arm64, ideally also Intel) with the probe reporting `seatbelt` enforced and full protection.
- [x] `python3 -m unittest tests/test_sandbox_cli.py` passes on macOS.
- [x] The `sandbox-e2e` GitHub workflow macOS job is green.
- [x] Verified inside the signed, notarized desktop app and the signed CLI zip (hardened runtime, `scripts/pando.entitlements` unchanged).
- [x] Real workflows in workspace-write: `go build`, `npm install`, `git commit`, `brew`, `xcrun clang`, zsh login shell.

## Notes

Follow-up of PANDO-EP-0009. `(allow mach-lookup)` is unrestricted; consider an allow-list later.
