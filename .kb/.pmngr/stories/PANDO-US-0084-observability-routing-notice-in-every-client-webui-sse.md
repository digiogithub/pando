---
id: PANDO-US-0084
type: story
title: "Observability: routing notice in every client, WebUI SSE system-message forwarding, ModelRouted event, telemetry and doctor check"
status: in_review
priority: medium
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, observability, webui, acp]
estimate: 3
created: 2026-09-30T19:34:53Z
updated: 2026-09-30T21:00:23Z
started: 2026-09-30T21:00:23Z
---

## Description

As a user, I want to see which model answered each prompt and why, and what routing costs when a hosted decision provider is used. This lets me trust and tune auto mode.

- **Per-turn notice:**
  - Emitted through `a.emitStatus` / `addRunStatusMessage` (agent.go:971/983).
  - ACP replays it, and AG-UI maps it to `pando.system_message`.
  - Formats:
    - `Auto: <route id> → <model> (p=0.93, 38 ms via ollama/tev1:0.8b)`
    - `Auto: no confident match (best <id> p=0.41) → <coder model>`
    - `Auto: router unavailable (<reason>) → <coder model>`
  - `<reason>` is taken from the typed error: unreachable, unauthorized, model not found, Ollama too old, timeout.
- **WebUI SSE:**
  - `dispatchSSEEvent` (`handlers_chat.go:375`) has no SystemMessage case today. Add one.
  - Render the notice in the chat as a compact chip attached to the assistant message. The user can dismiss it. It is not rendered as a full message.
- **Event bus:** add a new extension event in `internal/extevents/extevents.go`:
  `ModelRouted{SessionID, RouteID, Model, Probability, Confidence, Reason, LatencyMs, RouterProvider, RouterModel, RouterCostUSD}`.
- **Logs and telemetry:**
  - `slog` writes one Info line `model_auto:` per decision.
  - Opt-in telemetry sends counters only: routed, no-match, router-unavailable (by error class), failover (by provider), and the router's cumulative cost. It never sends prompt text.
  - The router cost goes into the savings/cost ledger when the gateway returns `usage.cost`. The ledger estimate for TypeSafe uses input tokens × price.
- **Doctor:** `pando doctor`, or the equivalent status command, checks auto mode:
  - config is valid
  - provider kind is set
  - the provider is reachable and authorized
  - Ollama version is ≥ 0.35 (Ollama only)
  - the router model is present and is a decision model
  - a warm-up latency sample
  - routes that reference unknown or disabled models

## Acceptance Criteria

- [ ] The notice is visible in TUI, WebUI, desktop, ACP (Zed/Xcode) and AG-UI for routed, no-match, router-unavailable and failover turns.
- [ ] Telemetry never contains prompt content or API keys. Debug logs may include probabilities.
- [ ] The model shown per message in the chat header or tooltip matches `messages.model`.
- [ ] Tests cover:
  - SSE forwarding
  - the `ModelRouted` payload, including provider and cost
  - doctor output for each provider kind, run against fake servers

## Notes

- Optional follow-up, out of scope: persist routing decisions to feed the evaluator (EP-0014), and suggest threshold or route tuning from reward data.
- Spec: "Model auto mode: selectors, settings and observability".
