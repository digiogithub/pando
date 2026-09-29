---
created_at: 2026-09-29T21:52:40.313845742Z
updated_at: 2026-09-29T21:52:40.313845742Z
tags:
    - change
    - evaluator
    - observability
---
# Evaluator observability, doctor and docs (PANDO-US-0075)

Last story of PANDO-EP-0014. Context: [[pando/analysis/self-improvement-status-2026-09.md]]. Builds on [[pando/changes/evaluator-session-triggers.md]], [[pando/changes/evaluator-honest-reward.md]], [[pando/changes/evaluator-judge-gating.md]], [[pando/changes/evaluator-template-variants.md]], [[pando/changes/evaluator-reviewable-skills.md]], [[pando/changes/evaluator-context-trimmer-flag]]. Design and validation: [[pando/docs/self-improvement-system-analysis.md]], [[pando/docs/self-improvement-manual-validation.md]].

## What changed
- `internal/evaluator/doctor.go`: `Diagnose(ctx, DiagnoseOptions)` returns a `Report` (enablement and why, eligible/never-evaluated sessions, recent window, last evaluation and in-memory error, judge budget today and last judge error, variant dirs and competing sections, skills by status, trimmer, background state) plus `LintPatterns` (compile errors and literal double backslash, e.g. TOML `'\\b'`), `Text()`, `Summary()`, `ProblemLine()`. Read-only.
- `internal/evaluator/observability.go`: `RecentSessionDetails` (components, corrections, variants, skills, judge), `DailyMetrics` (14 zero-filled days), `Service.Diagnose`. `Stats` gained `RecentSessions`, `Daily`, `Problem`.
- `service.go` / `sweeper.go`: last evaluation error, `BackgroundState()`, `SetFirstPassHook` (startup diagnostic hook run after the first backfill+sweep on the primary).
- SQL (read-only, hand-written sqlc style): `internal/db/sql/evaluator_observability.sql` + `internal/db/evaluator_observability.sql.go`, `Querier` entries. No migration, no dbproxy entries (reads only).
- CLI `cmd/evaluator.go`: `pando evaluator doctor [--json]` (parent `evaluator`; `pando evaluate` unchanged).
- API: `GET /api/v1/evaluator/doctor`; `/metrics` adds `daily`, judge totals, `problem`; `/sessions` adds title, corrections, pattern_hits, feedback, variants, skills.
- App: doctor summary logged at Info (warnings at Warn) after first background pass. TUI: status-bar warning ~75 s after start; evaluator page shows metrics sparkline, judge usage, problem banner, `s` toggles recent-sessions table; panel content clamped (the old height test only exercised the loading view, so the real view overflowed with many skills).
- WebUI: `DoctorBanner`, `DailyChart`, `SessionsList` (open in chat via `setActiveSession` + `/chat`), tabs, judge card; Settings exposes idleTimeout, backfill, includeSubagents, judge bands/budget, templates.enabled, contextTrimmer. TUI settings page has the same keys with hints.
- Cleanup: removed unused queries InsertPromptTemplate, GetPromptTemplate, ListActiveTemplatesBySection, CountPromptTemplates, GetTokenBaseline, GetUCBStats, ListUCBRanking, DeactivateLowestSkill from the .sql, generated Go, db.go prepared statements, Querier, dbproxy and changepub topics.
- Not stored: task type per score, so metrics are mean reward per day (task type exists only inside judge analysis).

## Verification
`go build ./...`, `go vet` on touched packages, `go test -race` on evaluator, api, tui/..., app, config, db/..., ipc/..., cmd, session; new tests: `doctor_test.go` (double-backslash lint, counts, disabled reasons), `handlers_evaluator_observability_test.go` (seeded migrated DB for sessions/metrics/doctor), TUI page height test now runs the real view. `web-ui` `tsc -p tsconfig.app.json --noEmit` clean. `pando evaluator doctor` run on the developer DB: enabled, 265 eligible sessions never evaluated, 8 double-backslash pattern issues.
