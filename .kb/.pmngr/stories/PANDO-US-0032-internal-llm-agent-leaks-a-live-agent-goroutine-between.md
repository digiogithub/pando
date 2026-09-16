---
id: PANDO-US-0032
type: story
title: internal/llm/agent leaks a live agent goroutine between tests, so the package cannot run under -race
status: backlog
priority: high
parent: PANDO-EP-0004
milestone: PANDO-M-0001
author: claude
labels: [testing, agent, race]
estimate: 3
created: 2026-09-16T16:01:10Z
updated: 2026-09-16T16:01:10Z
---

## Description

As a contributor, I want `go test -race ./internal/llm/agent` to pass, so that the command AGENTS.md prescribes for agent changes actually gates them instead of failing on a defect unrelated to the change under test.

`--- FAIL: TestSessionModelIDFollowsOverride: race detected`. The write is `config.SetForTests` (`internal/config/config.go:4322`) called from `setup_bridge_model_test.go:41`; the read is `effectiveContextWindow` (`internal/llm/agent/agent.go:1514`) reached from a goroutine that an earlier test's agent run started at `agent.go:939` and that is still alive after that test returned. The two tests do not overlap logically — they overlap because a finished `Run`/`Resume` keeps a goroutine reading process-global configuration.

Reproduced on 2026-09-16 with every file of the `PANDO-US-0031` branch reverted to `710a39281`, so it predates that work and is not caused by it. It is a test-isolation defect, but the leak it exposes is real: a run that has answered still has a goroutine touching globals, which in production means a cancelled or finished turn can outlive its session.

Do NOT fix it by serialising the package with `-p 1` or by dropping `-race` from the documented command: that hides the leak rather than closing it.

## Acceptance Criteria

- [ ] `go test -race -count=1 ./internal/llm/agent` passes reliably (at least 20 consecutive runs).
- [ ] The goroutine an agent run starts at `agent.go:939` terminates before `Run`/`Resume` is observable as finished, or is owned by a context the test can cancel and wait on; whichever route is taken is stated in a comment.
- [ ] A test asserts the absence of the leak directly (for example `goleak` or an explicit wait on the run's lifecycle), so a regression fails loudly instead of appearing as a flaky race in an unrelated test.
- [ ] `config.SetForTests` and the agent's configuration reads are no longer racy under the package's own test suite.

## Notes

Found while closing PANDO-US-0031. Size S/M. Evidence in `.kb/pando/fixes/agui_mcp_tool_permission_binding.md`; the fix branch's own packages (`internal/permission`, `internal/agui`, `internal/api`) are green under `-race`, so this is confined to `internal/llm/agent`.
