---
id: PANDO-US-0070
type: story
title: "Fire session evaluation: define session-completion triggers, startup backfill and a manual command"
status: done
priority: critical
parent: PANDO-EP-0014
author: mcp
labels: [evaluator, session-lifecycle]
estimate: 5
created: 2026-09-29T20:33:19Z
updated: 2026-09-29T21:04:10Z
started: 2026-09-29T20:51:12Z
closed: 2026-09-29T21:04:10Z
---

## Description

`EvaluateSession` is only reachable through `session.EndSession()` (`internal/session/session.go:394-399`), and nothing in the codebase calls `EndSession` (only the interface method at `session.go:162` exists). With 745 local sessions and 0 `session_scores`, this is the single blocker to the whole loop.

Define what "session completed" means for Pando and wire every surface to it:

- **TUI**: switching session (`internal/tui/tui.go:786` `chat.SessionSelectedMsg`), creating a new session, and app exit. Evaluate the session being left.
- **WebUI / REST**: explicit close, and an idle timeout (no new message for N minutes, configurable, default 30m) evaluated by a background ticker in the primary instance only (see IPC primary-only services).
- **ACP**: session end / cancel-and-no-resume; idle timeout as above.
- **App shutdown**: synchronous flush with a short deadline so pending evaluations are not lost (respect `evaluator.async`).
- **Startup backfill sweep**: on start, when the evaluator is enabled, enqueue sessions with messages but no `session_scores`, oldest first, bounded per run (e.g. 50) and rate-limited; the judge is **off** for backfill unless `evaluator.backfillJudge = true`.
- **Manual**: `pando evaluate [session-id | --all --limit N]` CLI and a `/evaluate` slash command; keep `pando_evaluator_evaluate`.

Guards: skip sessions with fewer than 2 user turns; skip mesnada subagent/child sessions unless `evaluator.includeSubagents`; evaluation stays idempotent (`GetSessionScore` check already exists) and must be safe under the single-writer SQLite proxy when running as a secondary instance (delegate to primary over IPC or skip with a debug log).

## Acceptance Criteria

- [ ] Leaving a session in the TUI, closing/idling one in WebUI or ACP, and shutting the app down each produce exactly one `session_scores` row for that session.
- [ ] Startup on the developer DB backfills the existing unevaluated sessions in bounded batches without judge calls and without blocking startup.
- [ ] `pando evaluate <id>` and `--all` work and print the reward decomposition.
- [ ] An integration test drives a session through the agent with a fake provider, ends it via the new trigger and asserts a persisted score; `go test -race ./internal/evaluator ./internal/session ./internal/llm/agent ./internal/api` pass.
- [ ] KB document under `pando/changes/` describing the triggers and their configuration.

## Notes

`EndSession` also creates the "end" snapshot and runs `hook_session_end`; decide whether the new triggers call `EndSession` (all side effects) or a narrower `MarkCompleted` that only evaluates. Recommendation: narrower path plus an explicit `EndSession` for real closes, to avoid snapshot churn on every TUI switch.
