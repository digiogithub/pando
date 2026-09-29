---
created_at: 2026-09-29T21:06:40.250592941Z
updated_at: 2026-09-29T21:06:40.250592941Z
---
# ContextTrimmer behind its own opt-in flag (PANDO-US-0076)

Context: [[pando/analysis/self-improvement-status-2026-09.md]]

## What
The pre-session ContextTrimmer (one LLM call on the first message of each session to choose the relevant tools) was wired whenever the evaluator was enabled. It is now opt-in behind its own flag, default OFF.

## Config
`evaluator.contextTrimmer.enabled` (default false) and `evaluator.contextTrimmer.minConfidence` (default 0.7, was hardcoded 0.5). A per-trimmer model override was skipped; it reuses the evaluator judge model.

## Files
- internal/config/config.go: `ContextTrimmerConfig`, field on `EvaluatorConfig`, viper defaults, `EvaluatorWithDefaults`.
- internal/evaluator/service.go: `NewContextTrimmer()` returns nil unless the flag is on.
- internal/evaluator/context_trimmer.go: `MinConfidence()`, Info log `evaluator: context_trimmer usage` with token counts and `cost_category=evaluator`.
- internal/app/app.go: wiring is gated on the flag; the adapter uses `MinConfidence()`.
- web-ui/packages/pando-client/src/types/index.ts: `contextTrimmer` on `EvaluatorSettingsConfig`.
- internal/evaluator/triggers_test.go: `TestContextTrimmer_OptInOnly`.

## Cost accounting
The savings ledger (internal/llm/tools/savings_stats.go) tracks tool-output savings, so integrating there would be invasive. The trimmer cost is logged at Info under a stable key instead.

## Verification
`go build ./...`, `go vet`, and `go test -race` on evaluator, agent, app, config and api all pass. The new test checks that a fake provider gets zero calls with defaults and one call with the flag on.
