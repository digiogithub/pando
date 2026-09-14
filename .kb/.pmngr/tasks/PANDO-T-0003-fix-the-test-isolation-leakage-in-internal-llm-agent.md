---
id: PANDO-T-0003
type: task
title: Fix the test-isolation leakage in internal/llm/agent
status: done
priority: high
labels: [tests, ci, config]
estimate: 3
author: claude
created: 2026-09-14T00:00:00Z
updated: 2026-09-14T00:00:00Z
---

## Description

`go test ./internal/llm/agent` fails four tests when the package runs as a whole and passes every
one of them in isolation: `TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`,
`TestCavemanSessionPolicyInstructions` and `TestApplyToolDiscoveryWithoutManagerIsUnchanged`.

Six independent agents implementing unrelated stories on 2026-09-14 each hit this, each spent time
confirming it was not theirs, and each reported it separately. It is process-global state leaking
between tests — the pattern matches the one already recorded for `internal/config`, where a test
calling `Load()` must call `isolateGlobalConfig(t)` first, and it is worth checking whether the
same fix applies here.

The cost is not the four failures. It is that `go test ./...` is not a usable signal, so every
change to this repository is verified against a package list that deliberately excludes this one.

## Acceptance Criteria

- [ ] `go test ./internal/llm/agent` passes with the whole package running, and with `-count=2`.
- [ ] The leaking global is named in the fix, not worked around by reordering or skipping tests.
- [ ] If the cause is the same global-config leak as `internal/config`, the guard is applied the
      same way rather than reinvented.

## Notes

Found while delivering PANDO-M-0001 and PANDO-M-0002, not caused by them. Not part of either
milestone; filed so it stops being rediscovered.
