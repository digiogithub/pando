---
id: PANDO-US-0081
type: story
title: "Provider failover: on provider failure switch the turn to fallback 1 then fallback 2, with bounded retries per candidate"
status: in_review
priority: high
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, agent, provider, reliability]
estimate: 5
created: 2026-09-30T19:34:53Z
updated: 2026-09-30T21:00:23Z
started: 2026-09-30T21:00:23Z
---

## Description

As a user, if the provider of the routed model fails, I want Pando to continue the same turn on the configured fallback models, so that one outage or quota exhaustion does not break my work.

**Where the failure is caught:** today a stream error surfaces at agent.go:1384-1391, after the provider has exhausted `maxRetries=10` with backoff from `baseDelayMs=500` (`provider/provider.go:18`). With 10 retries, failover would take minutes. In an Auto turn that still has fallbacks left, the provider is built with a **reduced retry budget**, for example 2 retries, configurable. The last candidate keeps the normal budget.

**Errors that trigger failover** are classified per provider, reusing each provider's `shouldRetry` logic:
- 429 or quota errors
- 5xx and overloaded responses
- network, DNS or TLS errors
- auth failures (401/403)
- model-not-found responses

**Errors that never trigger failover:**
- user cancellation or context cancelled
- context-length or 413 errors, which go to the auto-compact path
- content-policy refusals
- tool-execution errors, which are not provider errors

**On failover:**
1. Stop the partial stream and discard the partial assistant message, or mark it as failed.
2. Switch the model with `applyPendingModelSwitch` (`model_switch.go:158`), including history sanitisation and context refit.
3. Emit `⇄ Auto: <model A> failed (<reason>), retrying on <model B>`.
4. Re-issue the same iteration.

If failover happens in the middle of a tool loop, tool results that were already executed stay in history and are not executed again.

## Acceptance Criteria

- [ ] With candidates [A, B, C]: a failure on A makes the turn continue on B; failures on A and B make it continue on C; failures on all three surface C's error plus a summary of the chain.
- [ ] Failover is only active in Auto turns. A manually selected model behaves exactly as today.
- [ ] Every failover increments a counter in telemetry/logs with the provider, model and error class.
- [ ] A candidate that failed with auth or model-not-found errors is marked unhealthy for a short cooldown (for example 5 min, in memory). Later prompts in any session skip it while the cooldown lasts.
- [ ] Tests:
  - fake providers returning 429, 500, a network error, a cancellation and context-length exceeded; each case checks the expected failover or no-failover outcome
  - failover mid tool-loop does not re-execute tools

## Notes

The KB note `claude_code_improvements.md` already describes a missing "fallback model" concept (a `shouldRetry` that can signal a model change). This story delivers it, scoped to Auto turns first. Extending it to manual models is a possible follow-up.
