---
id: PANDO-EP-0018
type: epic
title: Shared decision model configuration (`decisionModel` block) used by model auto mode, persona auto-select and a new relevance filter for injected context (code, KB, events, memories)
status: backlog
priority: high
author: mcp
labels:
  - decision-model
  - model-routing
  - ollama
  - config
  - remembrances
  - context-enrichment
  - webui
  - tui
  - acp
created: 2026-10-01T16:15:51Z
updated: 2026-10-01T16:15:51Z
---

## Description

**Today.** The System One / Jev decision provider (Ollama ≥ 0.35 `tev1`, TypeSafe Jev, any Jev-compatible gateway) is configured **inside model auto mode**: `config.ModelAutoModeConfig.Router` (`internal/config/model_auto_mode.go:78`, type `DecisionRouterConfig{Provider, BaseURL, APIKey, Model, KeepAlive, Headers}`). Two features already consume it:

- **Model auto mode** (PANDO-EP-0015): `modelrouter.Engine` is built from `config.ModelAutoModeConfig` (`engine.go:97`, `ForConfig`, `configKey`), `ProviderFor(cfg.Router, cfg.EffectiveTimeout())` (`health.go:35`), warm-up gated on `ModelAutoMode.Enabled && Router.Model != ""` (`health.go:65`).
- **Persona auto-select** (PANDO-EP-0017): `agents["persona-selector"].useDecisionModel` reads `modelAutoMode.router` whether or not auto mode is enabled (`PersonaEngineFor(cfg.ModelAutoModeConfig)`, `persona.go:209`; `persona_decision.go:277`).

The router also leaks into every surface as a model-auto-mode concern: REST `/api/v1/config/model-auto-mode` and `/api/v1/model-auto-mode/router/{models,test,health,pull,pull/{id}}` (`internal/api/routes.go:221-228`), WebUI `ModelAutoModeSettings.tsx` ("Decision provider" section, lines 267-511) and `AgentsSettings.tsx`, TUI `buildModelAutoModeSection` (`settings_model_auto.go:105`) and the persona-selector read-only router line (`settings.go:1710-1721`, `settings_persona_health.go`), ACP session option description `autoModelDescription()` (`internal/mesnada/acp/session_state.go:579`), `pando doctor` (`cmd/doctor.go`), `agecrypto.go:417` (key encryption), init template `[ModelAutoMode.Router]` (`init.go:624`).

**Problems.**

- A user who wants only persona auto-select, or only context filtering, has to open "Model auto mode" to configure a provider that has nothing to do with routing models. The dependency is visible in the UI copy ("Use decision model (from model auto mode)").
- Every new consumer repeats the same wiring: provider resolution, timeout defaults, health cache, warm-up, key masking, REST exposure, UI health line.
- Engine, health and warm-up are keyed on `ModelAutoModeConfig`, so a consumer that is on while auto mode is off has to pass a struct full of unrelated routes.

**Third consumer, new in this epic: relevance filter for injected context.** Context enrichment (`Remembrances.ContextEnrichment*`, `internal/rag/enricher.go`) runs KB, events and code searches in parallel and appends everything above `ContextEnrichmentMinScore` to the user message (`agent.go:1358-1385`). The memory injector prepends a `<memories>` block to the system prompt (`internal/rag/memory_enricher.go:BuildMemoryBlock`, `agent.go:122`). Both filter by embedding score only: a vector score says the text is *similar*, not that it is *useful for this task*. The agent-loop enricher (`internal/app/context_enricher_agent.go`) curates with a generative model, but it is expensive and off by default. The decision model is the right tool for the middle ground: after retrieval, ask one cheap, calibrated question per candidate ("is this snippet useful to carry out the user's request?") and drop what is not.

**What this epic does.**

1. **Extract the configuration.** A new top-level block `decisionModel` (`config.DecisionModelConfig`) owns the provider: `router` (the existing `DecisionRouterConfig`), `timeoutMs`, `keepAlive`. `modelAutoMode.router` is removed from the public config; a loader migration copies a non-empty `modelAutoMode.router` into `decisionModel.router` once and rewrites the file. `modelAutoMode` keeps only routing policy (`enabled`, `defaultAuto`, `selected`, `threshold`, `minConfidence`, `historyPrompts`, `routes`).
2. **One engine factory.** `modelrouter` (or a new `internal/llm/decision` package) builds `systemone.DecisionProvider`, health cache and warm-up from `DecisionModelConfig` alone. Model auto mode, persona auto-select and the context filter are three consumers of the same provider, with their own thresholds.
3. **Relevance filter.** Opt-in under Remembrances: after the enricher collects candidates (code symbols, KB chunks, events) and after the memory store returns memories, a System One request with one question per candidate (≤ 64 per request, body ≤ 64 KiB, token-aware truncation) decides keep/drop. Candidates below the threshold are removed before formatting. Fail-open: any router problem keeps the unfiltered result.
4. **Settings at parity in WebUI and TUI.** A "Decision model" settings page/section holds provider, base URL, key, model (discovery, pull, health, test, privacy note). Model auto mode, the `persona-selector` agent and Remembrances show a read-only "router in use" line linking to it.
5. **ACP / CLI parity.** ACP session option descriptions, notices, `pando_setup`, `pando doctor` and the JSON schema read the new block.

