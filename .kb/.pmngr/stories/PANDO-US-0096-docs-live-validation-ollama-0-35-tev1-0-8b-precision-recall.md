---
id: PANDO-US-0096
type: story
title: Docs, live validation (Ollama 0.35 `tev1:0.8b`), precision/recall benchmark of the relevance filter on a labelled set, spec coverage and KB summary
status: in_review
priority: medium
parent: PANDO-EP-0018
author: mcp
labels: [decision-model, docs, benchmark, spec-coverage, kb]
estimate: 5
created: 2026-10-01T16:18:26Z
updated: 2026-10-01T17:12:57Z
started: 2026-10-01T17:12:57Z
---

## Description

As the team, we want proof that the filter helps before it ships, and documentation that reflects the new configuration layout.

- **Benchmark** `tests/decision_model/relevance_bench.py` (Python, `tests/` folder): a labelled set of ≥ 60 (prompt, candidate, useful?) triples built from real Pando prompts and real `code_hybrid_search`/`kb_search_documents` hits (mix of true positives and semantically-close distractors across code, KB, events, memories). Runs against Ollama 0.35 `tev1:0.8b` (and optionally `nimble`, TypeSafe Jev) through `POST /api/v1/remembrances/...` playground or a Go live test (`relevance_live_test.go`, skipped without `PANDO_SYSTEMONE_LIVE=1`). Reports precision, recall, F1 at thresholds 0.5/0.6/0.7, p50/p95 latency, tokens, and compares `choice {useful,not_useful}` vs `score` framing.
- **Ship gate:** default-off stays unless precision ≥ 0.85 and recall ≥ 0.80 at the default threshold; otherwise record the numbers and keep the feature opt-in with the measured threshold recommended in docs.
- **Docs:** new `docs/decision-model.md` (providers, install `ollama pull tev1:0.8b`, privacy, consumers table, migration note); `docs/model-auto-mode.md` trimmed to routing policy; `docs/knowledge-base.md` and `docs/configuration.md` gain the filter options; `docs/webui.md`, `docs/acp.md` cross-links; README feature list line.
- **Specs:** create the gintrack specs named in the stories and trace every requirement to a test; `spec_coverage` for PANDO-EP-0018 shows 100 % traced, verified requirements stamped.
- **KB:** `kb_add_document` under `pando/features/decision-model-shared-config-and-relevance-filter.md` linking to `[[pando/features/model-auto-mode-implementation.md]]`, `[[pando/features/persona-auto-select-decision-model.md]]`, `[[pando/features/context-enrichment-agent-loop.md]]`; update the auto-memory index.

## Acceptance Criteria

- [ ] Benchmark script committed with its dataset and a Markdown report of results (numbers, hardware, model versions) in `docs/decision-model.md` or `tests/decision_model/REPORT.md`.
- [ ] Live Go test passes against Ollama 0.35 locally (skipped in CI without the env flag).
- [ ] Decision on default (on/off) and threshold recorded in the epic comments with the measured figures.
- [ ] Docs updated; `docs/model-auto-mode.md` no longer documents `modelAutoMode.router`; migration note present.
- [ ] All stories' specs exist, `spec_coverage` 100 % traced, all requirements verified.
- [ ] KB summary document stored and findable with an untagged `kb_search_documents` query for "decision model configuration".

## Notes

- Reuse the methodology of PANDO-US-0085 (model auto mode validation) and the persona benchmark from PANDO-EP-0017.
- Anchors: `tests/model_auto_mode/test_playground_live.py`, `internal/llm/modelrouter/*_live_test.go`, `docs/model-auto-mode.md`.
