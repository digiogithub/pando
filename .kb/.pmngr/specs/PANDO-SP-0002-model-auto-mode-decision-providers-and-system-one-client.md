---
id: PANDO-SP-0002
type: spec
title: "Model auto mode: decision providers and System One client"
status: in_review
priority: high
author: mcp
labels: [model-routing, ollama, providers, PANDO-EP-0015]
created: 2026-09-30T20:01:14Z
updated: 2026-10-01T07:57:58Z
started: 2026-09-30T21:06:56Z
requirements:
  R1:
    status: done
    trace:
      code: [internal/llm/systemone/client.go]
      tests:
        - internal/llm/systemone/client_test.go#TestClientChoiceDecoding
        - internal/llm/systemone/client_test.go#TestClientAuthHeaders
        - internal/llm/systemone/client_test.go#TestClientUsageCost
        - internal/llm/systemone/client_test.go#TestQuestionMarshalOrder
    verified: {rev: "sha256:ebca3b2d554465d4", commit: 95b34f12792a30bf862007ec8ae53a0c7e87afb5, at: 2026-10-01T07:56:56Z, by: José F. Rives}
  R2:
    status: done
    trace:
      code: [internal/llm/systemone/client.go, internal/llm/systemone/errors.go]
      tests:
        - internal/llm/systemone/client_test.go#TestClientTimeout
        - internal/llm/systemone/client_test.go#TestClientErrorMapping
        - internal/llm/systemone/client_test.go#TestClientTooLarge
        - internal/llm/systemone/client_test.go#TestClientMalformed
    verified: {rev: "sha256:d1459ee9b6bbffc9", commit: 95b34f12792a30bf862007ec8ae53a0c7e87afb5, at: 2026-10-01T07:56:56Z, by: José F. Rives}
  R3:
    status: done
    trace:
      code:
        - internal/llm/systemone/provider_ollama.go
        - internal/llm/systemone/provider.go
      tests:
        - internal/llm/systemone/provider_ollama_test.go#TestOllamaListDecisionModels
        - internal/llm/systemone/provider_ollama_test.go#TestOllamaVersionGate
        - internal/llm/systemone/provider_ollama_test.go#TestOllamaContextBudget
        - internal/llm/systemone/provider_ollama_test.go#TestOllamaDefaultKeepAlive
        - internal/llm/systemone/provider_ollama_live_test.go#TestOllamaLive
    verified: {rev: "sha256:5ed9fce29f46970f", commit: 95b34f12792a30bf862007ec8ae53a0c7e87afb5, at: 2026-10-01T07:56:56Z, by: José F. Rives}
  R4:
    status: in_review
    trace:
      code: [internal/llm/systemone/provider_remote.go]
      tests:
        - internal/llm/systemone/provider_remote_test.go#TestTypeSafeListModels
        - internal/llm/systemone/provider_remote_test.go#TestRemoteUnauthorized
        - internal/llm/systemone/provider_remote_test.go#TestCustomCatalogueFiltering
        - internal/llm/systemone/provider_remote_test.go#TestCustomNoModelsEndpoint
        - internal/llm/systemone/provider_remote_live_test.go#TestRemoteLive
  R5:
    status: done
    trace:
      code:
        - internal/api/handlers_model_auto_mode.go
        - internal/llm/systemone/health.go
        - internal/llm/modelrouter/health.go
      tests:
        - internal/api/handlers_model_auto_mode_test.go#TestRouterTestEndpointDraft
        - internal/api/handlers_model_auto_mode_test.go#TestRouterEndpointsNeverEchoKey
        - internal/api/handlers_model_auto_mode_test.go#TestRouterModelsEndpoint
        - internal/api/handlers_model_auto_mode_test.go#TestRouterModelsSuggestionsAndPull
        - internal/llm/systemone/health_test.go#TestHealthCache
        - internal/llm/systemone/health_test.go#TestWarmupOnReload
    verified: {rev: "sha256:79a32b1c826ca405", commit: 95b34f12792a30bf862007ec8ae53a0c7e87afb5, at: 2026-10-01T07:56:56Z, by: José F. Rives}
---

## Purpose

Pando talks to decision models through the System One (Jev) protocol, `POST {baseURL}/v1/systemone`. This spec covers three backends:

- Ollama 0.35 or later, local
- TypeSafe Jev, hosted
- any Jev-compatible gateway, such as OpenRouter, LiteLLM, Vercel AI Gateway or Kev

For each backend it defines authentication, bounded calls, typed errors, discovery of decision models, health checks and the REST endpoints used by settings.

## Scope

In scope:
- `internal/llm/systemone`: the client, the providers and the `systemonetest` fake server
- `GET /api/v1/model-auto-mode/router/models`
- `POST /api/v1/model-auto-mode/router/test`

