---
id: PANDO-US-0079
type: story
title: "Router engine: build the routing question, apply threshold/none, truncate state, return a routing decision with candidates"
status: in_review
priority: high
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, agent]
estimate: 5
created: 2026-09-30T19:34:53Z
updated: 2026-09-30T21:00:23Z
started: 2026-09-30T21:00:23Z
---

## Description

As the agent, I need a pure decision component that turns (config, prompt, short history) into a `RoutingDecision`, so that the routing logic can be tested without a provider or agent loop.

Suggested package: `internal/llm/modelrouter`. The component is **backend-agnostic**. It receives a `DecisionProvider` (Ollama, TypeSafe or custom) and that backend's context budget, and it never branches on the provider kind except to decide whether to send `keep_alive`.

**Request shape** sent through the System One client:

```json
{
  "model": "<router.model>",
  "keep_alive": "<router.keepAlive>",
  "state": {"request": "<current user prompt>", "previous_requests": ["..."]},
  "questions": {
    "task": {
      "type": "choice",
      "instructions": "A developer sent this request to an AI coding assistant. Which task category best describes it?",
      "criteria": {
        "<route.id>": "<route.description>",
        "none": "None of the listed tasks, or a general request"
      }
    }
  }
}
```

`keep_alive` is sent for `ollama` only.

**Decision rules:**

1. Pick `answers.task.choice`, but only when `choice != "none"` and `probabilities[choice] >= threshold`. When `minConfidence > 0`, also require `confidence >= minConfidence`.
2. In every other case the decision is `Reason=NoMatch`, and the candidates are `[coderModel]`.
3. **Candidates** are `[route.Model, route.Fallbacks...]`. Duplicates, models unknown to the registry and models whose provider is disabled or unconfigured are removed. If nothing is left, the candidates become `[coderModel]` with `Reason=RouteUnusable`.
4. On any router error (typed errors from the client) the decision is `Reason=RouterUnavailable`, the candidates are `[coderModel]`, and the error is attached.

**`RoutingDecision` fields:**
- `RouteID`
- `Probability`
- `Confidence`
- `Probabilities` (the full map)
- `Candidates []models.ModelID`
- `Reason`
- `Latency`
- `RouterProvider`
- `RouterModel`
- `Cost *float64`
- `Err`

## Acceptance Criteria

- [ ] **Token-aware truncation.**
  - The rendered prompt (instructions + criteria + state) must fit the backend context budget minus a safety margin. Examples of budgets: Ollama `num_ctx` from `/api/show` (2050 for `tev1:0.8b`), 32K for Jev.
  - Token counts use the cheap estimator already in the repo (chars/4 or the lean-ctx estimator).
  - When the state is too long, keep its head and tail joined by an ellipsis marker.
  - The body always stays under 64 KiB.
  - Attachments and images are never sent; only their file names are listed.
- [ ] **Route descriptions that do not fit the budget are a config-time warning**, surfaced by the settings "Test connection" check. They never trigger silent truncation of route descriptions.
- [ ] Previous user prompts are included only when `historyPrompts > 0`, and each one is truncated harder.
- [ ] Disabled routes are excluded from the criteria. With zero enabled routes the router is skipped and returns `NoMatch` without any HTTP call.
- [ ] The engine is safe for concurrent sessions. It holds no per-session state; stickiness is a caller concern.
- [ ] Table-driven tests with a fake client cover:
  - match above threshold
  - match below threshold
  - `none` wins
  - ties: first route in order, as the API specifies
  - route with only an unknown model
  - router timeout
  - router 401
  - router 404
  - truncation of an 80 KB prompt under a 2050-token budget
  - `keep_alive` present for ollama only

## Notes

- `confidence` is entropy concentration, `1 - H(p)/ln(N)`, and is not calibrated correctness. With many routes it is naturally low, so it is only an optional secondary guard.
- The user-facing threshold is `p(choice)`. Default 0.60, backed by the tev1:0.8b benchmark: quick questions score 0.64–0.70, so 0.80 would reject them.

Spec: "Model auto mode: prompt routing".
