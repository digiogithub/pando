---
id: PANDO-US-0085
type: story
title: Docs, end-to-end validation (Ollama 0.35 tev1:0.8b/nimble + optional TypeSafe/OpenRouter Jev), spec coverage and KB summary
status: in_review
priority: medium
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, docs, testing]
estimate: 2
created: 2026-09-30T19:34:53Z
updated: 2026-09-30T21:00:23Z
started: 2026-09-30T21:00:23Z
---

## Description

As a maintainer, I want auto mode documented, every spec requirement traced to a passing test, and the defaults validated on real backends. Users can then set it up, and we know the defaults are sensible.

- **User doc** (in the `docs/` split from the README). It covers:
  - **Decision providers:**
    - Ollama: needs ≥ 0.35, then `ollama pull tev1:0.8b` or `nimble`.
    - TypeSafe Jev: needs an API key. Pricing is $0.042 per 1M input tokens.
    - Custom Jev-compatible gateways: OpenRouter, LiteLLM, Vercel AI Gateway, Kev.
  - **Base URL convention:** use the root URL only; Pando appends `/v1/systemone`.
  - **Privacy:** hosted providers receive the prompt text.
  - **Writing good task descriptions:** mutually exclusive, concrete verbs, examples.
  - **Threshold:** what it means, and why `confidence` is not correctness.
  - **Fallbacks:** how they work, and the prompt-cache cost of switching models.
  - **The Auto entry** in every client.
- **Test fixtures:**
  - Recorded responses from Ollama 0.35.0 go in `internal/llm/systemone/testdata/`: `/api/tags` with capabilities, `/api/show` for `tev1:0.8b`, `/api/version`, and `/v1/systemone` choice/noul answers.
  - Also record Jev-style `/v1/models` payloads in both shapes, `{models:[…]}` and `{data:[{id}]}`.
  - The fake decision server helper (`internal/llm/systemone/systemonetest`) is shared by all story tests and by the WebUI Playwright run.
- **Live validation**, opt-in through env variables:
  - `tests/model_auto_mode/bench_router.py`: about 30 labelled developer prompts, English and Spanish, across 4 starter routes plus `none`. It reports accuracy, no-match rate and p50/p95 latency for `tev1:0.8b` and `nimble`.
  - `tests/model_auto_mode/live_providers.py`: runs the same set against TypeSafe (`TYPESAFE_API_KEY`) or a custom gateway (`PANDO_LIVE_JEV_BASEURL` / `PANDO_LIVE_JEV_KEY`) when they are set, and reports cost.
  - The default router model, threshold and timeout per provider kind are chosen from this data.
- **Failover drill:** point a route's primary model at a provider with an invalid key and confirm the turn continues on fallback 1. Do this on at least two provider types.
- **Spec coverage:** every requirement in this epic's specs has `trace.tests`, and `spec_coverage` reports `passing` for all non-live requirements.
- **KB:** after implementation, add a summary with `kb_add_document` under `pando/features/model-auto-mode.md`. Link it with wiki links to `pando/analysis/model-auto-mode-systemone-router.md`, `claude_code_improvements.md` and the persona selector.

## Acceptance Criteria

- [ ] The docs are published, and the settings UI links to them.
- [ ] The validation results table is committed to the KB, and the chosen defaults are justified by it.
- [ ] The failover drill passes on at least two provider types.
- [ ] The gintrack spec coverage for the epic's specs is `passing`, except for requirements that are live-only by design.
- [ ] `go test ./...` passes for the touched packages, and the WebUI build passes.

## Notes

- The developer machine runs Ollama 0.35.0 with `tev1:0.8b`, installed by the user at system level.
- First benchmark: 14/16 correct, p50 65 ms (see the epic comment).
