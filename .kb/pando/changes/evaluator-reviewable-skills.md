---
created_at: 2026-09-29T21:38:45.345673096Z
updated_at: 2026-09-29T21:38:45.345673096Z
tags:
    - change
    - evaluator
    - skills
---
# Evaluator: reviewable learned skills (PANDO-US-0074)

Builds on [[pando/changes/evaluator-template-variants.md]] (same frozen-per-session + Go-side stats pattern) and [[pando/analysis/self-improvement-status-2026-09.md]]. Consistent with [[decision_no_user_memory_scope]]: repo files are the source of truth, humans review.

## Design
- Judge proposals (confidence >= 0.7) are written to `.pando/skills/learned/<id>.md` (id = slug of a generated title; the skills discovery loader only reads SKILL.md so no collision). Status `pending|approved|rejected`. Only `approved` is injected.
- `skill_library` mirrors status (`status`, `is_active = approved`) and keeps stats (`usage_count`, `eval_count`, `reward_total`, `success_rate`). Migration `20260929000004_add_reviewable_skills.sql` adds columns + `session_skill_injections(session_id, skill_id, position, injected_at)`. Legacy rows become `legacy`, inactive, counters reset; the next sync exports them as pending files.
- `SyncLearnedSkills` (files -> mirror, diff-only writes) runs at each new-session selection, on list and on review. Deleted file => mirror row `deleted`, inactive.
- `SessionSkills(ctx, sessionID, taskType)` (prompt interface method replacing `GetActiveSkills`): first build syncs, picks top approved (success_rate, usage), persists them (an empty-set marker row with skill_id '' when none), increments usage once per skill; later turns/processes read the persisted set. A skill approved mid-session only reaches the next session.
- success_rate: `ApplySessionRewardToSkillStats` in applySessionReward, `ApplyRewardDeltaToSkillStats` on re-score (feedback path).
- MaxSkills: approving beyond the cap rejects the lowest ranked approved skill; approved skills with >=5 evaluations and success_rate < 0.3 are rejected at proposal time. Rejected files feed dedupe (word overlap > 0.70 against pending/approved/rejected).

## File format
```
---
id: run-the-project-build-and-tests-before-reporting
title: Run the project build and tests before reporting a task as finished
status: pending
task_type: code
confidence: 0.85
source_session: <session id>
judge_model: gpt-5-mini
created: 2026-09-29T21:00:00Z
---
Run the project build and tests before reporting a task as finished, and quote the command output.
```

## Review workflow
- CLI: `pando skills list [--status pending]`, `pando skills approve <id>`, `pando skills reject <id>` (files only if DB unavailable).
- REST: `GET /api/v1/evaluator/skills[?status=]`, `POST /api/v1/evaluator/skills/{id}/approve|reject`.
- WebUI skills panel with pending/approved/rejected tabs and approve/reject buttons; TUI evaluator page: `a` approve / `x` reject in the skills panel; MCP `pando_evaluator_skills` gains `status` (default approved, `all`).

## Files
internal/evaluator/{learned_skills.go,service.go,types.go}, internal/db/{skills_review.go,models.go,self_improvement.sql.go,querier.go,sql/self_improvement.sql,migrations/20260929000004_*}, internal/ipc/dbproxy/{proxy,handlers}.go, internal/ipc/changepub/topics.go, internal/llm/prompt/builder.go, internal/app/app.go, internal/api/{handlers_evaluator,routes}.go, cmd/skills.go, internal/tui/{page/evaluator.go,components/evaluator/skills.go}, internal/llm/evaluatortools/evaluator_tool.go, web-ui evaluator components/store/types.

## Deferred
`autoApproveAfter` shadow mode; refresh of the frozen skill set after compaction (memory block does this; skills stay frozen for the whole session).

## Verification
go build ./...; go vet; `go test -race` over evaluator, prompt, agent, app, session, api, config, db, ipc, tui, cmd all pass; web-ui `tsc -p tsconfig.app.json --noEmit` clean. New tests: internal/evaluator/skills_review_test.go, internal/db/reviewable_skills_migration_test.go, byte-stability test in internal/llm/agent/memory_block_test.go.
