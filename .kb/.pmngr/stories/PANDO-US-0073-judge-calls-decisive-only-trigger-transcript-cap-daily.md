---
id: PANDO-US-0073
type: story
title: "Judge calls: decisive-only trigger, transcript cap, daily budget and persisted output"
status: done
priority: medium
parent: PANDO-EP-0014
author: mcp
labels: [evaluator, judge, cost]
estimate: 3
created: 2026-09-29T20:33:19Z
updated: 2026-09-29T21:19:11Z
started: 2026-09-29T21:13:38Z
closed: 2026-09-29T21:19:11Z
---

## Description

The LLM judge (`internal/evaluator/judge.go`, called from `service.go` `runEvaluation`) fires whenever `reward > 0.5 || S_success == 1.0` (almost every session with the current weak reward), sends a transcript bounded only for tool results (500 chars), and its output is thrown away: `InsertSessionScore` writes `JudgeAnalysis` and `JudgeModel` as NULL.

- Trigger only when the session is decisive: very high or very low reward (configurable bands), at least N user turns, and never for backfill unless enabled.
- Cap the transcript by tokens (head + tail, middle elided with a marker), reuse the token estimator used by the context window logic.
- Daily budget in calls and estimated tokens (`evaluator.judge.dailyBudget`); when exhausted, log once and skip.
- Persist `judge_analysis` (reasoning + key points JSON), `judge_model`, prompt/completion tokens of the judge call, and record the call in the savings ledger as evaluator cost.
- Respect the provider abstraction already used (`provider.NewProvider`); make judge failures visible in `pando evaluator doctor`.

## Acceptance Criteria

- [ ] A session with reward 0.55 and 3 turns does not call the judge; one with reward 0.15 and 6 turns does.
- [ ] A 200-message transcript sent to a fake provider stays under the configured token cap and contains the elision marker.
- [ ] After the daily budget is spent, further evaluations persist scores but skip the judge, with a single warning per day.
- [ ] `session_scores.judge_analysis` and `judge_model` are populated and shown in the WebUI session detail.
- [ ] `go test -race ./internal/evaluator` passes; KB change document written.
