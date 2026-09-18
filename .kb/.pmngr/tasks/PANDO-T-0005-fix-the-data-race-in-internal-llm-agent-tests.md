---
id: PANDO-T-0005
type: task
title: Fix the data race between two internal/llm/agent tests
status: cancelled
priority: medium
author: claude
labels: [tests, concurrency]
estimate: 2
created: 2026-09-14T00:00:00Z
updated: 2026-09-18T12:40:00Z
---

## Description

`go test -race ./internal/llm/agent` reports a data race between
`TestRunResetsResurrectionCount` and `TestSessionModelIDFollowsOverride`: existing test code starts
a goroutine and never joins it, so it is still running when the next test begins.

Found on 2026-09-14 while delivering PANDO-T-0002; pre-existing and unrelated to it. The package
passes without `-race`, which is why it went unnoticed — and why it is worth fixing rather than
tolerating: a race in test scaffolding hides races in the code under test.

Note that `internal/agui`, `internal/config` and `internal/api` are all clean under `-race`.

## Acceptance Criteria

- [ ] `go test -race ./internal/llm/agent` is clean, including with `-count=2`.
- [ ] The goroutine is joined or given a lifetime bounded by the test, not silenced with a sleep.

## Notes

Sibling of PANDO-T-0003, which fixed the config-singleton leak in the same package. That package's
test scaffolding is where this repository's test-hygiene debt has collected.
