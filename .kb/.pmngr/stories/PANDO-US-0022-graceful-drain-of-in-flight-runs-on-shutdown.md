---
id: PANDO-US-0022
type: story
title: Graceful drain of in-flight runs on shutdown
status: backlog
priority: medium
parent: PANDO-EP-0004
milestone: PANDO-M-0001
author: claude
labels: [agui, ops]
estimate: 5
created: 2026-09-13T21:16:01Z
updated: 2026-09-13T21:16:01Z
---

## Description

As an operator, I want SIGTERM to let in-flight runs finish or checkpoint within a deadline, so that a rolling restart does not throw away every user's turn mid-answer.

Today shutdown is a hard cancel. `Listener.Shutdown` (`internal/agui/listener.go:57-59`) is `http.Server.Shutdown`, which does wait for handlers — but `Runtime.Close` (`internal/agui/runtime.go:91-101`) calls `finishRun` on **every** active run, then cancels the root context, and merely logs `"AG-UI adapter closing with runs still in flight"` when the pool is still busy. `agui-serve` compounds it: the listener gets a 5 s context (`cmd/agui_serve.go:188-192`) and the `defer runtime.Close()` fires after, so any agent turn longer than 5 s is lost.

Implement:

- a `[AGUI] ShutdownGrace` duration (default around 30 s, 0 = today's immediate cancel) resolved in `internal/agui/deps.go` with the other adapter defaults;
- a draining mode on `Runtime`: stop admitting new runs (return 503 + `Retry-After` from `handleRun`, reusing the rejection path from the `MaxConcurrentRuns` story), keep existing streams alive;
- `Runtime.Close` waits up to `ShutdownGrace` for active runs to finish, then falls back to the current hard cancel for whatever is left, logging how many were cut;
- runs that cannot finish in the deadline are **checkpointed**: their accumulated messages are persisted so the session is coherent on restart, rather than lost;
- suspended runs (parked on a permission prompt, up to 11 minutes) are not waited on — they are checkpointed and released immediately, since waiting for a human during a restart is never right;
- `cmd/agui_serve.go` gives the listener a context derived from the same grace instead of the hardcoded 5 s, and orders `listener.Shutdown` before `runtime.Close`.

Do NOT wait forever, and do NOT wait on suspended runs. Do NOT change run behaviour outside shutdown.

## Acceptance Criteria

- [ ] `[AGUI] ShutdownGrace` is configurable; 0 reproduces today's immediate cancel.
- [ ] SIGTERM with a run in flight: the run completes and its messages are persisted, asserted by reading the session's messages after the process exits.
- [ ] While draining, a new run POST gets 503 + `Retry-After` and no stream.
- [ ] A run that exceeds the grace is cancelled, its partial messages are persisted, and a log line reports the count of cut runs.
- [ ] A suspended run does not hold shutdown open; it is checkpointed and released.
- [ ] The draining flag is visible in `GET {path}/healthz` so a load balancer can take the instance out of rotation.
- [ ] No goroutine leak after shutdown, asserted with a leak check in `internal/agui/runtime_test.go`.

## Notes

Size: M. Depends on the `/healthz` story (draining flag) and reuses the 503 + `Retry-After` rejection path from the `MaxConcurrentRuns` story, so schedule it after both. Evidence: `report-agui-server.md` §5, Graceful shutdown row.
