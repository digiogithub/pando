---
created_at: 2026-09-29T21:28:08.329085349Z
updated_at: 2026-09-29T21:28:08.329085349Z
tags:
    - change
    - evaluator
    - prompt
---
# Evaluator template variants as versioned files (PANDO-US-0072)

Builds on [[pando/changes/evaluator-honest-reward.md]]; context in [[pando/analysis/self-improvement-status-2026-09.md]] and [[project_prompt_templates_plan]].

## Problem
UCB template selection never worked: seeded rows were keyed by a section name the builder never used, content was a nil-data snapshot returned verbatim (bypassing template data and `hook_template_section`), one variant per section, and the per-session choice lived in a process-local `sync.Map` where the last section overwrote the others.

## Design
- Variants are human-authored files: `.pando/prompts/variants/<section>/<variant>.md.tpl` (project) and `~/.config/pando/prompts/variants/...` (global; project wins on name clash). `<section>` is exactly the builder render name (e.g. `base/workflow`). The embedded/overridden template is the implicit `default` variant. Variant id = `<section>#<variant>`. A file named `default.md.tpl` is ignored.
- DB holds statistics only:
  - `session_template_selections(session_id, section, variant_id, selected_at, PK(session_id, section))`: chosen once per session+section and frozen (INSERT OR IGNORE, read back, so concurrent processes agree). Same idea as `sessionMemoryBlock`: prompt bytes stay cache-stable and survive restarts.
  - `prompt_variant_stats(variant_id PK, section, times_used, total_reward, avg_reward, updated_at)`. Stats are keyed by variant id string; the old `prompt_ucb_stats`/`prompt_templates` tables stay (emptied) and are unused.
  - Migration `20260929000003_add_template_variants.sql` deletes all `prompt_templates` and `prompt_ucb_stats` rows, nulls `session_scores.template_id` / `skill_library.source_template_id`, and DROPS the triggers `update_ucb_after_score` and `update_ucb_after_rescore` (they wrote per-template rows and would be wrong). Stats are now maintained from Go.
- Stats updates (one SQL statement each, joined to the session's selections): `ApplySessionRewardToVariantStats` on first evaluation (times_used+1, add reward); `ApplyRewardDeltaToVariantStats` on re-score (delta only, times_used untouched). Idempotent because evaluation inserts once and re-scores use deltas. Both plus `InsertSessionTemplateSelection` are write queries registered in `internal/ipc/dbproxy`.
- Selection (`EvaluatorService.SelectVariant`): only for sections with >=2 candidates (default + files), with a session id, evaluator enabled and `evaluator.templates.enabled`. Until the section has `minSessionsForUCB` evaluated sessions it picks the least-selected variant (ties: default first); afterwards UCB1 (`ucb.go`) over `prompt_variant_stats`. A frozen variant whose file disappeared falls back to default.
- Rendering: `prompt.PromptBuilder.renderSection` discovers variant files, asks the evaluator (via `PromptEvaluator.SelectVariant`, adapter in `internal/app/app.go`), renders the variant source with `TemplateRegistry.RenderSource` using the normal `PromptData`, then applies `hook_template_section` like the default. Parse/render/read failure logs a warning and falls back to default. `seedEvaluatorTemplates`, `SelectTemplate`, `RecordTemplateSelection` were removed.
- UI/API: `GET /api/v1/evaluator/templates` returns `{sections:[{section, variants:[{id,name,path,is_default,missing,times_used,avg_reward,ucb_score}]}]}` (files on disk merged with stats). TUI evaluator table shows section/variant/used/avg/UCB from `Stats.Templates` (`TemplateStats` now variant-based, also feeds `pando_evaluator_stats`). WebUI `UCBRankingTable` updated.

## Config
`evaluator.templates.enabled` (default true, viper default + `EvaluatorWithDefaults` for a fully unset struct). Inert when no variants exist. Unit configs built by hand need `Templates: config.TemplatesConfig{Enabled: true}`.

## Authoring a variant
1. Copy the embedded template from `internal/llm/prompt/templates/<section>.md.tpl` (or start fresh).
2. Save it as `.pando/prompts/variants/base/workflow/terse.md.tpl`. Same Go template syntax and data (`.WorkingDir`, `.Date`, ...).
3. Commit it. New sessions are split between `default` and `terse` until the threshold, then UCB1 favours the better reward. Delete the file to retire it; no DB change needed.

## Verification
`go build ./...`, `go vet`, `go test -race` on evaluator, prompt, agent, app, session, api, config, db, ipc, tui, cmd (all pass); `npx tsc -p tsconfig.app.json --noEmit` clean. New tests: `internal/evaluator/variants_test.go` (UCB picks B on 6th session, frozen across processes, attribution per section after restart, inert cases), `internal/llm/prompt/variants_test.go` (data + Lua hook, removed dir, broken variant, project-over-global), `internal/llm/agent/memory_block_test.go` (byte stability with variants and across evaluator restart), `internal/db/template_variants_migration_test.go`, `internal/api/handlers_evaluator_templates_test.go`.

## Known issues / notes
- Pre-existing: the Lua `hook_template_section` input is exposed under `ctx.parameters.*` and only a top-level `section_content` in the returned table is read back; `docs/lua-hooks-example.lua` shows `ctx.section_name` directly, which does not work.
- A session that gets a new section variant after it was already evaluated has that variant's reward delta applied on re-score without a times_used increment (rare).
- Old generated queries (`InsertPromptTemplate`, `ListActiveTemplatesBySection`, `ListUCBRanking`, ...) remain in `internal/db` but are unused.
