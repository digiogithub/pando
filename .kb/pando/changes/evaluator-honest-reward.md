---
created_at: 2026-09-29T21:12:40.400013703Z
updated_at: 2026-09-29T21:12:40.400013703Z
---
# Evaluator honest, cheap reward (PANDO-US-0071)

Context: [[pando/changes/evaluator-session-triggers.md]], [[pando/analysis/self-improvement-status-2026-09.md]], [[PANDO-EP-0014]].

## What
Session reward reworked with NO extra LLM call.
- Corrections (`internal/evaluator/reward.go`): defaults in `config.DefaultCorrectionsPatterns()` no longer contain bare negations or "vuelve a"; added undo/revert/still failing/eso no es lo que pedi/sigue fallando etc. First user turn ignored; correction right after an assistant turn = 0.35 penalty, otherwise 0.2; total penalty capped at 0.8. Legacy bare-negation pattern `(?i)\bno[,.]?\b` in existing configs is dropped in `compilePatterns`. Fixture test `TestCorrectionPatternsFixture` (23 turns ES/EN).
- Persisted signals, all derived from `messages` parts (ToolCall / ToolResult.IsError / Finish reason), each in [0,1] and weighted; unavailable components are excluded and weights renormalised: `toolErrors` (1 - errors/calls), `cancels` (finish=canceled, 2+ = 0), `repetition` (identical name+input calls), `turns` (free up to 4 user turns, linear decay over 20), `endState` (0 when last assistant/tool activity was an error). `tokens` and `success` as before.
- NOT implemented: wall time (sessions.updated_at is bumped by a trigger on every update and idle gaps pollute it; no reliable start/end), savings-ledger signal (not linked to sessions in a stable way), AgentEventTypeError events (transient pubsub events, not persisted; finish=error/canceled in messages covers it).
- Explicit feedback: `EvaluatorService.RecordFeedback(ctx, sessionID, good|bad, note)`. Stored as an `events` row (subject `session_feedback:<id>`, JSON metadata, no embedding/FTS), then `EvaluateNow(Force, Rescore, SkipJudge)`. bad => total <= 0.25, good => total >= 0.85. Latest feedback wins. Surfaces: WebUI `/feedback good|bad [note]` (handlers_chat.go + commands registry), `POST /api/v1/evaluator/sessions/{id}/feedback`, TUI command palette "Session Feedback: good/bad". Not done: ACP, WebUI thumbs, typed /feedback in TUI editor.
- Re-score: `UpdateSessionScore` UPDATEs the row in place. UCB: migration `20260929000001_add_session_score_components.sql` adds trigger `update_ucb_after_rescore` (AFTER UPDATE OF reward) that applies only the reward delta to prompt_ucb_stats and never touches times_used, so no double count. The insert trigger is unchanged. US-0072 reworks UCB attribution.
- Decomposition: `session_scores.components` TEXT (JSON `Breakdown`: components, weights, patternHits, feedback, counters, baseline, weightedTotal). Returned by `/api/v1/evaluator/sessions` (`components`) and typed in `web-ui/packages/pando-client/src/types/index.ts` (`EvaluatorSessionScore`).
- Baseline: new query `GetSessionsTokenBaseline` over `sessions` (last N=maxTokensBaseline root sessions with message_count >= 4 and tokens > 0, excluding current). GLOBAL, not per task type: task classification needs the first user message of each past session (one message load each).
- Config: `evaluator.weights` (success, tokens, toolErrors, cancels, repetition, turns, endState). Unset => alphaWeight/betaWeight become success/tokens (0.8/0.2) and other components take `DefaultEvaluatorWeights()` (0.5/0.15/0.1/0.05/0.05/0.05/0.1 when fully defaulted). No viper default for weights on purpose (needed to detect "unset"); `EvaluatorWithDefaults` resolves and exposes them in `/api/v1/config/evaluator`; corrections patterns also defaulted there. TUI alpha/beta edits also set Weights.Success/Tokens. WebUI settings shows the 7 sliders.

## Files
internal/evaluator/{reward,service,types}.go, tests reward_test.go, feedback_test.go, service_test.go (schema); internal/db/migrations/20260929000001_*.sql, internal/db/sql/self_improvement.sql, hand-written internal/db/{self_improvement.sql.go,db.go,querier.go,models.go}; internal/ipc/dbproxy/{proxy,handlers}.go (write proxy for UpdateSessionScore, InsertSessionFeedbackEvent); internal/config/config.go (+ evaluator_weights_test.go); internal/api/{handlers_evaluator,handlers_chat,routes}.go; internal/commands/registry.go; internal/tui/{tui.go,page/settings.go}; web-ui types, extensionsStore, EvaluatorSettings.tsx.

## Verification
`go build ./...`; go vet on touched packages; `go test -race ./internal/evaluator ./internal/session ./internal/api ./internal/config ./internal/db/... ./internal/ipc/... ./internal/commands ./internal/tui/... ./cmd` pass; agui, app pass; web-ui `npx tsc -p tsconfig.app.json --noEmit` clean.
Note: `.pando.toml` dev patterns use `'...\\b...'` (double backslash in single quotes), which are inert literals; left untouched.
