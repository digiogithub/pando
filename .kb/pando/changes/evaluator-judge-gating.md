---
created_at: 2026-09-29T21:18:01.865163909Z
updated_at: 2026-09-29T21:18:01.865163909Z
tags:
    - change
    - evaluator
---
# Evaluator judge gating, transcript cap, daily budget (PANDO-US-0073)

Builds on [[pando/changes/evaluator-honest-reward.md]]; context in [[pando/analysis/self-improvement-status-2026-09.md]].

## What
- Decisive-only trigger: judge runs only if reward >= `evaluator.judge.highReward` (0.8) or <= `lowReward` (0.3) AND user turns >= `minTurns` (4); never with SkipJudge (backfill unless backfillJudge, feedback re-scores). `shouldJudge` / `maybeJudge` in `internal/evaluator/service.go`.
- Transcript cap: `buildCappedTranscript` keeps head (40%) + tail (60%), middle replaced by `[... N messages elided ...]`; budget = `maxTranscriptTokens` (6000) minus the rendered prompt boilerplate, so the whole prompt stays under the cap. Token estimate reuses `skills.EstimateTokens` (~4 chars/token).
- Daily budget: `dailyCalls` (20) and `dailyTokens` (200000); 0 = unlimited. Counted from persisted `session_scores` (judge_model NOT NULL, created_at >= local midnight; new query `GetJudgeUsageSince`) so it survives restarts. Judge calls are serialised by `judgeMu`. Exhausted: score persisted, judge skipped, one warning per local day (in-memory day marker).
- Persistence: `judge_analysis` (JSON of JudgeOutput: reasoning, key_points, new_skill, task_type, confidence), `judge_model`, new `judge_prompt_tokens`/`judge_completion_tokens` (migration `20260929000002_add_session_score_judge_tokens.sql`). Written by new `UpdateSessionScoreJudge` after insert; it never touches `reward`, so UCB triggers stay inert. Tokens come from provider usage, estimated when absent.
- `EvaluatorService.LastJudgeError()` returns `*JudgeError{Message, At}` (init or call failure) for the future `pando evaluator doctor` (US-0075).
- API: `/api/v1/evaluator/sessions` returns `judge_analysis` (null if none), `judge_model`, `judge_prompt_tokens`, `judge_completion_tokens`. TS types updated (`EvaluatorSessionScore`, `EvaluatorJudgeAnalysis`, settings `judge`). No WebUI session detail component exists under web-ui/src/components/evaluator, so no UI added.
- Not done: savings-ledger integration (tokens live in session_scores).

## Files
internal/config/config.go (JudgeConfig, viper defaults, JudgeWithDefaults), internal/evaluator/{service,judge}.go, internal/db/{db,models,querier,self_improvement.sql.go,sql/self_improvement.sql}, migration above, internal/ipc/dbproxy/{proxy,handlers}.go (UpdateSessionScoreJudge write), internal/api/handlers_evaluator.go, web-ui pando-client types. Tests: internal/evaluator/{judge_gating_test,judge_bands_test}.go; triggers_test and test schema updated.

## Verification
go build ./...; go vet touched pkgs; go test -race evaluator, session, api, config, db, ipc, cmd all pass; web-ui tsc clean.