Epic: PANDO-EP-0015. Stories: PANDO-US-0078, PANDO-US-0086.

## Requirements

### PANDO-SP-0002.R1 — System One request/response and authentication

The client SHALL send `POST {baseURL}/v1/systemone` with a JSON body `{model, state, questions, keep_alive?}`.

- When an API key is configured, the request SHALL carry `Authorization: Bearer <key>`.
- Every configured extra header SHALL be sent.
- `keep_alive` SHALL be sent only when it is set.

The client SHALL decode `choice`, `noul` and `score` answers, and SHALL decode `usage` including an optional `cost`. Probabilities SHALL be accepted at full float precision, without assuming any rounding.

#### Scenario: Choice answer decoded
- GIVEN a fake server that returns the recorded Ollama 0.35 payload `{"answers":{"intent":{"type":"choice","choice":"duplicate_charge","probabilities":{"duplicate_charge":0.9760968387170561,...},"confidence":0.9145015492062412}}}`
- WHEN the client sends a request with one choice question
- THEN `Answers["intent"].Choice == "duplicate_charge"`
- AND `Probabilities["duplicate_charge"] == 0.9760968387170561`

#### Scenario: Bearer header
- GIVEN the client is configured with the key `k1`
- WHEN a request is sent
- THEN the server receives `Authorization: Bearer k1`

#### Scenario: No key means no header
- GIVEN no API key is configured (Ollama)
- WHEN a request is sent
- THEN the request has no `Authorization` header

#### Scenario: Gateway cost
- GIVEN the response has `usage.cost=0.0000042`
- WHEN it is decoded
- THEN `Usage.Cost` is non-nil and equals 0.0000042

### PANDO-SP-0002.R2 — Bounded calls and typed errors

Every System One call SHALL be bounded by the configured timeout. The client SHALL NOT retry internally.

- Request bodies larger than 64 KiB SHALL be refused locally, without making a network call.
- HTTP failures SHALL map to typed errors:

| Failure | Typed error |
|---|---|
| unreachable | `ErrUnreachable` |
| 401/403 | `ErrUnauthorized` |
| 404 | `ErrModelNotFound` |
| 400 | `ErrBadRequest` |
| 413 | `ErrTooLarge` |
| 429 | `ErrRateLimited` |
| 5xx | `ErrServer` |
| deadline | `ErrTimeout` |
| an answer missing the question, or a choice not among the criteria | `ErrMalformedResponse` |

API keys SHALL NOT appear in error strings.

#### Scenario: Timeout
- GIVEN a fake server that sleeps 2 s and a timeout of 300 ms
- WHEN a request is sent
- THEN `errors.Is(err, ErrTimeout)` is true
- AND the call returns in less than 400 ms

#### Scenario: Status mapping
- GIVEN a fake server that returns 401, 404, 413, 429 and then 500
- WHEN a request is sent for each
- THEN the typed errors are `ErrUnauthorized`, `ErrModelNotFound`, `ErrTooLarge`, `ErrRateLimited` and `ErrServer`, in that order

#### Scenario: Oversized body refused locally
- GIVEN a state of 70 KiB
- WHEN a request is sent
- THEN `ErrTooLarge` is returned
- AND the fake server receives no request

#### Scenario: Malformed answer
- GIVEN a response whose choice is `"foo"`, which is not among the criteria
- WHEN it is decoded
- THEN `ErrMalformedResponse` is returned

### PANDO-SP-0002.R3 — Ollama backend: decision-model discovery, version gate and context budget

The Ollama decision provider SHALL list the router models from `GET /api/tags`, keeping only entries whose `capabilities` contains `"decision"` and whose name does not end in `:cloud`.

If no entry exposes `capabilities`, the provider SHALL return `ListStatus=unsupported` together with an upgrade hint.

Health SHALL fail with a version problem when `GET /api/version` reports a version below 0.35.0.

The provider SHALL expose the router model's context budget, read from the `num_ctx` in `/api/show` parameters, or from `context_length` when `num_ctx` is absent.

#### Scenario: Only decision models listed
- GIVEN the recorded Ollama 0.35 `/api/tags` fixture with `tev1:0.8b` (capabilities decision, tools, thinking, completion), `qwen2.5-coder:0.5b` (completion, tools, insert), `nomic-embed-text` (embedding) and `glm-5.1:cloud`
- WHEN the provider lists models
- THEN the result is exactly `[tev1:0.8b]`
- AND `ListStatus=filtered`

#### Scenario: Old Ollama
- GIVEN `/api/version` returns `0.32.14`
- WHEN health runs
- THEN `VersionOK=false`
- AND the problems include "Upgrade Ollama to ≥ 0.35"

