---
id: PANDO-US-0049
type: story
title: Cross-platform test suite, CI and documentation
status: backlog
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 5
created: 2026-09-18T08:36:28Z
updated: 2026-09-18T08:36:28Z
---

## Description

**As a** maintainer **I want** automated e2e coverage and user docs **so that** sandbox regressions are caught and users understand the defaults.

### Implementation

- Go e2e under `internal/sandbox/e2e_linux_test.go` and `e2e_darwin_test.go`, which self-skip without kernel support (following Grok `tests/deny_paths_e2e.rs`).
- Python black-box tests in `tests/sandbox/` (repo rule), driving the bash tool through the API.
- CI: add a Linux job with a Landlock-capable runner (GitHub ubuntu-latest has Landlock ABI ≥3) and a macOS job. Both are in the `digiogithub/ci-actions` workflows.
- Docs: `docs/` user guide "Sandbox" (modes table, protected paths, escalation, platform matrix, how to disable), and a README mention.
- Save the KB doc `pando/features/host-command-sandbox.md` and add a MEMORY index entry (project rule).

## Acceptance Criteria

- [ ] CI runs the Linux and macOS e2e suites on every PR touching `internal/sandbox`, `internal/llm/tools/shell` or `bash.go`.
- [ ] The docs cover the "off" path and the limitations (Windows, Linux without bwrap, ACP-client bash, Lua).
- [ ] The KB doc is written.

## Notes

Depends on: PANDO-US-0041, PANDO-US-0042, PANDO-US-0044, PANDO-US-0045, PANDO-US-0046.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
