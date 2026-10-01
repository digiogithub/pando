---
id: PANDO-SP-0006
type: spec
title: "Decision model: shared engine and consumers"
status: backlog
priority: high
author: mcp
labels: [decision-model, model-routing, PANDO-EP-0018]
created: 2026-10-01T17:06:48Z
updated: 2026-10-01T17:10:58Z
requirements:
  R1:
    status: backlog
    trace:
      code:
        - internal/llm/modelrouter/engine.go
        - internal/llm/modelrouter/health.go
      tests:
        - internal/llm/modelrouter/engine_test.go#TestForConfigCaching
        - internal/llm/modelrouter/ask_test.go#TestForConfigHotReloadSwitchesProvider
    verified: {rev: "sha256:f1db305c206160d1", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R2:
    status: backlog
    trace:
      code:
        - internal/llm/modelrouter/engine.go
        - internal/llm/modelrouter/state.go
      tests:
        - internal/llm/modelrouter/ask_test.go#TestEngineAskAnswersManyQuestions
        - internal/llm/modelrouter/ask_test.go#TestEngineAskRejections
        - internal/llm/modelrouter/ask_test.go#TestEngineAskStateTruncatedToBudget
    verified: {rev: "sha256:72ca018559a20208", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R3:
    status: backlog
    trace:
      code: [internal/llm/modelrouter/health.go, internal/config/config.go]
      tests:
        - internal/llm/modelrouter/ask_test.go#TestWarmupRunsOnlyWithAConsumerEnabled
        - internal/llm/modelrouter/ask_test.go#TestWarmupInvalidatesHealth
        - internal/config/decision_model_test.go#TestAnyDecisionConsumerEnabled
    verified: {rev: "sha256:34f71147d97ecba9", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R4:
    status: backlog
    trace:
      code:
        - internal/llm/modelrouter/persona.go
        - internal/llm/agent/persona_decision.go
      tests:
        - internal/llm/modelrouter/ask_test.go#TestPersonaRoutingWithAutoModeDisabled
        - internal/llm/modelrouter/persona_test.go#TestRouteWithPersonaOneRequest
        - internal/llm/agent/persona_decision_test.go#TestPersonaCombinedRequestOneCall
        - internal/llm/agent/persona_decision_test.go#TestPersonaDecisionWithAutoModeOffSendsPersonaQuestionOnly
    verified: {rev: "sha256:8b5ea045b555abec", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R5:
    status: backlog
    trace:
      code:
        - internal/llm/modelrouter/engine.go
        - internal/llm/agent/model_auto.go
      tests:
        - internal/llm/modelrouter/engine_test.go#TestEngineDecisionRules
        - internal/llm/agent/model_auto_test.go#TestAutoRoutesOncePerUserTurn
        - internal/llm/agent/model_auto_test.go#TestAutoRouterDownUsesCoder
    verified: {rev: "sha256:bb8e89242d6db391", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
---

## Purpose

This spec defines how one decision-model engine, built from the `decisionModel` block alone, serves several independent consumers: model auto mode, persona auto-select and the context relevance filter. It covers the engine cache, generic question asking, health and warm-up.

## Scope

In scope:
- `internal/llm/modelrouter` (`ForConfig`, `Engine.Ask`, health cache, warm-up)
- `config.AnyDecisionConsumerEnabled`
- the independence of persona auto-select from model auto mode

Out of scope: the block itself (spec "Decision model: configuration, migration and REST") and the relevance filter (specs "Relevance filter: ...").

Epic: PANDO-EP-0018. Stories: PANDO-US-0090, PANDO-US-0096.

## Requirements

### PANDO-SP-0006.R1 — Engine cache keyed only on the decision model block

The system SHALL build one decision-model engine per distinct `decisionModel` block through `modelrouter.ForConfig`, reuse it while the block is unchanged, and rebuild it when the provider, base URL, model or key changes. Consumer policy (for example the auto mode threshold) SHALL NOT be part of the engine key and the consumer passes its policy on each call.

#### Scenario: Reuse and rebuild
- GIVEN an engine built for a block
- WHEN the same block is requested again, or only the auto mode threshold changes
- THEN the same engine is returned
- AND WHEN `router.model` changes THEN a new engine is built

#### Scenario: Hot reload switches provider
- GIVEN a running engine for provider A
- WHEN the config now points at provider B
- THEN the next call is served by provider B without a restart

### PANDO-SP-0006.R2 — Engine.Ask answers many questions within protocol bounds

`Engine.Ask` SHALL send a caller-built state and up to 64 named questions in one System One request and return the answers. It SHALL refuse more than 64 questions or a state above the 64 KiB body cap without calling the provider, and state built for a request SHALL be truncated to the model's context budget.

#### Scenario: Many questions, one request
- GIVEN 10 questions
- WHEN `Ask` runs
- THEN exactly one `/v1/systemone` request is sent and every answer is returned

#### Scenario: Rejections
- GIVEN 65 questions, or a state larger than the body cap
- WHEN `Ask` runs
- THEN a bad-request or too-large error is returned and no request is sent

#### Scenario: Context budget
- GIVEN a model with a small context window and a very long prompt
- WHEN the state is built
- THEN it is truncated so state plus questions fit the budget

### PANDO-SP-0006.R3 — Warm-up and health follow the consumers

The system SHALL warm the decision model only when `decisionModel.router.model` is set AND at least one consumer is enabled (`config.AnyDecisionConsumerEnabled`: model auto mode, the persona-selector `useDecisionModel`, or either relevance filter flag). A reload SHALL drop the cached health so the next health read probes the provider again; otherwise health SHALL be cached.

#### Scenario: No consumer, no warm-up
- GIVEN a configured model and no consumer enabled
- WHEN warm-up starts
- THEN it does not run

#### Scenario: Only the context filter
- GIVEN only `ContextEnrichmentDecisionFilterEnabled=true`
- WHEN warm-up starts
- THEN a warm-up request is sent
- AND with an empty model it does not run

#### Scenario: Health cache
- GIVEN a health report was read
- WHEN health is read again
- THEN the provider is not probed again
- AND after a warm-up/reload it is probed again

#### Scenario: Consumer detection
- GIVEN each of the four switches in turn
- WHEN `AnyDecisionConsumerEnabled` is evaluated
- THEN it is true for each and false when all are off

### PANDO-SP-0006.R4 — Persona auto-select is independent of model auto mode

The system SHALL let persona auto-select use the decision model whether or not model auto mode is enabled, SHALL send a single combined `/v1/systemone` request when both the persona and the task-route decisions are due in the same turn, and SHALL send only the persona question when auto mode is off.

#### Scenario: Auto mode disabled
- GIVEN model auto mode off and the persona-selector `useDecisionModel` on
- WHEN a prompt arrives
- THEN the persona is chosen by the decision model with only the persona question sent

#### Scenario: Combined request
- GIVEN both decisions are due
- WHEN the prompt is routed
- THEN exactly one `/v1/systemone` request carries both questions

### PANDO-SP-0006.R5 — Model auto mode keeps its behaviour on the shared engine

Model auto mode SHALL route through the shared engine with unchanged rules: a route wins only when its probability reaches the threshold; no match, router error or no routes runs the turn on the coder model; routing happens once per user turn; and a router failure never blocks the turn.

#### Scenario: Decision rules
- GIVEN routes and a scripted decision model
- WHEN prompts are routed
- THEN a probability below the threshold, the `none` choice and an unknown choice give no route

#### Scenario: One routing per user turn
- GIVEN a user prompt that triggers several tool steps
- WHEN the agent runs
- THEN the router is called once

#### Scenario: Router down
- GIVEN an unreachable router
- WHEN a prompt is sent
- THEN the turn runs on the coder model and a warning notice is shown
