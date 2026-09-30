---
id: PANDO-US-0086
type: story
title: "Decision providers: Ollama, TypeSafe Jev and custom Jev-compatible backends — discovery of decision models, health check and \"Test connection\""
status: in_review
priority: high
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, ollama, providers, api]
estimate: 5
created: 2026-09-30T19:59:53Z
updated: 2026-09-30T21:00:23Z
started: 2026-09-30T21:00:23Z
---

## Description

As a user, I want to route with local Ollama decision models, with TypeSafe's hosted Jev, or with any Jev-compatible gateway. Examples of such gateways are OpenRouter, a LiteLLM proxy, Vercel AI Gateway, or a self-hosted server such as Kev. I want to pick the router model from a list that only contains decision models.

Add a `DecisionProvider` interface in `internal/llm/systemone` (or `internal/llm/modelrouter/providers`):

```go
type DecisionProvider interface {
    Kind() DecisionProviderKind
    Client() *systemone.Client
    ListDecisionModels(ctx context.Context) ([]DecisionModel, ListStatus, error) // ListStatus: filtered | unfiltered | unsupported
    Health(ctx context.Context, model string) HealthReport
}
type DecisionModel struct { ID, DisplayName string; SizeBytes int64; ContextLength int; Local bool }
type HealthReport struct { Reachable, Authorized, VersionOK, ModelPresent, IsDecisionModel bool; Version string; LatencyMs int; Problems []string }
```

**Backend behaviour:**

- **Ollama** (`ollama`):
  - Base URL is the configured Ollama raw base URL.
  - Discovery calls `GET /api/tags`. It keeps entries whose `capabilities` contains `"decision"`, which Ollama 0.35 exposes; for example `tev1:0.8b` reports `["decision","tools","thinking","completion"]`. It excludes names ending in `:cloud`.
  - If the capabilities field is missing, which happens with older Ollama, the result is `ListStatus=unsupported` with a version hint.
  - Health:
    1. `GET /api/version` must report ≥ 0.35.0.
    2. The model must be present in the tags list.
    3. The model must have the decision capability.
    4. A one-question warm-up probe records the latency.
  - Pulling a model uses the `internal/ollamasetup` pull for the suggested models `tev1:0.8b`, `tev1` and `nimble`.
  - `num_ctx` is read from `/api/show` (for example 2050 for `tev1:0.8b`) and passed to the router engine as the context budget.
- **TypeSafe Jev** (`typesafe`):
  - Base URL is `https://api.typesafe.ai` and auth is a Bearer key.
  - Discovery calls `GET /v1/models`. Every entry counts as a decision model, so `ListStatus=filtered`.
  - Health:
    - a 401 or 403 response means `Authorized=false`
    - a probe with one tiny question records the latency
  - The context budget is 32K tokens, as documented for `jev-1.13`.
- **Custom** (`custom`):
  - The user sets the base URL and an optional key.
  - Discovery calls `GET {baseURL}/v1/models` and parses both `{"models":[…]}` and the OpenAI-style `{"data":[{"id":…}]}`.
  - On gateways that list every kind of model (for example OpenRouter), entries are narrowed with a best-effort heuristic: the id contains `jev`, `systemone`, `decision`, `tev`, `nimble` or `kev`, or the gateway exposes a decision or `systemone` capability flag. The result is then `ListStatus=unfiltered`, and the UI offers "show all".
  - If listing fails, the result is `ListStatus=unsupported`, and the UI accepts a model id typed by hand.
  - Health uses the same probe.

**REST endpoints:**
- `GET /api/v1/model-auto-mode/router/models?provider=&baseURL=`
  - Uses the stored key unless a draft key is sent in the body of the POST variant.
  - Returns the models, `listStatus` and problems.
- `POST /api/v1/model-auto-mode/router/test`
  - Takes a draft router config and returns a `HealthReport`.
- Both endpoints are rate-limited and never echo the API key.

## Acceptance Criteria

- [ ] Ollama discovery returns `tev1:0.8b` and hides embedding and chat models such as `nomic-embed-text` and `qwen2.5-coder:0.5b`. This is verified against a recorded `/api/tags` fixture from Ollama 0.35.0 and, when available, live.
- [ ] If Ollama is older than 0.35, or has no decision models, the result explains the fix: "Upgrade Ollama to ≥ 0.35" or "ollama pull tev1:0.8b".
- [ ] TypeSafe and custom providers with a wrong key report `Authorized=false` and are never marked healthy. An unreachable base URL reports `Reachable=false`.
- [ ] Custom discovery handles both list shapes, heuristic filtering and the free-text fallback.
- [ ] Health results are cached for about 60 s per (kind, baseURL, model) and invalidated on config change.
- [ ] Unit tests use fake servers for each backend kind. Optional live tests are gated by the env variables `PANDO_LIVE_OLLAMA=1`, `TYPESAFE_API_KEY` and `PANDO_LIVE_JEV_BASEURL`/`PANDO_LIVE_JEV_KEY`.

## Notes

- **Privacy:** `typesafe` and `custom` send prompt text off the machine. Health reports carry `Remote=true` so the UI can show the notice.
- **Cost:** Jev is billed per input token ($0.042 per 1M, output free). The router records `usage.cost` when the gateway returns it (OpenRouter does).

Spec: "Model auto mode: decision providers and System One client".
