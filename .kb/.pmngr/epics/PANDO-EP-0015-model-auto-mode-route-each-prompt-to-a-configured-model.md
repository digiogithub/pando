---
id: PANDO-EP-0015
type: epic
title: "Model auto mode: route each prompt to a configured model with a decision model (System One / Jev API: Ollama, TypeSafe Jev or any compatible provider)"
status: in_review
priority: high
author: mcp
labels: [model-routing, ollama, config, agent, webui, tui, acp]
created: 2026-09-30T19:33:11Z
updated: 2026-09-30T21:00:23Z
started: 2026-09-30T21:00:23Z
---

## Description

**Decision models.** Decision models ("System One") answer typed questions with calibrated option probabilities in milliseconds and do not generate text. The protocol is **TypeSafe's Jev API**: `POST {baseURL}/v1/systemone`. Several backends implement it:

| Backend | Base URL | Auth | Models | Notes |
|---|---|---|---|---|
| **Ollama ≥ 0.35.0** (local) | `http://localhost:11434` | none | `GET /api/tags`; decision models carry `capabilities: ["decision", …]` | `nimble` 9B, `tev1` 4B, `tev1:0.8b`. Local only: cloud models are rejected with 400. |
| **TypeSafe Jev** (hosted) | `https://api.typesafe.ai` | `Authorization: Bearer <TYPESAFE_API_KEY>` | `GET /v1/models` | `jev-latest`, currently `jev-1.13`. $0.042 per 1M input tokens, output free, 70–500 ms. |
| **Compatible providers** | any base URL + optional API key | Bearer | `GET {baseURL}/v1/models` when available | Examples: OpenRouter `https://openrouter.ai/api` (model `typesafe/jev-1.13`, `~typesafe/jev-latest`, `usage.cost`), LiteLLM proxy `https://<proxy>/typesafe`, Vercel AI Gateway, and self-hosted servers such as Kev. |

The base URL follows the TypeSafe SDK convention (`TYPESAFE_BASE_URL`): Pando appends `/v1/systemone` and `/v1/models` to it.

**What this epic adds.** Pando gets a **model auto mode**:

1. **Settings: enable and connect.**
   - The user turns auto mode on.
   - The user picks the **decision provider**: Ollama (uses the configured or auto-detected Ollama), TypeSafe Jev, or a custom Jev-compatible provider.
   - For TypeSafe and custom providers the user enters the base URL and API key.
   - The user picks the **router model** from a selector that lists **only decision models** when the backend lets us tell them apart. Ollama does, through `capabilities`. For other backends the selector lists `/v1/models`, and the user can type the name when listing is unavailable.
2. **Settings: task routes.**
   - A task route has a natural-language task description, a primary model and up to **2 fallback models**.
   - The confidence threshold is also set here.
3. **Selector.** When auto mode is enabled, **"Auto" is the first entry** of every model selector (WebUI, TUI, ACP) and the default for new sessions.
4. **Routing per prompt.** For each user prompt in an Auto session, Pando asks the decision model which task description fits.
   - It sends one `choice` question whose options are the routes plus `none`.
   - If the winner clears the threshold, the turn runs on that route's model.
   - On a provider failure the turn moves to the fallbacks, in order.
5. **No confident match.** If no route is confident, or the router is unavailable, the turn runs on the **default coder model** (`agents.coder.model`).

### Protocol facts that drive the design (docs.ollama.com/api/systemone plus the TypeSafe, OpenRouter and LiteLLM docs)

- **Request:** `{model, state, questions (1–64), keep_alive? (Ollama only)}`. A `choice` question takes `instructions` and `criteria` with 2–26 keys mapped to descriptions.
- **Response:** `answers.<q> = {type:"choice", choice, probabilities, confidence}` plus `usage`. `usage.cost` is present on some gateways.
  - Probabilities come back as **unrounded** floats.
  - `confidence = 1 - H(p)/ln(N)` is entropy concentration, **not calibrated correctness**. Routing therefore uses `probabilities[choice]`.
- **Limits:**
  - Ollama caps the body at 64 KiB and never truncates input. The rendered prompt must fit the loaded context, and `tev1:0.8b` loads with `num_ctx 2050`, so Pando must truncate with token awareness.
  - Jev hosted has a 32K context.
  - With at most 26 criteria, the route list is capped at **25 routes** (one slot goes to `none`).
- **Privacy:** hosted providers receive the prompt text, so the settings UI must say so explicitly. Ollama keeps it local.

### Codebase anchors (exploration 2026-09-30)

- **Config:**
  - `Config.ModelAutoMode` goes next to `PersonaAutoSelect` (`internal/config/config.go:1281`), following the pattern at :844 and :6187.
  - Ollama auto-detection: `tryAutoDetectOllama` (:5336). Raw base URL: `models.ResolveOllamaRawBaseURL`.
  - Sensitive fields are encrypted by `updateConfigFileAt`.
