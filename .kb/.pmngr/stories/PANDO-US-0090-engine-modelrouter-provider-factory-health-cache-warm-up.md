---
id: PANDO-US-0090
type: story
title: "Engine: `modelrouter` provider factory, health cache, warm-up and engine cache keyed on `DecisionModelConfig`; model auto mode and persona auto-select migrated as consumers with unchanged behaviour"
status: in_review
priority: high
parent: PANDO-EP-0018
author: mcp
labels: [decision-model, model-routing, persona, engine]
estimate: 5
created: 2026-10-01T16:18:26Z
updated: 2026-10-01T16:37:00Z
started: 2026-10-01T16:37:00Z
---

## Description

As a maintainer, I want one place that turns `decisionModel` into a `systemone.DecisionProvider`, with health and warm-up, so that every consumer (model routes, personas, context filter) reuses it and none needs a `ModelAutoModeConfig`.

Refactor `internal/llm/modelrouter`:

- `ProviderFor(dm config.DecisionModelConfig) (systemone.DecisionProvider, error)`; `RouterHealth(ctx, dm)`; warm-up (`health.go:65`) runs when `dm.Router.Model != ""` and `config.AnyDecisionConsumerEnabled()` (auto mode enabled ∨ persona-selector `useDecisionModel` ∨ context filter enabled).
- `NewEngine(dm config.DecisionModelConfig, opts...)`; `Engine.Route(ctx, policy config.ModelAutoModeConfig, in Input)` and `RoutePersona/RouteWithPersona` take the per-consumer policy (threshold, minConfidence, historyPrompts, routes) as arguments instead of reading them from the engine's config. `configKey`/`ForConfig` cache is keyed on the decision block only.
- Add a generic `Engine.Ask(ctx, state string, qs map[string]systemone.Question) (*systemone.Response, latency, error)` with the existing token-budget truncation and `classify(err)` so the context filter (next story) does not re-implement the request path.
- Update consumers: `internal/llm/agent/model_auto.go` (`beginAutoTurn`, `RouterProvider/RouterModel` in notices), `persona_decision.go:277` (cache key), `modelrouter/health.go`, `internal/mesnada/acp/session_state.go:579`, `internal/tui/page/settings_persona_health.go`, `cmd/doctor.go`, `handlers_models.go:autoModelInfo`.

## Acceptance Criteria

- [ ] No reference to `ModelAutoMode.Router` remains outside the migration code (`grep -rn "ModelAutoMode.Router" internal cmd` empty except `internal/config/decision_model.go`).
- [ ] Existing tests in `internal/llm/modelrouter`, `internal/llm/agent` (`model_auto*_test.go`, `persona_decision*_test.go`), `internal/api`, `internal/mesnada/acp`, `cmd` pass after adapting fixtures to the new block.
- [ ] Combined request test still asserts one `/v1/systemone` call when auto mode and persona decision are both on.
- [ ] Persona auto-select works with `modelAutoMode.enabled=false` when `decisionModel.router.model` is set (test).
- [ ] Warm-up test: runs with only the context filter enabled; does not run when no consumer is on.
- [ ] Hot reload: a `decisionModel` config event invalidates the engine cache and health entry; the next prompt uses the new provider (test with `systemonetest` switching base URLs).
- [ ] `Engine.Ask` covered by a unit test for truncation to the context budget and for `MaxQuestions`/`MaxBodyBytes` rejection.

## Notes

- Anchors: `engine.go:97-155`, `:238` (`ask`), `:298` (`contextBudget`), `:316` (`classify`), `persona.go:82-215`, `health.go`.
- Keep `Decision`/`PersonaDecision` payloads identical so WebUI `RoutingNotice` and ACP notices do not change.
- Spec: "Decision model: shared engine and consumers".
