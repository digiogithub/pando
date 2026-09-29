---
created_at: 2026-09-29T20:31:46.96184707Z
updated_at: 2026-09-29T21:52:40.190248932Z
tags:
    - analysis
    - evaluator
    - self-improvement
---
# Self-improvement system: status audit (2026-09-29)

Audit of the evaluator / UCB / skill-library loop after the user reported "I think it never worked". Result: confirmed. The loop is broken at every stage; the developer DB holds **745 sessions, 0 `session_scores`, 0 `skill_library`, 0 `prompt_ucb_stats`, 4 `prompt_templates`** with `evaluator.enabled = true` and a judge model configured (`openrouter.deepseek/deepseek-v4-flash-0731`).

Filed as epic in gintrack (PANDO, "Make the self-improvement loop actually run and be useful") with child stories. Builds on [[project_self_improvement_plan]], [[plans/self-improvement-engine-implementation-plan.md]] and [[pando/docs/self-improvement-system-analysis.md]] (the last one describes the intended flow as if it worked; treat it as design intent, not behaviour). Related decision: [[decision_no_user_memory_scope]] / PANDO-EP-0008 — no per-turn LLM calls, no unreviewable automatic prompt mutation.

## Findings (file:line)

| # | Stage | Finding |
|---|-------|---------|
| 1 | Trigger | `EvaluateSession` is only called from `session.EndSession()` (`internal/session/session.go:394-399`). **Nothing calls `EndSession`** — grep finds only the interface method at `session.go:162`. TUI session switch, WebUI/API, ACP session close, app shutdown: none end a session. Only `pando_evaluator_evaluate` (MCP tool) can produce a score. |
| 2 | Template identity | `seedEvaluatorTemplates` (`internal/app/app.go:1366-1412`) inserts `name="base/identity", section="base"`. `SelectTemplate` (`internal/evaluator/service.go`) filters `ListActiveTemplatesBySection(sectionName)` with the render name `"base/identity"` passed from `builder.go:245`. Never matches. Only 4 of the 9 listed sections seeded locally (others fail `Render(name, nil)`). |
| 3 | Template content | Seeded content is `registry.Render(s.name, nil)` — a snapshot rendered with nil data. When selected, `builder.go:250` returns `tmpl.Content` verbatim: skips `b.data` and the Lua `hook_template_section`. One variant per section, so UCB has nothing to compare anyway. |
| 4 | Attribution | `RecordTemplateSelection` stores one templateID per session in `sync.Map` (`service.go`) — called once per section (`builder.go:247`), last section wins. In-memory only; lost on restart. |
| 5 | Reward | `reward.go`: `S_success = 1 - 0.3*corrections`, corrections = regex over user text. Dev config pattern `(?i)\bno[,.]?\b` matches any "no". `S_tokens` baseline from `session_scores` (empty) → always neutral 0.5. Free signals ignored: tool errors, cancelled runs, repeated identical tool calls, explicit feedback. |
| 6 | Judge | `InsertSessionScore` writes `JudgeAnalysis`/`JudgeModel` NULL always. `buildTranscript` truncates only tool results (500 chars); user/assistant text unbounded. No budget. Judge fires when `reward > 0.5 || S_success == 1.0` — with the weak reward that is almost every session. |
| 7 | Skills | No query updates `skill_library.success_rate`; `DeactivateLowestSkill` orders by it → arbitrary eviction. Title `"<task_type> skill"`. `GetActiveSkills` increments `usage_count` per prompt build = per turn. New skill mid-session changes system prompt → prompt-cache miss (same class as PANDO-US-0036). |
| 8 | ContextTrimmer | `internal/evaluator/context_trimmer.go:124` sends a judge-model request at every new session start to filter tools; wired at `app.go:530-533` whenever evaluator + model exist. Not an evaluation feature; no separate flag. |
| 9 | Tests | `internal/evaluator/service_test.go` tests `EvaluateSession` directly (persist, idempotent, threshold, stats, eviction). No test drives session lifecycle → score. |
| 10 | Surfaces | TUI page `internal/tui/page/evaluator.go`, WebUI `web-ui/src/components/evaluator`, REST `internal/api/handlers_evaluator.go` (`/api/v1/evaluator/{metrics,templates,skills,sessions}`), settings `/api/v1/config/evaluator`. All render zeros. |

What does work in isolation: `calculateReward`, `UCBScore`, `renderJudgePrompt`/`parseJudgeOutput`, DB trigger `update_ucb_after_score`, `ClassifyTask` patterns, skill dedupe by word overlap, `MaxSkills` eviction.

## Proposed direction (epic stories)

1. **Fire the evaluation** (critical): define session-completion events (TUI switch/exit, API/WebUI close or idle timeout, ACP session end, app shutdown flush) → `EvaluateSession`; startup backfill sweep of unevaluated sessions (bounded, judge off for backfill); `pando evaluate` CLI / `/evaluate`; skip trivial and subagent sessions; e2e test.
2. **Honest cheap reward** (high): saner default patterns, count only user turns after the first; add persisted-data signals (tool errors, cancelled runs, repeated tool calls, run duration); explicit `/feedback good|bad` in TUI/WebUI/ACP as dominant signal; persist components; fixture tests from real sessions.
3. **Template variants as files + correct attribution** (high): `section == render name`; store template *source*, render with data + Lua; variants from `.pando/prompts/variants/<section>/<name>.md.tpl` (versioned, human-written); UCB only when ≥2 variants; per-session per-section selections in a DB table.
4. **Judge cost + persistence** (medium): decisive-only trigger, min messages, transcript head/tail cap, daily budget, persist reasoning/model/tokens, savings ledger.
5. **Learned skills as reviewable proposals** (medium): files with status pending/approved/rejected + provenance; inject approved only; frozen block per session; `success_rate` from later rewards; usage once per session; approve/reject in TUI/WebUI/CLI.
6. **Observability** (medium): pages with real data, `pando evaluator doctor`, startup diagnostic, docs refresh.
7. **ContextTrimmer decision** (low): own flag default off, or remove.

## Status: resolved by PANDO-EP-0014

All findings above were addressed on 2026-09-29. Where each one went:

| Finding | Resolution |
|---|---|
| 1 Trigger | [[pando/changes/evaluator-session-triggers.md]] (session switch/exit, ACP close, shutdown flush, idle sweeper, startup backfill, `pando evaluate`, `/evaluate`) |
| 5 Reward | [[pando/changes/evaluator-honest-reward.md]] (correction defaults, persisted signals, explicit feedback, stored components) |
| 6 Judge | [[pando/changes/evaluator-judge-gating.md]] (decisive-only, transcript cap, daily budget, persisted output) |
| 2, 3, 4 Templates | [[pando/changes/evaluator-template-variants.md]] (variant files, frozen per-session selection, per-variant stats) |
| 7 Skills | [[pando/changes/evaluator-reviewable-skills.md]] (reviewable files, approved-only frozen injection, success_rate) |
| 8 ContextTrimmer | [[pando/changes/evaluator-context-trimmer-flag]] (own opt-in flag, default off) |
| 9 Tests, 10 Surfaces | [[pando/changes/evaluator-observability-doctor.md]] (`pando evaluator doctor`, startup diagnostic, real data in TUI/WebUI/API, settings, dead-query cleanup, docs) |

Current design: [[pando/docs/self-improvement-system-analysis.md]]. Reproducible validation: [[pando/docs/self-improvement-manual-validation.md]].
