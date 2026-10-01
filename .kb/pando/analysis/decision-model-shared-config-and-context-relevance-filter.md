---
created_at: 2026-10-01T16:20:29.992903349Z
updated_at: 2026-10-01T16:21:08.290972021Z
tags:
    - analysis
    - plan
    - decision-model
    - model-routing
    - context-enrichment
    - remembrances
---
# Shared decision model configuration + relevance filter for injected context — analysis (PANDO-EP-0018, 2026-10-01)

Builds on [[pando/features/model-auto-mode-implementation.md]] (EP-0015), [[pando/features/persona-auto-select-decision-model.md]] (EP-0017), [[pando/features/context-enrichment-agent-loop.md]] and [[plans/context-enrichment-enhacement.md]]. Status: epic and stories in backlog, nothing implemented.

## Gintrack

- Epic **PANDO-EP-0018**.
- Stories: US-0097 (config block + migration + REST), US-0090 (engine/health/warm-up on the shared block), US-0098 (relevance filter core in `internal/rag`), US-0099 (agent integration, notices, event, telemetry, doctor), US-0100 (WebUI), US-0102 (TUI), US-0095 (ACP + `pando_setup` + CLI), US-0096 (docs, live validation, benchmark, spec coverage, KB summary).
- Ids US-0089, 0091-0094, 0101 were consumed by failed creates (title > 200 chars) and do not exist.

## Where the decision provider lives today

`config.ModelAutoModeConfig.Router` (`DecisionRouterConfig`, `internal/config/model_auto_mode.go:78`). Consumers: `modelrouter.Engine` (`engine.go:97`, keyed on the whole auto-mode config), `ProviderFor(cfg.Router, cfg.EffectiveTimeout())`, warm-up gated on `ModelAutoMode.Enabled` (`health.go:65`), persona engine `PersonaEngineFor(cfg.ModelAutoModeConfig)` (`persona.go:209`) and cache key `persona_decision.go:277`. Surfaces: REST `/api/v1/config/model-auto-mode` + `/api/v1/model-auto-mode/router/*` (`routes.go:221-228`), WebUI `ModelAutoModeSettings.tsx` section "Decision provider" (lines 267-511) + `AgentsSettings.tsx`, TUI `settings_model_auto.go` + `settings.go:1710-1721` + `settings_persona_health.go`, ACP `session_state.go:579`, `cmd/doctor.go`, `agecrypto.go:417`, `init.go:624`.

## Decision

New top-level block `decisionModel{router{provider,baseURL,apiKey,model,keepAlive,headers}, timeoutMs}`; `modelAutoMode.router` removed from the public config with a one-shot loader migration (copy when `decisionModel.router.model` empty and legacy non-empty, persist, log once; legacy key tolerated on read). `modelAutoMode` keeps only routing policy. `modelrouter` factory/health/warm-up/engine cache keyed on `DecisionModelConfig`; consumer policy (threshold, routes, personas) passed per call. Warm-up when model set and any consumer on (auto mode ∨ persona `useDecisionModel` ∨ context filter).

## Relevance filter (third consumer)

Hook points: `rag.ContextEnricher.EnrichContext` after the three parallel searches and `minScore`, before per-source formatters so `*MaxChars`/`TotalMaxChars` apply to the filtered set (`enricher.go:120-214`); `rag.BuildMemoryBlock` after `GetMemoriesForInjection` (`memory_enricher.go:19`), pinned scopes never dropped. `internal/rag` exposes `RelevanceFilter` interface (nil = off); `internal/llm/modelrouter/relevance.go` implements it; `internal/app` wires. Agent-loop enricher output not filtered.

Protocol framing: state = prompt (+ historyPrompts); one `choice` question per candidate with criteria `useful`/`not_useful`; keep when `p(useful) ≥ threshold` (0.60). ≤ 64 questions/request, body ≤ 64 KiB, `tev1:0.8b` num_ctx 2050 → token-aware truncation, deterministic order code → KB → events → memory, unseen candidates kept. Fail-open on every error class. `score` question type is the alternative; the live benchmark (US-0096) decides. Two `/v1/systemone` calls per turn accepted (task+persona are decided before retrieval at `agent.go:1334`; filter needs retrieved candidates).

Config under `RemembrancesConfig`: `ContextEnrichmentDecisionFilterEnabled` (false), `…Threshold` (0.60), `…MaxCandidates` (32), `…MaxCandidateChars` (400), `…LocalOnly` (true, hosted providers would receive snippets), `MemoryContextDecisionFilterEnabled` (false).

Observability: notice `Context filter: kept n/m (…ms)` only when `Dropped > 0`, same path as `emitRoutingNotice` (WebUI SSE, TUI, ACP, AG-UI); fail-open warning once per session/class; `ContextFiltered` ext event + telemetry counters without text; `pando doctor` "Decision model" block.

## Ship gate

Filter default-off unless the labelled benchmark (≥ 60 prompt/candidate pairs, Ollama 0.35 `tev1:0.8b`) reaches precision ≥ 0.85 and recall ≥ 0.80 at the default threshold.

## Rejected alternative

Keep `modelAutoMode.router` as source and only add links from other sections: fixes UI copy but not engine/health/warm-up/REST coupling, and shows auto-mode-only fields next to the provider.
