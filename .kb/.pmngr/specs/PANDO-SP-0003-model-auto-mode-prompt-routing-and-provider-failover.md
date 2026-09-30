---
id: PANDO-SP-0003
type: spec
title: "Model auto mode: prompt routing and provider failover"
status: in_review
priority: high
author: mcp
labels: [model-routing, agent, PANDO-EP-0015]
created: 2026-09-30T20:01:14Z
updated: 2026-09-30T21:06:56Z
started: 2026-09-30T21:06:56Z
requirements:
  R1:
    status: in_review
    trace:
      code: [internal/llm/modelrouter/engine.go]
      tests:
        - internal/llm/modelrouter/engine_test.go#TestEngineDecisionRules
        - internal/llm/modelrouter/engine_test.go#TestEngineCriteriaOrder
        - internal/llm/modelrouter/engine_test.go#TestEngineNoRoutesNoCall
        - internal/llm/modelrouter/engine_test.go#TestEngineKeepAliveOllamaOnly
        - internal/llm/modelrouter/engine_test.go#TestForConfigCaching
        - internal/llm/modelrouter/engine_live_test.go#TestEngineLiveOllama
  R2:
    status: in_review
    trace:
      code:
        - internal/llm/modelrouter/engine.go
        - internal/llm/agent/model_auto.go
      tests:
        - internal/llm/modelrouter/engine_test.go#TestEngineRouterErrors
        - internal/llm/agent/model_auto_test.go#TestAutoRouterDownUsesCoder
        - internal/llm/agent/model_auto_test.go#TestAutoWarnOncePerSession
  R3:
    status: in_review
    trace:
      code: [internal/llm/modelrouter/state.go]
      tests:
        - internal/llm/modelrouter/state_test.go#TestStateTruncationBudget
        - internal/llm/modelrouter/state_test.go#TestStateAttachmentsNames
        - internal/llm/modelrouter/state_test.go#TestStateHistoryPrompts
  R4:
    status: in_review
    trace:
      code:
        - internal/llm/agent/model_auto.go
        - internal/llm/agent/agent.go#processGeneration
        - internal/llm/agent/session_overrides.go
      tests:
        - internal/llm/agent/model_auto_test.go#TestAutoRoutesOncePerUserTurn
        - internal/llm/agent/model_auto_test.go#TestAutoConsecutiveTurnsSwitchModel
        - internal/llm/agent/model_auto_test.go#TestAutoConcurrentSessions
        - internal/llm/agent/model_auto_test.go#TestAutoSubagentsUnaffected
        - internal/llm/agent/model_auto_failover_test.go#TestSessionAutoModeFlag
  R5:
    status: in_review
    trace:
      code:
        - internal/llm/modelrouter/candidates.go
        - internal/llm/agent/model_auto.go
      tests:
        - internal/llm/modelrouter/candidates_test.go#TestCandidatesAttachments
        - internal/llm/modelrouter/candidates_test.go#TestCandidatesContextWindow
        - internal/llm/modelrouter/candidates_test.go#TestCandidatesUnusableRoute
        - internal/llm/modelrouter/candidates_test.go#TestDefaultLookup
  R6:
    status: in_review
    trace:
      code:
        - internal/llm/agent/model_auto_failover.go
        - internal/llm/agent/model_switch.go
        - internal/llm/provider/errors_classify.go
        - internal/llm/provider/provider.go
      tests:
        - internal/llm/agent/model_auto_failover_test.go#TestFailoverToNextCandidate
        - internal/llm/agent/model_auto_failover_test.go#TestFailoverAllFail
        - internal/llm/agent/model_auto_failover_test.go#TestNoFailoverOnCancel
        - internal/llm/agent/model_auto_failover_test.go#TestNoFailoverOnContextLength
        - internal/llm/agent/model_auto_failover_test.go#TestFailoverMidToolLoop
        - internal/llm/agent/model_auto_failover_test.go#TestFailoverCooldown
        - internal/llm/agent/model_auto_failover_test.go#TestNoFailoverOutsideAutoTurn
        - internal/llm/provider/errors_classify_test.go#TestClassifyProviderErrors
        - internal/llm/provider/errors_classify_test.go#TestErrorClassPolicy
        - internal/llm/provider/retry_budget_test.go#TestWithMaxRetriesOverridesTheDefaultBudget
---

## Purpose

This spec covers what happens on each user prompt in an Auto session:

1. A decision model classifies the prompt against the configured task routes.
2. The prompt runs on the matched route's model. If that provider fails, it moves to the route's fallbacks. If no route matches confidently, or the router is unavailable, it falls back to the default coder model.

## Scope

In scope:
- `internal/llm/modelrouter`: the decision engine
- the agent integration in `internal/llm/agent`: routing hook, session override, capability and context filtering, provider failover

Out of scope: the UI surfaces.

