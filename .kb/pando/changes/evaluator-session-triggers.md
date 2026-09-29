---
created_at: 2026-09-29T21:03:27.198018562Z
updated_at: 2026-09-29T21:03:27.198018562Z
---
# Evaluator session-completion triggers (PANDO-US-0070)

Context: [[pando/analysis/self-improvement-status-2026-09.md]], [[PANDO-EP-0014]].

## Problem
`evaluator.EvaluateSession` was only reachable via `session.EndSession()`, which nothing called: 745 sessions, 0 rows in `session_scores`.

## Design
Narrow evaluate-only path `evaluator.Service.MarkCompleted(ctx, id, reason)` (no snapshot, no hook_session_end). Package function `session.MarkCompleted(ctx, id, reason)` delegates to the global evaluator; `EndSession` now calls it too. Guards live in ONE place (`EvaluatorService.runEvaluation`): already scored -> skip; child (parent_session_id) session -> skip unless `includeSubagents`; <2 user messages -> skip; in-flight set dedupes concurrent triggers. `EvaluateSession` (MCP tool) and `EvaluateNow(..., Force:true)` bypass the turn/subagent guards but never idempotency.

## Changes
- `internal/evaluator/service.go`: dispatch/begin/end (dedupe), `Flush(ctx)` (WaitGroup over async runs), `EvaluateNow` (sync, returns `Result`), `runEvaluation(ctx,id,EvaluateOptions)` with guards and `SkipJudge`.
- `internal/evaluator/sweeper.go`: `Sweep` (unscored, idle, oldest first, bounded, rate-limited, failed sessions not retried in-process), `RunBackground` (30s start delay, one-shot backfill, then idle sweep every clamp(idle/2, 1m, 5m); only acts when `isPrimary()`).
- `internal/evaluator/types.go`: `EvaluateOptions`, `Result` (+`Summary()`).
- SQL: new query `ListUnscoredSessions` (sessions.sql; sqlc output hand-written in sessions.sql.go, db.go, querier.go since sqlc is not installed).
- `internal/app/app.go`: starts `RunBackground` (ctx-cancelled on shutdown, `ownsDBWriter` = IPCIsPrimary || no ipcClient, so it follows failover promotion); `Shutdown` flushes evaluator with 5s deadline.
- TUI (`internal/tui/tui.go`, `cmd/root.go`): evaluate the session being LEFT on `SessionSelectedMsg` and `SessionClearedMsg` (new session), and the open session at app exit; skipped while the agent is busy on it. "Evaluate Session" palette command.
- ACP `CloseSession` -> `MarkCompleted` via optional interface (adapter in cmd/root.go).
- REST/WebUI: `/evaluate [session-id]` slash command (registry + `handleSlashCommandStream`), `POST /api/v1/evaluator/sessions/{id}/evaluate`.
- CLI: `pando evaluate [session-id | --all --limit N] [--judge]` (cmd/evaluate.go) prints reward decomposition; judge off by default.
- Secondary instances: triggers use the normal DB path (writes proxied); sweeper/backfill skip with debug log unless primary.

## Config (`[evaluator]`)
- `idleTimeout` (default "30m"), `backfillLimit` (default 50, negative disables), `backfillJudge` (false), `includeSubagents` (false). Also in `EvaluatorWithDefaults`, JSON of `/api/v1/config/evaluator`, and WebUI type.

## Not implemented
- ACP `/evaluate` slash command (ACP has its own command spec table with tests; use CLI/REST/TUI).
- No explicit REST "close session" endpoint exists; idle sweeper covers WebUI.

## Verification
`go build ./...`; `go vet` on touched packages; `go test -race ./internal/evaluator ./internal/session ./internal/llm/agent ./internal/api ./internal/app ./internal/config ./internal/tui/... ./internal/commands ./internal/agui ./internal/ipc/... ./cmd` pass. New tests: `internal/evaluator/triggers_test.go` (guards, idempotency/dedupe, async+Flush, sweep selection, ordering/limit, judge off), `internal/session/evaluator_trigger_test.go` (session-service-level integration on real migrations: persisted score, single row, sweep). `internal/mesnada/acp` has pre-existing flaky race failures (identical on a clean HEAD export).