### Protocol facts that constrain the filter

- `systemone.Question` supports `choice`, `noul` and `score` (`client.go:71`). A `choice` question has 2–26 criteria; a request carries up to 64 questions (`MaxQuestions`). Ollama caps the body at 64 KiB and does not truncate; `tev1:0.8b` loads with `num_ctx 2050`, so state plus candidates must be truncated with `modelrouter.BuildState`/`EstimateTokens` discipline.
- Per-candidate framing: state = user prompt (+ `historyPrompts`), questions `c1..cN` of type `choice` with criteria `{useful, not_useful}` (or `score` once the live test confirms calibration on Ollama). Decision uses `probabilities[useful] ≥ threshold`.
- Hosted providers receive the prompt **and the candidate snippets**. The privacy note must say so; the filter can be restricted to local providers by default.

## Acceptance Criteria

- [ ] **Config block.** `decisionModel{router{provider,baseURL,apiKey,model,keepAlive,headers}, timeoutMs}` exists with defaults (ollama, 1500 ms local / 3000 ms remote), validation (`ValidateDecisionModel`), normalisation, lock awareness, encrypted key at rest, `$ENV` references, JSON schema, init template, viper defaults.
- [ ] **Migration.** On load, when `decisionModel.router.model` is empty and the legacy `modelAutoMode.router.model` is not, the legacy router is copied into `decisionModel`, persisted, and a single info log is written. The legacy key is tolerated (no startup error) but no longer written. A test covers migration, idempotency and a locked file.
- [ ] **Single factory.** `modelrouter.ForConfig`, `PersonaEngineFor`, `ProviderFor`, `RouterHealth`, warm-up and `configKey` take `config.DecisionModelConfig` (plus the consumer's policy). Warm-up runs when `decisionModel.router.model` is set and at least one consumer is on (auto mode enabled, persona-selector `useDecisionModel`, or context filter enabled).
- [ ] **Consumers unchanged in behaviour.** Model auto mode and persona auto-select pass their existing tests against the new block; `/api/v1/config/model-auto-mode` no longer returns `router`, and the combined request (task + persona) still issues one `/v1/systemone` call.
- [ ] **REST.** `GET/PUT /api/v1/config/decision-model` (key masked, empty key keeps stored, `DELETE .../api-key`), and `/api/v1/decision-model/router/{models,test,health,pull,pull/{id}}`. The old `/api/v1/model-auto-mode/router/*` paths keep working as aliases for one release and log a deprecation warning once.
- [ ] **Relevance filter.** `remembrances.contextEnrichmentDecisionFilterEnabled` (default off), `contextEnrichmentDecisionFilterThreshold` (default 0.60), `contextEnrichmentDecisionFilterMaxCandidates` (default 32), `memoryContextDecisionFilterEnabled` (default off). When on: code, KB and event candidates above `minScore` and memories selected for injection are classified in one request (two when memories are on, since the memory block is built against the system prompt); candidates below threshold are dropped before formatting and before budgets are applied. Nothing is dropped on router error, timeout, malformed answer or when the router model is empty (fail-open, one warning per session and error class). The agent-loop enricher output is never filtered (already curated).
- [ ] **Latency bound.** The filter adds at most `decisionModel.timeoutMs` to a turn and runs after the parallel searches; candidate text per question is capped (default 400 chars) and the total request is truncated to the model's context budget with a deterministic order (code, KB, events, memories).
- [ ] **Observability.** Debug log per turn with kept/dropped per source, probabilities and latency; a status notice `Context filter: kept 4/9 (…ms)` shown in WebUI, desktop, TUI, ACP and AG-UI **only when something was dropped**; `ContextFiltered` external event and telemetry counters without prompt or snippet text; `pando doctor` reports the decision model block, its health, and which consumers use it.
- [ ] **WebUI.** New settings category "Decision model" (group AI) holding what the "Decision provider" section of `ModelAutoModeSettings.tsx` has today (provider, URL, key, headers, keep-alive, model discovery/pull/health/test, privacy note). `ModelAutoModeSettings`, `AgentsSettings` (persona-selector) and `RemembrancesSettings` show a read-only "Decision model: provider/model · health" row with a link to that page, and a warning when their option is on and no router model is configured. Remembrances gains the filter toggles and threshold. i18n in all locales; tests updated.
- [ ] **TUI.** New `buildDecisionModelSection` (group AI) with the same fields and actions (discover, pull, test, clear key); `buildModelAutoModeSection`, the persona-selector agent fields and `buildRemembrancesSection` show the read-only router/health line and the new toggles. Hot reload on `decisionModel` config events.
- [ ] **ACP.** `autoModelDescription()` and persona option descriptions read `decisionModel.router`; the context-filter notice reaches ACP clients through the existing system-message path; `pando_setup` gains a `decision-model` subcommand (show/set provider, URL, key, model, test) so ACP/CLI users can configure it without the UI; ACP `available_commands` and docs list it.
- [ ] **Hot reload.** Changing `decisionModel` takes effect on the next prompt in all three consumers without restart; the engine cache is keyed on the new block.
- [ ] **Specs and tests.** Gintrack specs with every requirement traced to tests. Unit tests with `systemonetest`, including fail-open paths and request-count assertions. Live check on Ollama 0.35 `tev1:0.8b`: a labelled set of (prompt, candidate, useful?) pairs reports precision/recall of the filter and p50/p95 latency; the filter is shipped default-off unless precision ≥ 0.85 on that set.
- [ ] **Docs.** `docs/model-auto-mode.md` split: a new `docs/decision-model.md` for the shared provider; `docs/configuration.md`, `docs/knowledge-base.md`, `docs/acp.md`, `docs/webui.md` and the KB summary updated.

## Notes

- **Codebase anchors (exploration 2026-10-01).**
  - Config: `internal/config/model_auto_mode.go` (`DecisionRouterConfig`, `EffectiveProvider/BaseURL/APIKey`, `EffectiveTimeout`, `UpdateModelAutoMode`, `ClearModelAutoModeAPIKey`, `publishModelAutoModeChange`), `config.go:2588-2594` (viper defaults), `:133-137` and `:5680-5710` (`UseDecisionModel`), `:514-581` (`RemembrancesConfig` enrichment and memory fields), `agecrypto.go:417`, `init.go:624`.
  - Engine: `internal/llm/systemone` (client, providers, health, `systemonetest`), `internal/llm/modelrouter` (`engine.go`, `persona.go`, `health.go`, `state.go`, `candidates.go`).
  - Agent: `internal/llm/agent/agent.go:1334-1400` (persona → enrichment → `beginAutoTurn`), `model_auto.go`, `persona_decision.go`; enricher hooks `SetContextEnricher`/`SetMemoryInjector` (`agent.go:97-132`).
  - Retrieval: `internal/rag/enricher.go` (`EnrichContext`, `searchKB/Events/Code`, `EnricherConfig`), `internal/rag/memory_enricher.go` (`BuildMemoryBlock`, `formatMemoryLine`), `internal/app/app.go:371-460` (wiring), `internal/app/context_enricher_agent.go`, `internal/app/memory_injector_adapter.go`.
  - API: `internal/api/handlers_model_auto_mode.go`, `routes.go:154-168` (remembrances), `:221-228` (model auto mode).
  - WebUI: `web-ui/src/components/settings/{SettingsView,ModelAutoModeSettings,AgentsSettings,RemembrancesSettings}.tsx`, `components/chat/RoutingNotice.tsx`, `i18n/locales/*.json`.
  - TUI: `internal/tui/page/settings_model_auto.go`, `settings.go:989-1003` (sections), `:1606-1721` (agents), `:2389` (remembrances), `settings_persona_health.go`.
  - ACP: `internal/mesnada/acp/session_state.go:554-600`; CLI: `cmd/doctor.go`, `cmd/schema/main.go`, `pando_setup` tool.
- **Design decision: where the filter lives.** Inside `rag.ContextEnricher` after the three searches and before the per-source formatters, so budgets (`*MaxChars`, `TotalMaxChars`) apply to the filtered set; and inside `rag.BuildMemoryBlock` after `GetMemoriesForInjection`. The enricher receives a `RelevanceFilter` interface (nil = off) so `internal/rag` does not import `modelrouter`; `internal/app` wires the implementation.
- **Why not one combined request with task + persona.** Those decisions are taken before retrieval (`agent.go:1334`); the filter needs the retrieved candidates. Two requests per turn is accepted; a later optimisation could move the persona/task questions after retrieval.
- **Alternative rejected:** keep `modelAutoMode.router` as the source and only add links from the other sections. It fixes the UI but not the coupling in engine/health/warm-up/REST, and it still shows auto-mode-only fields (threshold, routes) next to the provider.
- **Open question (confirm with user):** `choice {useful, not_useful}` per candidate versus `score`. Start with `choice` because its probabilities are what model auto mode already thresholds; the live check decides.
- **Suggested stories, in implementation order:** (1) config block + migration + REST + schema; (2) engine/health/warm-up on the shared block, consumers migrated; (3) relevance filter core in `internal/rag`; (4) agent integration, fail-open, notices, events, telemetry, doctor; (5) WebUI; (6) TUI; (7) ACP + `pando_setup` + CLI; (8) docs, live validation, benchmark, spec coverage, KB summary.
- **References:** PANDO-EP-0015 (SP-0001..0004), PANDO-EP-0017; KB `pando/analysis/model-auto-mode-systemone-router.md`, `pando/features/model-auto-mode-implementation.md`, `pando/features/persona-auto-select-decision-model.md`, `pando/features/context-enrichment-agent-loop.md`, `plans/context-enrichment-enhacement.md`; docs.ollama.com/api/systemone.