Epic: PANDO-EP-0015. Stories: PANDO-US-0079, PANDO-US-0080, PANDO-US-0081.

## Requirements

### PANDO-SP-0003.R1 — Routing question and decision rule

For each routing call, the engine SHALL send exactly one `choice` question named `task`. Its criteria SHALL be the enabled routes, in configured order, keyed by route `id` and described by `description`, plus the key `none` with the description "None of the listed tasks, or a general request".

The engine SHALL select a route only when both hold:
- `choice != "none"`
- `probabilities[choice] >= threshold`

When `minConfidence > 0`, it SHALL also require `confidence >= minConfidence`. In every other case the decision SHALL be `NoMatch`, with candidates `[agents.coder.model]`.

With zero enabled routes, the engine SHALL return `NoMatch` without making a network call.

#### Scenario: Match above threshold
- GIVEN routes `implementation` and `planning`, `threshold=0.6`, and a router answer `implementation p=0.94`
- WHEN a prompt is routed
- THEN `Reason=Matched`, `RouteID=implementation`
- AND the candidates start with the implementation route's model

#### Scenario: Below threshold
- GIVEN a router answer `quick_question p=0.55` with `threshold=0.6`
- WHEN a prompt is routed
- THEN `Reason=NoMatch`
- AND the candidates are `[coder model]`

#### Scenario: none wins
- GIVEN a router answer `none p=0.89` for the prompt "ok continue"
- WHEN it is routed
- THEN `Reason=NoMatch`
- AND the candidates are `[coder model]`

#### Scenario: Criteria order and none last
- GIVEN routes `[b, a]`, where `a` is disabled
- WHEN the request is built
- THEN the criteria keys are exactly `[b, none]`, in that order

#### Scenario: No routes, no call
- GIVEN auto mode is enabled with zero enabled routes
- WHEN a prompt is routed
- THEN no HTTP request is made
- AND `Reason=NoMatch`

### PANDO-SP-0003.R2 — Router failure falls back to the coder model without blocking

When the router call fails with any typed client error, including a timeout, the engine SHALL return `Reason=RouterUnavailable` with candidates `[agents.coder.model]` and the error attached. The prompt SHALL proceed on the coder model within `timeoutMs` plus a small overhead.

The user-visible warning SHALL be emitted at most once per session for each error class. Repeated failures SHALL be logged at Debug level only.

#### Scenario: Router down
- GIVEN the router base URL points to a closed port
- WHEN a prompt is sent in an Auto session
- THEN the turn runs on the coder model
- AND one warning notice "router unavailable" is emitted

#### Scenario: Second failure is quiet
- GIVEN the previous scenario has already warned
- WHEN a second prompt is sent in the same session
- THEN the turn runs on the coder model
- AND no new warning notice is emitted

#### Scenario: Timeout bound
- GIVEN a router that sleeps 5 s and `timeoutMs=500`
- WHEN a prompt is sent
- THEN the provider request for the coder model starts within 700 ms

#### Scenario: Unauthorized hosted router
- GIVEN `provider=typesafe` with an invalid key
- WHEN a prompt is sent
- THEN the turn runs on the coder model
- AND the warning names "unauthorized"

### PANDO-SP-0003.R3 — Token-aware state truncation within the router budget

The engine SHALL build `state` as `{request, previous_requests?}` so that the rendered request fits within the provider's context budget minus a safety margin. The request body SHALL also stay under 64 KiB.

- When the prompt is too long, the engine SHALL keep its head and tail and insert an ellipsis marker between them.
- Attachments and images SHALL NOT be sent. Only their file names SHALL be listed.
- `previous_requests` SHALL be included only when `historyPrompts > 0`, and each entry SHALL be truncated more aggressively than the current request.
- If the route descriptions alone exceed the budget, settings validation SHALL raise a warning. The route descriptions SHALL NOT be truncated silently.

#### Scenario: 80 KB prompt under a 2050-token budget
- GIVEN a context budget of 2050 tokens and an 80 KB prompt
- WHEN the request is built
- THEN the estimated tokens are at most the budget minus the margin
- AND the body is under 64 KiB
- AND the state contains the first and last lines of the prompt joined by the ellipsis marker

#### Scenario: Attachments not sent
- GIVEN a prompt with an image attachment `screenshot.png`
- WHEN the request is built
- THEN the state contains the text `screenshot.png`
- AND the state contains no base64 data

#### Scenario: History prompts
- GIVEN `historyPrompts=1` and a previous user prompt "refactor the session service"
- WHEN the follow-up "and add tests" is routed
- THEN `state.previous_requests` equals `["refactor the session service"]`

### PANDO-SP-0003.R4 — Route once per user prompt via a session-scoped override

In an Auto session, the agent SHALL call the router exactly once at the start of each user turn. The call SHALL happen after persona auto-select and before the provider is prepared.

