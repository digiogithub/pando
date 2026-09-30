---
created_at: 2026-09-30T21:00:54.007666127Z
updated_at: 2026-09-30T21:00:54.007666127Z
tags:
    - feature
    - model-routing
---
# Model auto mode — implementation (EP-0015, 2026-09-30)

Implements [[pando/analysis/model-auto-mode-systemone-router.md]]: per-prompt routing with a System One / Jev decision model (Ollama ≥0.35, TypeSafe Jev, custom Jev-compatible gateways). Stories US-0077..0086, specs SP-0001..0004 (22 requirements, every traced test exists and passes). Status: in_review, uncommitted.

## Packages / files
- `internal/config/model_auto_mode.go`: `ModelAutoModeConfig` (Enabled, DefaultAuto, Selected *bool global Auto, Router DecisionRouterConfig{Provider ollama|typesafe|custom, BaseURL, APIKey encrypted, Model, KeepAlive, Headers}, Threshold 0.60, MinConfidence, TimeoutMs, HistoryPrompts, Routes{ID, Description, Model, Fallbacks≤2, Disabled}); `AutoModelID="auto"`; `ValidateModelAutoMode`, `UpdateModelAutoMode`, `SetModelAutoSelected`, `ClearModelAutoModeAPIKey`, `MaskAPIKey`; Effective* helpers. agecrypto encrypts router key.
- `internal/llm/systemone`: HTTP client (`Decide`), typed errors, `DecisionProvider` (Ollama: /api/version gate, /api/tags `decision` capability filter, /api/show num_ctx; remote: /v1/models both shapes, heuristic filter), `HealthCache`, `Warmup`, suggested Ollama models + pull helpers; `systemonetest` fake server (Ollama + remote modes, /api/pull fake). Real Ollama rejects questions without `instructions`.
- `internal/llm/modelrouter`: `Engine.Route` (one `task` choice question = routes + `none`; match iff p(choice) ≥ threshold and optional minConfidence), `BuildState` token-aware truncation (bytes/3.5, head/tail, history ≤25%, 40 KiB cap), `FilterCandidates` + `DefaultLookup`, `ForConfig` cache, `health.go` shared 60s health cache + `StartWarmupOnReload`.
- `internal/llm/provider/errors_classify.go`: `ClassifyError` → ErrorClass, `ShouldFailover`, `NeedsCooldown`, `MarkToolError`; `WithMaxRetries` per-client retry budget.
- `internal/llm/agent/model_auto.go`, `model_auto_failover.go`: hook in processGeneration (coder, user turns only), turn-scoped override, switch hygiene, failover chain (Auto candidates retry 2, last 10; auth/404 5-min cooldown; no failover on cancel/context-length/content-policy/tool), warn-once per session+class, routing notice as `AgentEventTypeSystemMessage` with `AgentEvent.Routing *RoutingInfo`; package funcs `SetSessionAutoMode`, `SessionAutoMode`, `LastRoutedModel`, `LastRouting`; `SessionLLMOverrides.AutoMode`. `extevents.ModelRouted` (no prompt text). `pando_setup model auto`.
- API (`internal/api/handlers_model_auto_mode.go`): GET/PUT `/api/v1/config/model-auto-mode`, `/api/v1/model-auto-mode/router/{models,test,health,pull}`, `/playground`; `/models` lists `auto` first + `autoSelected`; `/models/active` accepts auto; SSE `system_message` {text, routing}; ChatRequest.Model "auto".
- AG-UI: CUSTOM `pando.model_routed`. ACP: auto option first/default, persisted in acp_session_state JSON, resume/load restore, last notice replay, adopts agent-side Auto.
- TUI: model dialog Auto first, status bar `Auto → model` + routing notice, settings section `settings_model_auto.go` (provider, presets, masked key, discovery/show all, pull, test connection, routes with reorder).
- WebUI: `modelAutoModeStore.ts`, `ModelAutoModeSettings.tsx` (+playground, pull, presets), ModelSwitcher Auto entry with health dot, routing chips in chat; vitest + playwright configs added.
- `cmd/doctor.go`: `pando doctor`. Docs `docs/model-auto-mode.md`. Python: `tests/model_auto_mode/{bench_router,live_providers,test_playground_live}.py`.

## Verification
`go test ./...` green; web-ui typecheck/test(11)/build OK; live Ollama tev1:0.8b: bench 27/30 (0.90), p50 63 ms, p95 70 ms. Not run: Playwright e2e, playground live test, TypeSafe/OpenRouter live.

## Deviations / gaps
- Router-unavailable notice warn-once (SP-0003 R2) wins over per-turn notice (SP-0004 R4).
- TUI routing notice only in status bar. WebUI routing rows not persisted; ACP replays last notice only.
- Context-length in Auto: compact once + retry same model.
- gintrack CLI `spec ingest/coverage` doesn't discover projects under `.kb/` (DiscoverProjects(".") without declared docs folders) → coverage shows untested despite results.
