---
id: PANDO-US-0072
type: story
title: Template variants as versioned files, correct section identity and persisted per-section attribution
status: done
priority: high
parent: PANDO-EP-0014
author: mcp
labels: [evaluator, prompt, ucb]
estimate: 8
created: 2026-09-29T20:33:19Z
updated: 2026-09-29T21:28:43Z
started: 2026-09-29T21:19:24Z
closed: 2026-09-29T21:28:43Z
---

## Description

UCB template selection cannot work today:

- `seedEvaluatorTemplates` (`internal/app/app.go:1366-1412`) stores `name="base/identity", section="base"` while `SelectTemplate` filters by the render name (`"base/identity"`, from `builder.go:245`), so the query never matches.
- Seeded content is `registry.Render(name, nil)`: a snapshot rendered with nil data. When selected, `builder.go:250` returns it verbatim, bypassing template data and the Lua `hook_template_section`.
- There is one variant per section, so there is nothing to compare.
- `RecordTemplateSelection` keeps one template per session in a `sync.Map`; the last section overwrites the others and the map dies with the process.

Redesign so that variants are human-authored, versioned files and the DB only holds statistics:

- Variants live in `.pando/prompts/variants/<section>/<variant>.md.tpl` (project) and optionally the global config dir; the embedded template is the implicit `default` variant. Remove DB template *content* (`prompt_templates.content` becomes a source hash + path); drop `seedEvaluatorTemplates`.
- `SelectTemplate` runs only for sections with ≥2 variants and after `minSessionsForUCB`; it returns the variant *source*, which the builder renders with the normal data and Lua hook.
- New table `session_template_selections(session_id, section, variant_id, selected_at)` written once per session (frozen for the session, like the memory block, so the prompt prefix stays cache-stable); evaluation joins it to update `prompt_ucb_stats` per variant.
- `evaluator.templates.enabled` kill switch (default true only when a variants directory exists).
- UI: WebUI/TUI templates tab lists sections, variants, times used, avg reward, UCB score, and the file path.

## Acceptance Criteria

- [ ] Test: section `base/workflow` with two file variants; five evaluated sessions with higher reward on variant B; the sixth session selects B; deleting the variants directory restores the embedded template with no DB change.
- [ ] A selected variant is rendered with the same data as the default (environment, date, tools) and passes through `hook_template_section`.
- [ ] Attribution survives a process restart: select in one process, evaluate in another, `prompt_ucb_stats` updated per section.
- [ ] Prompt bytes are identical across turns of one session (extend `TestSystemPromptIsByteStableAcrossTurns`).
- [ ] Migration removes/ignores the 4 stale seeded rows; `go test -race ./internal/evaluator ./internal/llm/prompt ./internal/app` pass; KB change document written.

## Notes

No LLM-generated variants in this epic: variants are written by the user or by an explicit command, reviewed in the repo. UCB only chooses among what a human put there.
