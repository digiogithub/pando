---
created_at: 2026-05-12T08:46:05.576838077Z
updated_at: 2026-09-29T21:52:08.132589725Z
tags:
    - docs
    - evaluator
    - validation
---
# Self-Improvement Manual Validation Guide

Design: [[pando/docs/self-improvement-system-analysis.md]]. Changes: [[pando/changes/evaluator-observability-doctor.md]]. All scenarios below use current commands and can be repeated in a scratch project.

## Setup
```toml
# .pando.toml of the scratch project
[evaluator]
enabled = true
model = "<a cheap model your provider serves>"
provider = "<its provider>"
minSessionsForUCB = 1
idleTimeout = "1m"
[evaluator.judge]
dailyCalls = 5
```
Use single backslashes in `correctionsPatterns`. Run `pando evaluator doctor` first: it must say `Evaluator: enabled`, list no pattern lint issues and show the eligible/never-evaluated counts.

## 1. Startup wiring
1. Start `pando` (TUI) or `pando serve` with debug logging.
2. Expect `evaluator: initializing self-improvement system`, `evaluator: self-improvement system initialized`. About 30 s later on the primary: `evaluator: startup backfill finished` and `evaluator doctor: evaluator enabled: ...`.
3. Expected failure signal: no such lines -> `pando evaluator doctor` explains why (disabled, no model, provider disabled).

## 2. Triggers score a session
1. In the TUI chat send two prompts (at least 2 user turns) in session A, then start a new session (or open another one).
2. Log shows `evaluator: starting session evaluation ... reason=...` then `evaluator: session evaluated`.
3. Cross-check: `pando evaluator doctor` shows `evaluated: 1`; `GET /api/v1/evaluator/sessions` returns the row with `components`.
4. Idle sweeper: leave a 2-turn session idle for `idleTimeout`; within a few minutes it is scored (`evaluator: idle sweep evaluated sessions`).
5. Backfill / manual: `pando evaluate --all --limit 5` scores old sessions and prints the reward decomposition; `pando evaluate <session-id>` scores one.

## 3. Reward explanation and feedback
1. Send a correction such as "that is wrong, undo that" as the 3rd turn in a session and evaluate it (`pando evaluate <id>`).
2. The output lists corrections >= 1 and components. WebUI Self-Improvement > Sessions tab: expand the row to see components, pattern hits and snippets. TUI evaluator page: press `s`.
3. Feedback: in WebUI chat `/feedback bad not what I asked`. The session is re-scored: reward <= 0.25 and the row shows `feedback: bad`.

## 4. Prompt variants
1. Create `.pando/prompts/variants/base/workflow/terse.md.tpl` (copy the embedded section and shorten it). Restart pando.
2. `pando evaluator doctor` lists the directory and the section as competing.
3. Start several new sessions and complete/evaluate them. `GET /api/v1/evaluator/templates` shows `times_used` and `avg_reward` for `default` and `terse`; the sessions view shows the variant each session ran with. The choice is frozen per session: it does not change between turns.

## 4b. Judge and budget
1. Have a session with >= 4 user turns and a decisive reward (feedback good/bad forces one) and evaluate it with `pando evaluate <id> --judge`, or wait for the sweeper.
2. Sessions view shows judge reasoning; `GET /api/v1/evaluator/metrics` shows `judge_calls` and tokens; doctor shows `budget today: N/5 calls`.
3. Exhaust the budget (`dailyCalls = 1`): doctor prints `(EXHAUSTED)` and the log warns once per day.

## 5. Learned skills
1. A judged session with a proposal (confidence >= 0.7) creates `.pando/skills/learned/<id>.md` with `status: pending`. `pando skills list --status pending` lists it; doctor counts it.
2. `pando skills approve <id>`. Start a NEW session: the skill is in its system prompt; sessions view shows it under "Skills injected". The session in progress is unchanged.
3. `pando skills reject <id>` removes it from later sessions.

## 6. Doctor and lint
1. Put `correctionsPatterns = ['(?i)\\bwrong\\b']` in the config: doctor prints a `double_backslash` issue with a fix hint, the WebUI shows a banner and the TUI shows a warning in the evaluator page (and in the status bar ~75 s after start).
2. Put `(unclosed`: doctor reports a `compile` issue (the evaluator will refuse to start with it).
3. `GET /api/v1/evaluator/doctor` returns the same report as JSON plus the printable text.

## 7. UI checks
- WebUI: Self-Improvement shows non-zero cards after one evaluated session, the 14-day chart, and Settings > Self-Improvement exposes idle timeout, backfill, subagents, judge bands and budget, variants switch and context trimmer.
- TUI: evaluator page shows the metrics header with sparkline; `s` toggles sessions/variants; settings page has the same keys with hints.

## Failure interpretation
- Nothing evaluated after switching sessions: fewer than 2 user turns, or child session (see `includeSubagents`); doctor "never evaluated" and log level debug show the skip reason.
- Sessions evaluated, no judge data: reward in the middle band, too few turns, budget exhausted, or judge init failed (doctor "last judge error").
- Variants never selected: section has only the default (no files), `templates.enabled = false`, evaluator disabled, or the section name does not match the builder render name.
- No skills: no judge, no proposals with confidence >= 0.7, or all pending (review them).

## Success criteria
Doctor is healthy (no warnings), evaluated sessions increase after normal use, the sessions view explains each score, variants and skills are attributed per session, and judge usage stays within the configured budget.
