---
id: PANDO-US-0043
type: story
title: Windows degradation and Job Object containment
status: backlog
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 3
created: 2026-09-18T08:36:26Z
updated: 2026-09-18T08:36:26Z
---

## Description

**As a** Windows user **I want** Pando to tell me honestly that commands are not sandboxed and still clean up child processes **so that** I am not given a false sense of safety.

### Implementation

- `internal/sandbox/windows.go`: `Capability{Backend:"none", Reason:"not supported on windows"}`.
- `Wrap` assigns the child to a Job Object (`golang.org/x/sys/windows` `CreateJobObject`, `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`) after start, through a post-start hook in the shell.
- Auto-allow is forced off so prompts remain.
- Detect WSL on the Linux side (`/proc/sys/fs/binfmt_misc/WSLInterop`) and report it as full Linux support.
- Add a spike ticket note for AppContainer.

## Acceptance Criteria

- [ ] On Windows, settings and badge show "On (not enforced on this OS)", and bash still prompts.
- [ ] Killing Pando kills the shell's process tree (manual test).
- [ ] Cross-compile succeeds with `GOOS=windows` for both CGO modes.

## Notes

Depends on: PANDO-US-0040.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
