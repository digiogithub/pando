# Plan: Self-Improvement Engine Completion

## Goal
Complete the self-improvement engine so prompt template selection, session evaluation, learned skill extraction, and stats all work end-to-end from real session data.

## Current State
- UI wiring exists in TUI and web-ui.
- Session end already triggers `EvaluateSession()` via `internal/session/session.go`.
- Prompt builder already calls evaluator hooks for template selection and learned skill injection.
- SQL schema and generated queries already exist for prompt templates, session scores, UCB stats, and skill library.
- Main gaps are concentrated in `internal/evaluator/service.go`, with limited/no real tests for the engine.

## Phase 1 — Service design and data extraction
1. Refactor `internal/evaluator/service.go` to use focused helpers:
   - load session messages
   - build transcript
   - derive selected template
   - compute reward
   - optionally judge
   - persist score
   - persist/deactivate skills
2. Add internal conversion helpers from DB/message models to evaluator-specific input structs.
3. Handle nil DB / disabled evaluator safely and keep async behavior unchanged.

## Phase 2 — Complete runtime evaluation pipeline
1. Implement `runEvaluation(ctx, sessionID)`:
   - load messages with `ListMessagesBySession`
   - derive transcript from text/reasoning/tool results
   - detect corrections using configured regexes
   - compute token baseline with `GetTokenBaseline`
   - compute reward with `calculateReward`
   - read selected template from `sessionTemplates`
   - insert `session_scores` with `InsertSessionScore`
2. Make evaluation idempotent enough for repeated session-end calls:
   - if a score already exists for the session, skip or return cleanly
   - always clear in-memory template selection after processing
3. Add structured logging for success/failure paths.

## Phase 3 — Judge model integration
1. Reuse the existing provider stack instead of inventing a separate client.
2. Build a small helper to create a judge provider from `EvaluatorConfig.Model` and provider settings.
3. Render the judge prompt via `renderJudgePrompt`.
4. Send the transcript to the judge model and parse output via `parseJudgeOutput`.
5. Store judge reasoning/model into `session_scores`.
6. Keep failure mode soft:
   - if judge call fails, still persist reward score
   - do not fail session end

## Phase 4 — Learned skill persistence
1. Convert successful judge output into `skill_library` entries when:
   - `new_skill` is non-empty
   - confidence is above threshold (existing comment suggests >= 0.7)
2. Persist with `InsertSkill`.
3. Enforce `MaxSkills` using `CountActiveSkills` + `DeactivateLowestSkill`.
4. Normalize task type fallback to `general`.
5. Optionally increment usage for retrieved skills when injected into prompts.

## Phase 5 — Template selection and skill retrieval
1. Implement `SelectTemplate(ctx, sectionName)`:
   - require evaluator enabled and DB available
   - check `CountSessionScores`
   - before `MinSessionsForUCB`, return nil to keep default templates
   - fetch candidates with `ListActiveTemplatesBySection`
   - compute/select highest UCB score using `UCBScore`
   - prefer unexplored templates when applicable
2. Implement `GetActiveSkills(ctx, taskType)`:
   - use `ListActiveSkillsByType`
   - map DB rows to evaluator skill types
3. Implement `GetStats(ctx)`:
   - aggregate via `GetEvaluatorStats`, `ListUCBRanking`, `ListAllActiveSkills`
   - map to current TUI/web structs

## Phase 6 — API and UX verification
1. Verify `internal/api/handlers_evaluator.go` returns meaningful data once service persists records.
2. Seed or verify at least one active prompt template exists, otherwise UCB selection will never activate.
3. Confirm prompt builder records template selection using session context.
4. Confirm both UIs show real metrics/templates/skills after completed sessions.

## Phase 7 — Tests
1. Add unit tests for:
   - `calculateReward`
   - `UCBScore`
   - `renderJudgePrompt`
   - `parseJudgeOutput`
2. Add evaluator service tests with temp SQLite DB covering:
   - evaluation creates `session_scores`
   - UCB selection threshold behavior
   - skill insertion and max-skill eviction
   - stats aggregation
3. Keep the existing TUI height regression test.

## Risks / Decisions
- Judge model invocation should use the existing provider abstraction for consistency.
- Transcript construction must avoid leaking huge tool payloads unnecessarily; truncate or summarize large tool results if needed.
- Repeated evaluation of the same session should not create duplicate rows.
- Prompt template data may need bootstrapping if the DB is empty.

## Definition of Done
- Ending a session produces a persisted score in `session_scores`.
- Prompt template selection switches from default to UCB-driven after threshold.
- Learned skills are stored and re-injected into future prompts.
- TUI/web self-improvement pages show real metrics, templates, and skills.
- Core evaluator logic is covered by automated tests.