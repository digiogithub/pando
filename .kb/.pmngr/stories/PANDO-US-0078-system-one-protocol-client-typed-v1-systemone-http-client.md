---
id: PANDO-US-0078
type: story
title: "System One protocol client: typed `/v1/systemone` HTTP client (Jev API) with auth, bounded timeout, typed errors and warm-up"
status: in_review
priority: high
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, ollama]
estimate: 3
created: 2026-09-30T19:34:53Z
updated: 2026-09-30T21:00:23Z
started: 2026-09-30T21:00:23Z
---

## Description

As the router, I need a small, typed client for the System One (Jev) protocol that works the same against Ollama, TypeSafe and any compatible gateway. Decisions must be fast, bounded and fail cleanly.

Create a new package, for example `internal/llm/systemone`. This story covers the protocol only. Backend-specific discovery and health checks belong to the "Decision providers" story.

- **Constructor:** `New(Options{BaseURL, APIKey, Headers, Timeout, HTTPClient})`. The client sends `POST {BaseURL}/v1/systemone` with `Authorization: Bearer <key>` when a key is set, plus any extra headers.
- **Request types:**
  - `Request{Model, State any, Questions map[string]Question, KeepAlive any}`. `keep_alive` is sent only when set, because remote APIs ignore or reject it.
  - `Question` covers `choice`, `noul` and `score` (instructions and criteria).
- **Response types:**
  - `Response{Model, Answers map[string]Answer, Usage{InputTokens, OutputTokens, Cost *float64}}`.
  - `Answer{Type, Choice, Noul, Score, Probabilities map[string]float64, Confidence, Legend}`.
  - Probabilities are unrounded floats. The client must not assume 4-decimal rounding.
- **Warm-up:** `Warmup(ctx, model, keepAlive)` sends a minimal request. It runs at startup and on config reload when auto mode is enabled, only for Ollama, so the first user prompt does not pay the model load time.

## Acceptance Criteria

- [ ] Errors are typed, and code never branches on the message text:
  - `ErrUnreachable`
  - `ErrUnauthorized` (401/403)
  - `ErrModelNotFound` (404)
  - `ErrBadRequest` (400, including the Ollama cloud-model rejection and context overflow)
  - `ErrTooLarge` (413)
  - `ErrRateLimited` (429)
  - `ErrServer` (5xx)
  - `ErrTimeout`
  - `ErrMalformedResponse` (the answer is missing the question, or the choice is not in the criteria)

  Error bodies (`{error}` or `{error, code}`) are kept for logs only.
- [ ] Every call is bounded by the configured timeout through the context. There are no retries inside the per-prompt path.
- [ ] Request bodies over 64 KiB are refused locally with `ErrTooLarge` before any HTTP call.
- [ ] The API key never appears in logs, errors or telemetry.
- [ ] The client uses the shared HTTP transport and proxy settings. Localhost calls pass the host sandbox and portguard rules (EP-0009).
- [ ] Unit tests use an `httptest` fake Jev server and cover:
  - happy-path choice, noul and score
  - Bearer header present or absent
  - extra headers
  - 400, 401, 404, 413, 429 and 500 responses
  - timeout
  - malformed answer
  - an unrounded-probability payload (copied from the real Ollama 0.35 output)

## Notes

The client stays generic (all three question types) so other features can reuse it. Only `choice` is needed for routing.

Spec: "Model auto mode: decision providers and System One client".
