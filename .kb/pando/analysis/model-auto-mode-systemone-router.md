---
created_at: 2026-09-30T19:35:11.996078137Z
updated_at: 2026-09-30T20:05:02.850538595Z
tags:
    - analysis
    - plan
    - model-routing
    - ollama
---
# Model auto mode via System One / Jev decision models — analysis and plan (2026-09-30)

The work is tracked in Gintrack:

- **Epic:** PANDO-EP-0015.
- **Stories:** PANDO-US-0077 to US-0086.
- **Specs:** PANDO-SP-0001 to SP-0004, with 22 requirements, each traced to planned tests.

Everything is in backlog.

## Protocol: TypeSafe Jev API ("System One")

**Endpoint and request.** `POST {baseURL}/v1/systemone` takes:

- `model`
- `state`: a string or JSON value
- `questions`: 1 to 64 entries
- `keep_alive`: optional, Ollama only

**Question types.**

| Type | Input | Result |
|---|---|---|
| `choice` | criteria: map of 2–26 keys to a description or null | `{choice, probabilities, confidence}` |
| `noul` | yes/no question | P(true) |
| `score` | ordered array of options | probability-weighted index |

**Usage.** Every response includes `usage`. Some gateways also add `usage.cost`.

**How to read the numbers.**

- `confidence = 1 - H(p)/ln(N)`. It measures entropy concentration and is NOT calibrated correctness. Pando routes on `probabilities[choice]` compared with a threshold.
- Ollama 0.35 returns probabilities as unrounded floats.

## Backends

- **Ollama ≥ 0.35.0, local.**
  - Uses `POST /v1/systemone` with no auth.
  - `GET /api/tags` lists the models. The `capabilities` field contains `"decision"` for decision models: `tev1:0.8b` reports `[decision, tools, thinking, completion]`, while `qwen2.5-coder:0.5b` reports `[completion, tools, insert]`.
  - `GET /api/version` is used for the version gate.
  - `/api/show` gives `num_ctx`. For `tev1:0.8b` it is **2050**, a tiny budget.
  - Only local models are accepted; cloud models return 400. The request body is capped at 64 KiB (413). Input is never truncated by the server. A model that is not pulled returns 404.
  - Available models: `nimble` (9B, Bespoke), `tev1` (4B, Together) and `tev1:0.8b`.
- **TypeSafe Jev, hosted.**
  - Base URL `https://api.typesafe.ai`, with `Authorization: Bearer $TYPESAFE_API_KEY`.
  - `GET /v1/models` returns a models array.
  - Model `jev-latest` (currently jev-1.13).
  - Context 32K. Price $0.042 per 1M input tokens; output is free. Latency 70–500 ms.
- **Compatible gateways.** The base URL is the root, following the `TYPESAFE_BASE_URL` convention.
  - OpenRouter: `https://openrouter.ai/api`, models `typesafe/jev-1.13` and `~typesafe/jev-latest`. Returns `usage.cost`.
  - LiteLLM proxy: `/typesafe/v1/systemone` and `/typesafe/v1/models`.
  - Vercel AI Gateway.
  - Kev (self-hosted, for example `localhost:8009`).

## Design decisions

1. **Router question.** One `choice` question with the enabled routes plus `none`. This allows at most 25 routes. Route when `choice != none` and `p >= threshold` (default 0.60); otherwise use the coder model.
2. **Router failure.** On any router failure (unreachable, 401/403, 404, old Ollama, timeout, 4xx/5xx), use the coder model. Warn once per session for each error class.
3. **Router configuration.** The router is set by `modelAutoMode.router{provider: ollama|typesafe|custom, baseURL, apiKey (encrypted/masked/$ENV, TypeSafe falls back to $TYPESAFE_API_KEY), model, keepAlive, headers}`.
4. **Discovery.**
   - Ollama: filter models by the `decision` capability.
   - TypeSafe: all listed models count as decision models.
   - Custom: parse `{models}` and `{data:[{id}]}`. Narrow mixed catalogues with a heuristic and offer "show all". If the listing fails, accept a free-text model name.
