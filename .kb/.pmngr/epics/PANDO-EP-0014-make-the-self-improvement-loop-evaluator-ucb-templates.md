---
id: PANDO-EP-0014
type: epic
title: Make the self-improvement loop (evaluator, UCB templates, learned skills) actually run and be useful
status: backlog
priority: high
author: mcp
labels: [evaluator, self-improvement, prompt, reliability]
created: 2026-09-29T20:31:46Z
updated: 2026-09-29T20:31:46Z
---

## Description

The self-improvement system (`internal/evaluator/`, `internal/llm/prompt/builder.go`, `internal/session/session.go`, `internal/app/app.go`, TUI page, WebUI page, `/api/v1/evaluator/*`, MCP tools `pando_evaluator_*`) has been in the tree since 2026-03 and is enabled in the developer config with a judge model, yet it has never produced a single result: the local DB holds 745 sessions, 0 `session_scores`, 0 `skill_library` rows, 0 `prompt_ucb_stats` rows and 4 seeded templates. Audit of 2026-09-29 (KB: `pando/analysis/self-improvement-status-2026-09.md`) found the loop is broken at every stage, not just one:

1. **Never triggered.** Evaluation runs only from `session.EndSession()` (`internal/session/session.go:394-399`). No code path calls `EndSession` — the only occurrence in the repo is the interface declaration at `session.go:162`. TUI session switches, WebUI/API, ACP session close and app shutdown never end a session. The only way a score was ever computed is the manual MCP tool `pando_evaluator_evaluate`.
2. **UCB can never select.** `seedEvaluatorTemplates` (`internal/app/app.go:1366-1412`) stores rows with `name="base/identity", section="base"`, while `SelectTemplate` queries `ListActiveTemplatesBySection(sectionName)` with the render name `"base/identity"` (`builder.go:245`, `service.go`). The section filter never matches. Even if it did, every section has exactly one variant, so there is nothing to compare, and the stored content is a snapshot rendered with `nil` data (`registry.Render(s.name, nil)`) that, when selected, replaces the live template and skips both template data and the Lua `hook_template_section` (`builder.go:250`).
3. **Attribution is lossy and volatile.** `RecordTemplateSelection` keeps one `templateID` per session in a `sync.Map` (`service.go`), so the last rendered section overwrites the rest, and the mapping dies with the process — TUI/ACP sessions rarely end in the process that built the prompt.
4. **Reward signal is weak and miscalibrated.** `S_success` depends only on regex correction patterns over user text (`reward.go`); the developer config includes `(?i)\bno[,.]?\b`, so any "no" is a correction. `S_tokens` compares against a baseline computed from `session_scores`, which is empty, so it is always the neutral 0.5. Cheap, already-persisted signals (tool errors, cancelled runs, repeated identical tool calls, explicit user feedback) are ignored.
5. **Judge output is discarded.** `InsertSessionScore` always writes `JudgeAnalysis`/`JudgeModel` as NULL; the transcript is unbounded except for 500-char tool results; there is no cost cap.
6. **Skill library cannot rank itself.** No query ever updates `skill_library.success_rate`; `DeactivateLowestSkill` orders by it, so eviction is arbitrary. Titles are `"<task_type> skill"`. `GetActiveSkills` increments `usage_count` on every prompt build, i.e. every turn (`service.go`, goroutine), and a skill saved mid-session changes the system prompt and breaks the provider prompt cache.
7. **Hidden LLM call per session.** `ContextTrimmer` (`internal/evaluator/context_trimmer.go:124`) sends a judge-model request at every new session start to filter the tool list, wired unconditionally when `evaluator.enabled` and a model exist (`app.go:530-533`). It is unrelated to evaluation and was never surfaced as its own setting.
8. **No test covers the trigger.** `service_test.go` calls `EvaluateSession` directly; nothing exercises session lifecycle → score.

Goal of this epic: turn the system into a loop that (a) fires reliably, (b) scores from honest, cheap signals, (c) attributes correctly, (d) keeps every LLM call bounded and opt-in, and (e) emits human-reviewable, versioned output (skills and template variants as files in the repo) rather than silently mutating prompts from a hidden DB. Design constraints follow the memory decisions of PANDO-EP-0008: no per-turn LLM calls, no unreviewable automatic prompt mutation, everything cheap and in-repo.

## Acceptance Criteria

- [ ] Ending or abandoning a session in TUI, WebUI/API and ACP produces a `session_scores` row within the configured window, and a startup sweep backfills unevaluated sessions (bounded, no judge by default).
- [ ] UCB selection is proven by a test: two file-based variants of one section, five evaluated sessions, the higher-reward variant is selected; with a single variant the live registry template is always used unchanged.
- [ ] Per-section template attribution is persisted in the DB and survives process restarts.
- [ ] Reward decomposition is persisted per component, default correction patterns no longer match a bare "no", and explicit user feedback dominates when present.
- [ ] Judge calls have a per-day budget, a transcript token cap, and their reasoning/model/tokens are persisted; the savings ledger records their cost.
- [ ] Learned skills are written as reviewable files with provenance and a status; only approved skills are injected, the injected block is frozen per session, and `success_rate` is updated from later rewards.
- [ ] `ContextTrimmer` is either removed or behind its own flag, default off.
- [ ] TUI and WebUI evaluator pages show real data; `pando evaluator doctor` (or equivalent) reports "enabled but N sessions never evaluated" and the reason.
- [ ] `pando/docs/self-improvement-system-analysis.md` and the manual validation guide are updated to the shipped behaviour.

## Notes

Audit document: `pando/analysis/self-improvement-status-2026-09.md`. Earlier plans: `plans/self-improvement-engine-implementation-plan.md` (2026-04-20, defined "Ending a session produces a persisted score" as Definition of Done — never met), `pando/docs/self-improvement-system-analysis.md` (2026-05-30, describes the intended flow as if working). Suggested order: US "fire the evaluation" first (it is the only blocker to getting any data), then reward + attribution, then templates and skills, observability last but its diagnostic can land early.