#### Scenario: No decision model pulled
- GIVEN Ollama 0.35 with no decision models installed
- WHEN the provider lists models
- THEN the list is empty
- AND the hint suggests `ollama pull tev1:0.8b`

#### Scenario: Context budget from num_ctx
- GIVEN `/api/show` for `tev1:0.8b` returns parameters `num_ctx 2050`
- WHEN the context budget is read
- THEN the budget is 2050 tokens

#### Scenario: Live Ollama (opt-in)
- GIVEN `PANDO_LIVE_OLLAMA=1` and a local Ollama ≥ 0.35 with `tev1:0.8b` installed
- WHEN the live test runs
- THEN `tev1:0.8b` is listed
- AND health reports all checks passing with latency under 1000 ms

### PANDO-SP-0002.R4 — TypeSafe Jev and custom Jev-compatible backends

The `typesafe` and `custom` decision providers SHALL list models from `GET {baseURL}/v1/models` using the configured key. They SHALL parse both the `{"models":[...]}` shape (strings or objects) and the OpenAI-style `{"data":[{"id":...}]}` shape.

- **`typesafe`:** every listed model SHALL be treated as a decision model (`ListStatus=filtered`).
- **`custom` with a mixed catalogue:** the result SHALL be narrowed with the heuristic (id contains `jev`, `systemone`, `decision`, `tev`, `nimble` or `kev`) and reported as `ListStatus=unfiltered`, so the full list stays available to the UI.
- **Listing failure:** the provider SHALL return `ListStatus=unsupported` so that a model id can be typed by hand.
- **Health:** 401 or 403 SHALL yield `Authorized=false`. The provider SHALL be flagged `Remote=true`.

#### Scenario: TypeSafe models
- GIVEN a fake TypeSafe server that returns `{"models":["jev-latest","jev-1.13"]}` when called with `Bearer good`
- WHEN models are listed with the key `good`
- THEN the result is `[jev-latest, jev-1.13]`
- AND `ListStatus=filtered`

#### Scenario: Wrong key
- GIVEN the same server and the key `bad`
- WHEN health runs
- THEN `Authorized=false`
- AND the provider is not healthy

#### Scenario: OpenRouter-style catalogue
- GIVEN a custom server that returns `{"data":[{"id":"openai/gpt-5"},{"id":"typesafe/jev-1.13"},{"id":"~typesafe/jev-latest"}]}`
- WHEN models are listed
- THEN the filtered result is `[typesafe/jev-1.13, ~typesafe/jev-latest]`
- AND `ListStatus=unfiltered`

#### Scenario: No models endpoint
- GIVEN a custom server that returns 404 on `/v1/models` but answers `/v1/systemone`
- WHEN models are listed
- THEN `ListStatus=unsupported`
- AND a health probe with a typed model id succeeds

#### Scenario: Live hosted Jev (opt-in)
- GIVEN `TYPESAFE_API_KEY`, or `PANDO_LIVE_JEV_BASEURL` plus `PANDO_LIVE_JEV_KEY`, is set
- WHEN the live test runs
- THEN at least one model is listed
- AND a one-question probe returns a valid choice

### PANDO-SP-0002.R5 — Router discovery/test REST endpoints and health cache

The API SHALL expose two endpoints that accept draft router settings and SHALL never echo the API key:

- `GET /api/v1/model-auto-mode/router/models` (plus a POST variant that takes a draft key) SHALL return `{models, listStatus, problems}`.
- `POST /api/v1/model-auto-mode/router/test` SHALL return the `HealthReport`: `reachable`, `authorized`, `versionOK`, `modelPresent`, `isDecisionModel`, `remote`, `latencyMs` and `problems`.

Health results SHALL be cached for about 60 s per (kind, baseURL, model). The cache SHALL be invalidated when the config changes.

The Ollama provider SHALL warm up the router model with `keep_alive` at startup and on config reload when auto mode is enabled.

#### Scenario: Draft test before saving
- GIVEN saved settings use Ollama
- WHEN `/router/test` is posted with a draft `{provider:"custom", baseURL:<fake>, apiKey:"k"}`
- THEN the report reflects the fake server
- AND the saved config is unchanged

#### Scenario: Key never echoed
- GIVEN a draft with `apiKey="secret"`
- WHEN any router endpoint answers
- THEN the response body does not contain `secret`

#### Scenario: Health cache
- GIVEN a successful health check
- WHEN health is requested again within 60 s
- THEN the fake server receives no new probe
- AND after a config change, it receives one

#### Scenario: Warm-up on enable
- GIVEN auto mode is enabled with Ollama and `keepAlive=30m`
- WHEN the config is reloaded
- THEN one warm-up request with `keep_alive:"30m"` reaches the server
