---
id: PANDO-US-0021
type: story
title: MaxConcurrentRuns cap with 503 and Retry-After
status: backlog
priority: high
parent: PANDO-EP-0004
milestone: PANDO-M-0001
author: claude
labels: [agui, ops, backpressure]
estimate: 5
created: 2026-09-13T21:16:01Z
updated: 2026-09-13T21:16:01Z
---

## Description

As an operator, I want a configurable cap on concurrent AG-UI runs that rejects the overflow with 503 and `Retry-After`, so that a dozen users pressing send does not turn into a dozen unbounded concurrent model calls and tool executions.

There is no cap today. `AgentPoolSize` (default 4, `internal/agui/deps.go:84`) caps *cached agent instances*, not in-flight runs: one `agent.Service` serves unboundedly many sessions concurrently, and `IsBusy` is only used for shutdown diagnostics. N simultaneous threads means N simultaneous LLM calls; the only backstop is the provider's rate limit. There is no queue, no 503, no `Retry-After`.

Add `[AGUI] MaxConcurrentRuns` (int, 0 = unlimited, keeping today's behaviour as the default so existing deployments do not change) to the AG-UI config and resolve it in `ConfigFromApp` (`internal/agui/deps.go:90-...`) alongside the other adapter defaults. Enforce it in `handleRun` **early**: the counter must be taken before a session is created, before the agent is built and before any thread binding is written, so a rejected request leaves no state behind. Release it on every exit path, including the suspend/resume path — a run parked on a permission prompt still occupies a slot (its session is pinned for up to 11 minutes, `internal/agui/frontend_tool.go:41` plus the reaper grace at `run.go:28`), and that must be a deliberate, documented choice rather than an accident.

Over the cap: respond 503 with a `Retry-After` header (seconds) and a plain error body; do not open an SSE stream and do not emit `RUN_ERROR`, since no run was started. Log the rejection with the current and maximum counts, and expose both in the `/healthz` payload.

Do NOT build a queue — reject, do not park. Do NOT count resumes of an already-admitted run against the cap a second time; a resume re-attaches to a run that already holds its slot (`server.go:356-399`).

## Acceptance Criteria

- [ ] `[AGUI] MaxConcurrentRuns = N` is read from config; 0 or unset preserves current unlimited behaviour.
- [ ] With the cap reached, a new run POST returns 503 with a numeric `Retry-After`, no SSE stream and no `RUN_ERROR` event.
- [ ] A rejected request creates no session, no agent instance and no `agui_threads` row — asserted by a test inspecting the store after the rejection.
- [ ] A suspended (interrupt-parked) run still holds its slot; a test drives a run to `RUN_FINISHED{outcome:"interrupt"}` and asserts the gauge is unchanged, and the documented behaviour matches.
- [ ] Resuming an admitted run is not rejected even when the cap is full.
- [ ] Slots are released on normal completion, error, client abort and reaping; a test runs the cap to its limit repeatedly with no leak.
- [ ] Current and maximum concurrent runs appear in `GET {path}/healthz` and in a structured log line on rejection.
- [ ] Tests in `internal/agui/server_test.go` / `run_test.go`.

## Notes

Size: M. Depends on the `/healthz` story of this epic for the observability criterion. The git-in-track embedding host imposes its own per-user in-flight cap in its proxy in the meantime. Evidence: `report-agui-server.md` §5, Concurrency limit row.
