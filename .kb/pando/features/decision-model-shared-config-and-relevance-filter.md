---
created_at: 2026-10-01T17:12:51.54648392Z
updated_at: 2026-10-01T17:12:51.54648392Z
tags:
    - feature
    - decision-model
    - relevance-filter
    - benchmark
    - EP-0018
---
# Decision model: shared configuration and context relevance filter (EP-0018)

Date: 2026-10-01. Epic PANDO-EP-0018; docs/benchmark/specs story PANDO-US-0096. Status: implemented, uncommitted at the time of writing; filter default-off, ship gate NOT met.

Related: [[pando/analysis/decision-model-shared-config-and-context-relevance-filter.md]], [[pando/features/model-auto-mode-implementation.md]], [[pando/features/persona-auto-select-decision-model.md]], [[pando/features/context-enrichment-agent-loop.md]].

## What changed

- The System One / Jev decision provider moved out of model auto mode into a top-level `decisionModel` block (`internal/config/decision_model.go`): `router{provider, baseURL, apiKey, model, keepAlive, headers}` and `timeoutMs` (0 = 1500 ms ollama / 3000 ms remote). A loader migration (`migrateLegacyDecisionModel`) copies a legacy `modelAutoMode.router` once, persists it, is idempotent, lets `decisionModel` win and stays in memory when the file is locked.
- One engine factory: `modelrouter.ForConfig(DecisionModelConfig)`, `Engine.Ask` (up to 64 questions), health cache and warm-up driven by `config.AnyDecisionConsumerEnabled` (auto mode, persona-selector `useDecisionModel`, either filter flag). Consumers: model auto mode, persona auto-select, relevance filter.
- Relevance filter: `internal/llm/modelrouter/relevance.go` (`NewRelevanceFilter`), hooks in `internal/rag/relevance.go`, `enricher.go`, `memory_enricher.go`, wiring `internal/app/relevance_filter.go`, agent integration `internal/llm/agent/context_filter.go`. Options in `[Remembrances]` (`ContextEnrichmentDecisionFilterEnabled`, `MemoryContextDecisionFilterEnabled`, threshold 0.60, max candidates 32, max candidate chars 400, `...AllowHosted` false = local-only; stored inverted so a missing key stays safe).
- REST: `/api/v1/config/decision-model` (+ DELETE api-key), `/api/v1/decision-model/router/*`; the old `/api/v1/model-auto-mode/router/*` are deprecated aliases; `GET /config/model-auto-mode` returns `router` as a read-only copy.
- Surfaces: WebUI Decision model page + in-use rows + Remembrances filter controls + chat notice, TUI section, `pando_setup decision-model`, ACP `/decision-model` and Auto option description, `pando doctor` "Decision model" block.
- US-0096 deliverables: `docs/decision-model.md` (new), trimmed `docs/model-auto-mode.md`, `docs/configuration.md`, `docs/knowledge-base.md`, `docs/webui.md`, cross-links in `docs/acp.md` and `docs/pando-setup.md`, README feature and doc index; tests `internal/rag/relevance_store_test.go`, `TestRelevanceFailOpenOnOllamaOlderThan035` in `internal/llm/modelrouter/relevance_test.go`, live test `internal/llm/modelrouter/relevance_live_test.go`; benchmark `tests/decision_model/{relevance_bench.py,dataset.jsonl,REPORT.md}`; gintrack specs PANDO-SP-0005..0009 (37 requirements).

## Semantics worth remembering

Fail-open on every error class (including an Ollama < 0.35 that 404s `/v1/systemone`). Only the first `MaxCandidates` non-pinned candidates (order code, kb, events, memory) are asked; candidates beyond the cap or that do not fit the token budget / 64 KiB body are KEPT unseen. Pinned-scope memories are never asked or dropped. Hit counters move only for injected memories. Eligibility: coder agent, user-initiated, root session, not clean-mode or system runs; the agent-loop enricher output is never filtered. Notice `Context filter: kept 4/9 (38 ms) — code 1/3, kb 2/4, ...` only when something was dropped; fail-open warnings once per session and class. Events and telemetry carry no prompt or snippet text.

## Verification

- `go build ./...`, `go vet ./internal/... ./cmd/...`, `go test ./internal/... ./cmd/... ./pkg/...` green (also with `PANDO_LIVE_OLLAMA=1`).
- New store-backed tests run `EnrichContextWithResult` (KB + events) and `BuildMemoryBlockWithResult` against real migrated SQLite databases with a constant fake embedder and a recording filter; they cover pinned memories and hit counters. The code store (CodeIndexer) is not exercised there (needs tree-sitter indexing); code candidates are covered by golden and unit tests.
- Live: `PANDO_LIVE_OLLAMA=1 go test ./internal/llm/modelrouter -run RelevanceLive` against Ollama 0.35.0 + `tev1:0.8b`: applied in 143 ms, kept a useful code candidate (p=0.74), dropped an unrelated recipe (p=0.47).
- Benchmark (`tests/decision_model/REPORT.md`, 72 labelled candidates, 38 useful, tev1:0.8b, RTX 4000 SFF Ada GPU): production `choice` framing, threshold 0.50 precision 0.744 recall 0.763 F1 0.753; 0.60 precision 0.833 recall 0.658 F1 0.735; 0.70 precision 0.833 recall 0.395 F1 0.536. Best F1 0.769 at 0.45. ROC AUC 0.786. Latency p50 163 ms / p95 239 ms per request of 4 candidates (about 41/60 ms per candidate). `score` (array criteria, not emittable by the Go client today) and `noul` framings do not beat `choice`; one-question-per-request (`choice-single`) discriminates poorly (AUC 0.54).
- Ship gate (precision >= 0.85 and recall >= 0.80 at 0.60): NOT met. Filter stays default-off; no defaults or prompt framing changed. Recommendation: if enabling, use threshold about 0.50; consider code-only filtering or per-source thresholds (code precision 0.947, KB/memory weak).
- Specs: SP-0005..SP-0009, 37 requirements traced to tests; 36 verified; PANDO-SP-0009.R2 not stamped (stale vitest results from renamed tests at an older commit in the local gintrack result cache make the check report mixed-commits; the tests pass).

## Deviations from the plan

- `LocalOnly` stored inverted as `...AllowHosted` (viper defaults do not reach the struct and a default-true bool flips on save).
- Candidates beyond the cap or that cannot fit are kept, not dropped.
- `GET /config/model-auto-mode` still returns `router` as a read-only alias instead of omitting it.
- Benchmark dataset is LLM-authored with illustrative line numbers; it exercises Ollama directly, not the Go code.

## Known limitations

- Quality of `tev1:0.8b` is moderate on close distractors; the filter can drop a third of useful candidates at 0.60.
- Keyed memory upserts via `UpsertMemory` do not create chunks/embeddings in the store path itself (the real flow embeds elsewhere); tests use keyless memories and set `memory_scope` directly.
- WebUI was unit-tested (Vitest) but not exercised by hand in this story.
- Model auto mode specs PANDO-SP-0001/0004 show as suspect in gintrack because their traced code changed with this epic; their tests still pass.