The agent SHALL NOT call the router on:
- tool-iteration loops
- continuation or resurrection runs
- `/compact`
- summarization

The routed model SHALL be applied as a turn-scoped session model override. The global `agents.coder.model` SHALL NOT be modified.

Auto mode SHALL NOT affect subagents (mesnada delegation), the title, summarizer or persona-selector agents, or the evaluator judge.

Every assistant message SHALL record the model that actually answered in `messages.model`.

#### Scenario: One call per user turn
- GIVEN an Auto session and a turn that performs 3 tool iterations
- WHEN the turn completes
- THEN the fake router has received exactly 1 request

#### Scenario: Different models on consecutive turns
- GIVEN routes that send prompt 1 to model A and prompt 2 to model B
- WHEN both prompts run in the same session
- THEN the assistant messages have `model` A and then B
- AND the reasoning blocks from A are stripped before the request to B

#### Scenario: Concurrency isolation
- GIVEN two Auto sessions running concurrently and routed to different models
- WHEN both turns run
- THEN each turn uses its own routed model
- AND `agents.coder.model` is unchanged

#### Scenario: Subagents unaffected
- GIVEN an Auto session that delegates a task to a subagent
- WHEN the subagent runs
- THEN it uses its configured agent model
- AND the router is not called for it

### PANDO-SP-0003.R5 — Candidate chain filtering (unknown, disabled, attachments, context window)

The candidate chain of a matched route SHALL be built as `[model, fallbacks...]`, with duplicates removed. The following SHALL then be removed from the chain:

- models unknown to the registry
- models whose provider or account is disabled or unconfigured
- models without `SupportsAttachments`, when the prompt carries attachments
- models whose context window cannot hold the current history

If the chain ends up empty, the candidates SHALL be `[agents.coder.model]` with `Reason=RouteUnusable`. If the coder model's context is also exceeded, the existing auto-compact path SHALL apply.

#### Scenario: Attachment filtering
- GIVEN a route with primary A (no attachments support) and fallback B (supports attachments)
- WHEN a prompt with an image is routed to that route
- THEN the chain is `[B]`

#### Scenario: Context window skip
- GIVEN a history of 150K tokens and a route with primary A (128K context) and fallback B (200K context)
- WHEN the prompt is routed
- THEN the chain is `[B]`

#### Scenario: Unusable route
- GIVEN a route whose models all belong to a disabled provider
- WHEN the route matches
- THEN the candidates are `[coder model]`
- AND `Reason=RouteUnusable`

### PANDO-SP-0003.R6 — Provider failover to fallback 1 then fallback 2 within the same turn

In an Auto turn, when the provider of the current candidate fails with a failover-eligible error, the agent SHALL switch the turn to the next candidate. It SHALL use the existing model-switch path (history sanitisation and context refit) and re-issue the same iteration.

- **Failover-eligible errors:** 429 or quota, 5xx or overloaded, network, DNS or TLS failures, 401/403, and model-not-found.
- **Errors that SHALL NOT trigger failover:** user cancellation, context-length or 413 errors (these go to auto-compact), content-policy refusals, and tool errors.
- **Retry budget:** every candidate except the last SHALL use a reduced retry budget (default 2). The last candidate SHALL use the normal budget.
- **Tool results:** tool results already executed SHALL remain in history and SHALL NOT be executed again.
- **Cooldown:** a candidate that failed with an auth or model-not-found error SHALL be skipped by later turns for a cooldown period (default 5 min).
- **All candidates fail:** the error of the last candidate SHALL be shown together with a summary of the chain.
- **Manual selection:** when the user has picked a model manually (not Auto), behaviour SHALL be unchanged: no failover.

#### Scenario: A fails, B answers
- GIVEN candidates `[A, B, C]` and A returns 500 on every attempt
- WHEN the turn runs
- THEN the answer comes from B
- AND the notice "A failed (server error), retrying on B" is emitted
- AND A received at most 3 attempts (1 + 2 retries)

#### Scenario: All fail
- GIVEN A returns 429, B returns 401 and C returns 503
- WHEN the turn runs
- THEN the turn ends with C's error
- AND the error summary lists A, B and C with their error classes

#### Scenario: Cancellation does not fail over
- GIVEN the user cancels during A's stream
- WHEN the error surfaces
- THEN B is never called

#### Scenario: Context length does not fail over
- GIVEN A returns a context-length-exceeded error
- WHEN the error surfaces
- THEN the auto-compact path runs
- AND B is not called

#### Scenario: Mid tool-loop failover
- GIVEN A executes tool call T1 and then fails on the next iteration
- WHEN the turn fails over to B
- THEN T1 is not executed again
- AND B receives T1's result in its history

#### Scenario: Cooldown
- GIVEN A failed with 401 on turn 1
- WHEN turn 2 routes to the same route within 5 minutes
- THEN the chain starts at B
