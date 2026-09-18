---
id: PANDO-US-0042
type: story
title: macOS backend with a Seatbelt profile via sandbox-exec
status: backlog
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 5
created: 2026-09-18T08:36:25Z
updated: 2026-09-18T08:36:25Z
---

## Description

**As a** macOS user **I want** the same confinement **so that** the default protection is equal on my platform.

### Implementation

- `internal/sandbox/darwin.go` and `sbpl.go`: generate the SBPL.
  - deny default; allow process, sysctl-read, mach-lookup, file-read*
  - allow file-write* on the writable roots plus `/dev/null`, `/dev/tty`, `/dev/ptmx`, `/dev/ttys*`
  - deny file-write* on protected paths, emitted after the allows
  - network* allowed, or outbound denied except unix sockets when restricted
- Parameters go through `-D` to avoid escaping bugs. Reject control characters (Grok `deny/mod.rs:26-35`) and add `/private` aliases (Grok `macos_deny_aliases`).
- `Wrap` sets `cmd.Path=/usr/bin/sandbox-exec` and `Args=[sandbox-exec -p <profile> -D ... -- orig...]`.
- Capability check: `/usr/bin/sandbox-exec` exists and a trivial `true` run under the profile succeeds.

## Acceptance Criteria

- [ ] Golden-file tests for SBPL output in each mode.
- [ ] Darwin e2e (build tag, skipped elsewhere): the same matrix as S2, plus `/tmp` vs `/private/tmp` alias denies and network blocked in `restricted`.
- [ ] Verified in the signed, notarized desktop build (`desktop/`) and the CLI zip.

## Notes

Depends on: PANDO-US-0040.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