5. **Candidate chain.** `[primary, fb1, fb2]`, filtered for unknown or disabled models, attachment support and context window.
6. **Failover.** Applies to Auto turns only. Each candidate except the last has a reduced retry budget; the provider default is `maxRetries=10`. Failover reuses `applyPendingModelSwitch`. It does not trigger on cancellation, context-length errors, content-policy errors or tool errors. There is a cooldown after auth or 404 failures.
7. **Auto selection.** The pseudo model id `auto` is the first and default entry in every selector. It is tracked as a flag (global for WebUI/TUI, per session for ACP) and never written into `agents.coder.model`.
8. **Routing frequency.** Routing runs once per user prompt, never per tool iteration. Subagents, the summarizer, the title agent and the persona agent are not routed.
9. **Token-aware truncation.** State is truncated to fit each provider's context budget.
10. **Privacy.** A notice is shown when a remote provider is used. Telemetry records counters and cost only, never the prompt or the key.

## Early benchmark (tev1:0.8b, Ollama 0.35.0, RTX 4000 SFF Ada)

Run on 16 developer prompts in English and Spanish, with 4 routes plus `none`.

- **Accuracy and latency:** 14/16 correct. p50 65 ms, p95 71 ms; cold start 78 ms.
- **By route:**
  - Implementation and planning prompts: p 0.83–0.99.
  - Quick questions: p 0.64–0.70. A threshold of 0.80 would reject them, so the default is 0.60.
  - Short follow-ups: routed to `none` with p 0.71–0.93, which sends them to the coder model as intended.
- **Misses:**
  - "translate…" was routed to `none`.
  - "write the commit message" was routed to implementation with p 0.73, a confident misroute.
- **Takeaway:** the wording of route descriptions matters more than the threshold. The playground is essential.

## Code anchors

- **Config:** `Config.ModelAutoMode` goes next to `PersonaAutoSelect` (config.go:1281). Follow the patterns at :844 and :6187.
- **Agent:**
  - Hook point: between persona auto-select (agent.go:1286) and `prepareProvider` (:1334).
  - Use `SetSessionModelOverride` (session_overrides.go:98).
  - Stream errors are handled at agent.go:1384.
- **Selectors:**
  - WebUI: `ModelSwitcher.tsx`, with `/api/v1/models` and `/models/active` (handlers_models.go:382).
  - TUI: dialog/models.go and tui.go:801.
  - ACP: session_state.go:120/174 and agent.go:733/930.
  - pando_setup: setup_bridge_model.go:57.
- **Events:**
  - Use `emitStatus`/`addRunStatusMessage`.
  - The WebUI `dispatchSSEEvent` (handlers_chat.go:375) lacks a SystemMessage case.
  - Add a new extevents `ModelRouted` event.
- **Ollama:**
  - `ResolveOllamaRawBaseURL`
  - `tryAutoDetectOllama` (config.go:5336)
  - `internal/ollamasetup` for pulling models
- **Precedent:** persona_selector.go.
- **Planned packages:**
  - `internal/llm/systemone`: client, providers, and the `systemonetest` fake server with recorded 0.35 fixtures.
  - `internal/llm/modelrouter`: engine, state, candidates.
  - `tests/model_auto_mode/*.py`: live benchmark scripts, opt-in via `PANDO_LIVE_OLLAMA=1`, `TYPESAFE_API_KEY` or `PANDO_LIVE_JEV_BASEURL`/`KEY`.

## Environment

The user installs and upgrades Ollama at system level; the agent must not install it. The developer machine runs Ollama 0.35.0 with `tev1:0.8b` pulled.

Related: [[claude_code_improvements]] (missing fallback model concept), [[analisis-switch-dinamico-treesitter-lsp]], [[dynamic-model-switching-analysis]].