- **Agent:**
  - `processGeneration` (`internal/llm/agent/agent.go:1214`): the router hooks between persona auto-select (:1286) and `prepareProvider` (:1334), and sets the session model override (`session_overrides.go:98`).
  - Model switching: `applyPendingModelSwitch` and `sanitizeHistoryForModelSwitch` (`model_switch.go:158/248`).
  - Stream errors surface at :1384. Providers use `maxRetries=10`.
- **Selectors:**
  - WebUI: `ModelSwitcher.tsx` calls `/api/v1/models` and `/api/v1/models/active` (`handlers_models.go:382`).
  - TUI: `dialog/models.go` and `tui.go:801`.
  - ACP: `session_state.go:120/174` and `acp/agent.go:733/930`.
  - `pando_setup`: `setup_bridge_model.go:57`.
- **Events:**
  - Status messages: `emitStatus` / `addRunStatusMessage`.
  - The WebUI SSE `dispatchSSEEvent` (`handlers_chat.go:375`) does not forward system messages today.
- **Precedent:** persona auto-select (`persona_selector.go`).

## Acceptance Criteria

- [ ] **Config.** `modelAutoMode` is validated, persisted (API key encrypted, `$ENV` references allowed), hot-reloaded, lock-aware and exposed through REST and the schema. It covers:
  - enabled
  - the decision provider (`ollama` | `typesafe` | `custom`) with base URL, API key and router model
  - threshold, timeout and keep-alive
  - up to 25 routes, each with 1 primary model and 0–2 fallbacks
- [ ] **Router model selector.**
  - Ollama: lists only local models with the `decision` capability, and warns when Ollama is older than 0.35.
  - TypeSafe and custom: lists `/v1/models` using the configured key, and allows free-text entry when listing fails.
  - "Test connection" reports reachability, auth, model presence and a sample latency.
- [ ] **"Auto" entry.** When auto mode is enabled, "Auto" is the first and default entry in the WebUI ModelSwitcher, the TUI model dialog, ACP session config options and `pando_setup`. Picking a concrete model leaves Auto for that scope.
- [ ] **One router call per prompt.** Each user prompt in Auto issues one `/v1/systemone` call.
  - The turn runs on the matched route's primary model when `p(choice) ≥ threshold` and the choice is not `none`.
  - Otherwise it runs on `agents.coder.model`.
  - There are no router calls per tool iteration.
- [ ] **Router failure.** A router failure never blocks the prompt: the turn runs on the coder model, with one warning per session. Failures include: unreachable, auth (401/403), model missing, Ollama < 0.35, timeout, 4xx/5xx.
- [ ] **Provider failover.** A provider failure on the routed model fails over to fallback 1 and then fallback 2 in the same turn. Cancellation and context-length errors never trigger failover.
- [ ] **Candidate filtering.** Candidates are filtered by attachment support and by context window.
- [ ] **Visibility.** Every routed turn shows `Auto: <task> → <model> (p=…, … ms)` in every client. The notice is logged and counted in telemetry, including the router cost when `usage.cost` is present. `messages.model` records the model that actually answered.
- [ ] **Playground.** Settings include a router playground (sample prompt → per-route probabilities).
- [ ] **Specs.** The gintrack specs of this epic have every requirement traced to tests. Unit tests use a fake Jev server for each backend kind. A live check runs against Ollama 0.35 with `tev1:0.8b`, and an optional live check runs against TypeSafe or OpenRouter when a key is present.

## Notes

- **Routing question (decision).** One `choice` question with the routes plus `none: "None of the listed tasks / general request"`. One `noul` question per route is kept only as a future option.
- **Short follow-ups.** Prompts such as "yes, do it" score `none` and go to the coder model, as requested. The early benchmark confirmed this, with p = 0.71–0.93 for `none`. Two optional mitigations, both off by default: include the last N user prompts in `state`, or `lowConfidencePolicy: previous`.
- **Cost of switching models.** Changing models between turns breaks the provider prompt cache and can shrink the context window. The existing model-switch hygiene applies.
- **Scope.** Only the main coder agent is routed. Subagents, the summarizer, title generation and the persona selector are unaffected.
- **Implementation order:**
  1. US config
  2. US decision providers
  3. US client
  4. US router engine
  5. US agent integration
  6. US failover
  7. US selectors
  8. US settings UI
  9. US observability
  10. US docs and validation
- **Environment.** The developer machine runs Ollama 0.35.0 at system level, installed by the user, with `tev1:0.8b` pulled. See the epic comment for the first benchmark: 14/16 correct, p50 65 ms.
- **References:**
  - Ollama: ollama.com/blog/ollama-now-supports-jev-style-decision-models, docs.ollama.com/api/systemone, docs.ollama.com/capabilities/decision
  - OpenRouter: openrouter.ai/docs/guides/community/jev
  - LiteLLM: docs.litellm.ai/blog/typesafe_jev
  - Vercel AI Gateway: vercel.com/changelog/ai-gateway-now-supports-typesafe-clients-and-http-api-for-jev
  - Kev: github.com/jaredpalmer/kev
