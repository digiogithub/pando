---
created_at: 2026-05-30T15:28:21.20920331Z
updated_at: 2026-09-29T21:52:07.985227228Z
tags:
    - docs
    - evaluator
    - self-improvement
---
# Self-improvement system (shipped design, PANDO-EP-0014)

Status: describes what ships after PANDO-EP-0014 (2026-09-29). The pre-epic analysis is in [[pando/analysis/self-improvement-status-2026-09.md]]. Per-story detail: [[pando/changes/evaluator-session-triggers.md]], [[pando/changes/evaluator-honest-reward.md]], [[pando/changes/evaluator-judge-gating.md]], [[pando/changes/evaluator-template-variants.md]], [[pando/changes/evaluator-reviewable-skills.md]], [[pando/changes/evaluator-context-trimmer-flag]], [[pando/changes/evaluator-observability-doctor.md]]. Validation steps: [[pando/docs/self-improvement-manual-validation.md]].

Principle (see [[decision_no_user_memory_scope]]): no per-turn LLM calls, no unreviewable automatic prompt mutation. Prompt variants are human-authored files; learned skills are files a human approves.

## Lifecycle

1. **Triggers.** A session is scored when the TUI switches away from it or exits, an ACP session closes, the app shuts down (flush), the idle sweeper finds it idle for `idleTimeout`, the startup backfill picks it up, or explicitly (`pando evaluate`, `/evaluate`, `POST /api/v1/evaluator/sessions/{id}/evaluate`, MCP tool). Guards (one place, `EvaluatorService.runEvaluation`): already scored -> skip; child session -> skip unless `includeSubagents`; fewer than 2 user messages -> skip (explicit calls bypass turn/subagent guards, never idempotency). Sweeper and backfill run only on the primary instance (they follow failover promotion).
2. **Reward (no LLM).** Weighted mean of the components available for the session, weights renormalised: `success` (correction regexes over user turns after the first), `tokens` (vs recent baseline), `toolErrors`, `cancels`, `repetition`, `turns`, `endState`. Explicit `/feedback good|bad` overrides (bad <= 0.25, good >= 0.85) and re-scores in place. The decomposition is stored as JSON in `session_scores.components`.
3. **Judge gating.** The LLM judge runs only for decisive sessions: reward >= `judge.highReward` or <= `judge.lowReward`, at least `judge.minTurns` user turns, transcript capped to `judge.maxTranscriptTokens` (head + tail), within the daily budget (`dailyCalls`, `dailyTokens`, counted from persisted scores). Never during backfill unless `backfillJudge`, never on re-scores. Output (reasoning, key points, task type, confidence, skill proposal), model and tokens are persisted.
4. **Variant stats.** Each prompt section with variant files picks one variant per (session, section), frozen in `session_template_selections`. On first evaluation the session reward is added to `prompt_variant_stats` for the variants it ran with; selection uses least-used until `minSessionsForUCB` evaluated sessions, then UCB1.
5. **Skill proposals and review.** The judge may propose a rule (confidence >= 0.7). It is written as `.pando/skills/learned/<id>.md` with status `pending`. Only `approved` skills are injected, as a block frozen per session (approval reaches the next new session). Each injected skill accrues `success_rate` from the rewards of the sessions it ran in; underperformers are auto-rejected.

## Config reference (`[evaluator]`, defaults)

