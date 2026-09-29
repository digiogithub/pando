---
id: PANDO-US-0075
type: story
title: "Observability: real data in TUI/WebUI evaluator pages, `pando evaluator doctor`, startup diagnostic and docs refresh"
status: done
priority: medium
parent: PANDO-EP-0014
author: mcp
labels: [evaluator, observability, tui, webui, docs]
estimate: 5
created: 2026-09-29T20:33:19Z
updated: 2026-09-29T21:53:24Z
started: 2026-09-29T21:39:35Z
closed: 2026-09-29T21:53:24Z
---

## Description

The evaluator pages (`internal/tui/page/evaluator.go`, `web-ui/src/components/evaluator`, `/api/v1/evaluator/{metrics,templates,skills,sessions}` in `internal/api/handlers_evaluator.go`) have only ever rendered zeros, and nothing told the user that the loop was not running. The system must be able to explain itself.

- **Diagnostic**: `pando evaluator doctor` and a startup check that reports: enabled/disabled and why (missing model, provider disabled), number of sessions without a score, last evaluation time, judge budget state, variants directory found, pending skills, and the last evaluation error. Surface the same as a one-line warning in TUI/WebUI when enabled and nothing has been evaluated in the last N sessions.
- **Sessions view**: list recent sessions with reward, components, corrections, template variants used, judge reasoning, feedback; link to open the session.
- **Metrics**: evaluations per day, avg reward trend per task type, judge cost from the savings ledger.
- **Docs**: rewrite `pando/docs/self-improvement-system-analysis.md` to the shipped design, update `pando/docs/self-improvement-manual-validation.md` so each scenario is reproducible, and document configuration keys in the settings help text.
- **Tests**: API handler tests with a seeded DB; TUI height regression test kept.

## Acceptance Criteria

- [ ] On the developer DB before the other stories land, `pando evaluator doctor` prints "enabled, 745 sessions never evaluated, no trigger fired" (or equivalent) — this story's diagnostic may ship first.
- [ ] WebUI and TUI show non-zero metrics after one evaluated session, with the score explanation visible.
- [ ] Docs updated and linked from the KB analysis `pando/analysis/self-improvement-status-2026-09.md`.
- [ ] `go test -race ./internal/api ./internal/tui/...` pass; `web-ui` `tsc` clean; KB change document written.