| Key | Default | Meaning |
|---|---|---|
| `enabled` | false | Master switch. Also disabled at load time when no model resolves. |
| `model`, `provider` | coder model | Judge model; seeded from the coder agent on first run. |
| `async` | true | Evaluate in the background. |
| `weights.*` (`alphaWeight`/`betaWeight` legacy) | success 0.5, tokens 0.15, toolErrors 0.1, cancels/repetition/turns 0.05, endState 0.1 | Relative reward weights. |
| `correctionsPatterns` | built-in list | Regexes flagging user corrections. Use single backslashes. |
| `taskPatterns` | built-in list | Regex -> task type for `ClassifyTask`. |
| `explorationC` | 1.41 | UCB1 exploration. |
| `minSessionsForUCB` | 5 | Evaluated sessions per section before UCB replaces least-used. |
| `maxTokensBaseline` | 50 | Recent sessions used as token baseline. |
| `maxSkills` | 100 | Cap of approved skills. |
| `idleTimeout` | 30m | Idle time before the sweeper scores a session. |
| `backfillLimit` | 50 | Old sessions scored at startup (negative disables). |
| `backfillJudge` | false | Run the judge during backfill. |
| `includeSubagents` | false | Also score child sessions. |
| `judge.highReward` / `judge.lowReward` | 0.8 / 0.3 | Decisive bands. |
| `judge.minTurns` | 4 | Minimum user turns for a judge call. |
| `judge.maxTranscriptTokens` | 6000 | Transcript cap. |
| `judge.dailyCalls` / `judge.dailyTokens` | 20 / 200000 | Daily budget, 0 = unlimited. |
| `templates.enabled` | true | Kill switch for variant selection (inert without variant files). |
| `contextTrimmer.enabled` / `.minConfidence` | false / 0.7 | Opt-in extra LLM call per new session that filters tools. |
| `judgePromptTemplate` | built-in | Custom judge prompt path. |

All keys are editable in WebUI Settings > Self-Improvement and the TUI settings page (with help text).

## Surfaces

- CLI: `pando evaluate [id | --all --limit N] [--judge]`, `pando skills list|approve|reject`, `pando evaluator doctor [--json]`.
- REST: `/api/v1/evaluator/{metrics,templates,skills,sessions,doctor}`, `POST .../skills/{id}/approve|reject`, `POST .../sessions/{id}/evaluate|feedback`, `/api/v1/config/evaluator`.
- WebUI Self-Improvement view: doctor banner, metric cards (judge usage), 14-day evaluations chart, variants ranking, skills review, sessions tab (score components, correction hits, variants, skills, judge reasoning, feedback, open in chat).
- TUI evaluator page: metrics header with 14-day sparkline and doctor warning; `s` toggles variants / recent sessions (reward, corrections, top components, variants/skills, judge); `a`/`x` review skills. A status-bar warning appears ~75s after start when nothing was evaluated recently.
- Startup: after the first background pass on the primary, the doctor summary is logged at Info (warnings at Warn).

## Authoring variants

Create `.pando/prompts/variants/<section>/<name>.md.tpl` (project) or `~/.config/pando/prompts/variants/...` (global; project wins). `<section>` is the exact render name of the prompt section (for example `base/workflow`). The file is a normal Go template using the same data and Lua hooks as the embedded one. `default.md.tpl` is ignored (the embedded template is the implicit `default`). A section with the default plus at least one file is A/B tested. Check with `pando evaluator doctor` (sections listed as competing) and the WebUI ranking.

## Reviewing skills

`pando skills list --status pending`, read the file under `.pando/skills/learned/`, edit if wanted, then `pando skills approve <id>` or `reject <id>` (or the buttons in WebUI/TUI). Approval applies to sessions started afterwards.

## Troubleshooting with the doctor

Run `pando evaluator doctor` (or open the WebUI banner "Show report", or `GET /api/v1/evaluator/doctor`). It reports enablement and why not (disabled, missing model, provider missing/disabled/no key), eligible vs never-evaluated sessions, the last evaluation and its error (in-memory, only in a running instance), judge budget today and last judge error, variant directories and competing sections, skills by status, context trimmer state, sweeper/backfill state, and lints patterns. Typical findings:

- "never evaluated: N, last evaluation never": triggers did not fire; run `pando evaluate --all --limit 20` and check the log.
- Pattern lint `double_backslash`: a TOML single-quoted `'\\bwrong\\b'` keeps two backslashes and matches a literal backslash. Use `'(?i)\bwrong\b'` or a double-quoted `"(?i)\\bwrong\\b"`.
- Judge never runs: reward not in a decisive band, fewer than `minTurns`, budget exhausted, or judge init error (see doctor).
- Task type is not stored with a score (only inside the judge analysis), so metrics show mean reward per day, not per task type.
