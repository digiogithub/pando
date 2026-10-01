---
id: PANDO-SP-0008
type: spec
title: "Relevance filter: observability and eligibility"
status: backlog
priority: medium
author: mcp
labels: [decision-model, observability, PANDO-EP-0018]
created: 2026-10-01T17:06:48Z
updated: 2026-10-01T17:11:12Z
requirements:
  R1:
    status: backlog
    trace:
      code:
        - internal/llm/agent/context_filter.go
        - internal/llm/agent/agent.go
        - internal/rag/relevance.go
      tests:
        - internal/llm/agent/context_filter_test.go#TestContextFilterEligibility
        - internal/llm/agent/context_filter_test.go#TestContextFilterIneligibleMemoryContext
        - internal/rag/relevance_test.go#TestApplyRelevanceFilterSkippedWhenContextDisablesIt
    verified: {rev: "sha256:b4a6eb18da710dca", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R2:
    status: backlog
    trace:
      code: [internal/llm/agent/context_filter.go]
      tests:
        - internal/llm/agent/context_filter_test.go#TestContextFilterNoticeOnlyWhenDropped
    verified: {rev: "sha256:630fb52943f150f9", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R3:
    status: backlog
    trace:
      code: [internal/llm/agent/context_filter.go]
      tests:
        - internal/llm/agent/context_filter_test.go#TestContextFilterFailOpenWarnsOncePerClass
    verified: {rev: "sha256:018849bbc8466d72", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R4:
    status: backlog
    trace:
      code:
        - internal/llm/agent/context_filter.go
        - internal/extevents/extevents.go
      tests:
        - internal/llm/agent/context_filter_test.go#TestContextFilterTelemetryAndEventPrivacy
        - internal/extevents/extevents_test.go#TestContextFilteredPayload
    verified: {rev: "sha256:ab6527729ba6a5a1", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R5:
    status: backlog
    trace:
      code:
        - internal/llm/agent/context_filter.go
        - internal/llm/agent/memory_block.go
      tests:
        - internal/llm/agent/context_filter_test.go#TestContextFilterMemoryResultMerged
    verified: {rev: "sha256:bd9619d756f247bc", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R6:
    status: backlog
    trace:
      code:
        - internal/api/handlers_chat.go
        - internal/agui/translate.go
        - internal/mesnada/acp/prompt_handler.go
        - internal/tui/components/core/status.go
      tests:
        - internal/api/handlers_chat_test.go#TestSSEForwardsContextFilterNotice
        - internal/agui/translate_test.go#TestContextFilterNoticeCustomEvent
        - internal/mesnada/acp/prompt_handler_test.go#TestContextFilterNoticeForwardedAsAgentMessage
        - internal/tui/components/core/status_context_filter_test.go#TestStatusShowsContextFilterNotice
    verified: {rev: "sha256:c9e2a33720fa6cde", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R7:
    status: backlog
    trace:
      code: [cmd/doctor.go]
      tests:
        - cmd/doctor_decision_model_test.go#TestDoctorDecisionModelBlock
        - cmd/doctor_decision_model_test.go#TestDoctorChecksReferToDecisionBlock
    verified: {rev: "sha256:975e0561f397c583", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
---

## Purpose

This spec defines which turns the relevance filter may run on, how its outcome is reported to users (status notice, fail-open warnings), to extensions and telemetry (without prompt or snippet text), and how `pando doctor` reports the decision model.

## Scope

In scope:
- `internal/llm/agent/context_filter.go`
- notice delivery over SSE, AG-UI, ACP and the TUI status bar
- `extevents.ContextFiltered` and log counter records
- the `pando doctor` "Decision model" block

Out of scope: the filter's decisions (spec "Relevance filter: semantics and fail-open") and the WebUI rendering (spec "Decision model: settings surfaces").

Epic: PANDO-EP-0018. Stories: PANDO-US-0099, PANDO-US-0096.

## Requirements

### PANDO-SP-0008.R1 — Eligibility: which turns may be filtered

The system SHALL run the relevance filter only on turns of the main coder agent that are user initiated, belong to a root session and are not clean-mode or system-initiated runs. Delegated subagents, child sessions and the agent-loop context enricher SHALL receive unfiltered context. An ineligible turn SHALL still be enriched; it only runs unfiltered.

#### Scenario: Subagent and child sessions
- GIVEN a delegated subagent turn or a session with a parent
- WHEN context is enriched
- THEN no candidate is filtered

#### Scenario: Clean mode and system runs
- GIVEN a clean-mode or system-initiated run
- WHEN context is enriched
- THEN the filter is skipped

#### Scenario: Context disables the filter
- GIVEN a context created with `WithoutRelevanceFilter`
- WHEN the enricher or memory block runs with a filter installed
- THEN the filter is not called and everything is kept

### PANDO-SP-0008.R2 — Notice only when something was dropped

The system SHALL emit a status notice `Context filter: kept K/N (T ms)`, followed by ` — ` and the per-source `code a/b, kb c/d, events e/f, memory g/h` for sources that had candidates, ONLY when the filter dropped at least one candidate. A run that dropped nothing SHALL produce no notice. The notice SHALL carry a structured `ContextFilter` payload (counts, per-source counts, threshold, latency, router provider and model, partial reason) and SHALL be recorded as a run status message.

#### Scenario: Something dropped
- GIVEN the filter kept 4 of 9 candidates
- WHEN the turn is reported
- THEN a system message starting `Context filter: kept 4/9` is published with the structured payload

#### Scenario: Nothing dropped
- GIVEN the filter kept every candidate
- WHEN the turn is reported
- THEN no notice is published

### PANDO-SP-0008.R3 — Fail-open warnings once per session and error class

When the filter did not (fully) run, the system SHALL show one warning notice per session and error class (for example `Context filter unavailable (<reason>): context injected unfiltered`, or `partially unavailable` for a partial result, or a "no decision model is configured" notice) and SHALL log later occurrences at debug level only. A hosted-provider skip (local-only) SHALL be debug-only and produce no notice. Fail-open SHALL never block or fail the turn.

#### Scenario: Repeated failure
- GIVEN the router is down for three consecutive turns of one session
- WHEN each turn is reported
- THEN exactly one warning notice is published for that error class

#### Scenario: Different class
- GIVEN a second error class later in the same session
- WHEN it is reported
- THEN a new warning is published for that class

#### Scenario: Hosted skip
- GIVEN a hosted provider with local-only on
- WHEN the filter is skipped
- THEN no user-visible notice is shown

### PANDO-SP-0008.R4 — Extension event and telemetry carry no prompt or snippet text

When the filter dropped candidates the system SHALL publish a `ContextFiltered` extension event and a counter log record containing only session id, kept and dropped counts (per source), threshold, latency, router provider/model and the partial reason. Neither the event, the counter records nor the debug log SHALL contain prompt text or candidate text.

#### Scenario: Payload contents
- GIVEN a filtered turn
- WHEN the event is published
- THEN it carries counts, threshold, latency and router identifiers

#### Scenario: Privacy
- GIVEN a prompt and snippets containing a marker string
- WHEN the turn is reported
- THEN the marker appears in no event, counter record or log line

### PANDO-SP-0008.R5 — Enrichment and memory results are merged into one report per turn

The system SHALL add up the filter results of the enrichment block and the memory block of one turn into a single notice (counts, per-source counts and latency summed), SHALL treat the turn as applied when any part was applied, and SHALL surface a partial reason of any part. The memory block is built once per session and frozen, so its result SHALL be reported only for the turn that built it.

#### Scenario: Two filters in one turn
- GIVEN the memory filter kept 1 of 3 memories and the enrichment result kept 2 of 2 KB candidates
- WHEN the turn is reported
- THEN a single notice `Context filter: kept 3/5 (10 ms) — kb 2/2, memory 1/3` is published

#### Scenario: Frozen memory block
- GIVEN the memory block was already built for the session
- WHEN a later turn is reported
- THEN the memory result is not reported again and nothing is published when nothing was dropped

### PANDO-SP-0008.R6 — Notice delivery: SSE, AG-UI, ACP and TUI

The context filter notice SHALL reach every client: the SSE chat stream as a system message carrying the structured `context_filter` payload, AG-UI as a custom event, ACP as a plain agent message at the start of the turn, and the TUI as a status-bar info message.

#### Scenario: SSE
- GIVEN an agent event with a context filter payload
- WHEN it is streamed
- THEN the SSE message carries the system text and the `context_filter` payload

#### Scenario: AG-UI
- GIVEN the same event
- WHEN it is translated
- THEN a custom event with the payload is produced

#### Scenario: ACP
- GIVEN the same event during an ACP prompt
- WHEN it is forwarded
- THEN the client receives an agent message with the notice text

#### Scenario: TUI
- GIVEN the same notice
- WHEN the status bar receives it
- THEN it is shown as an info message

### PANDO-SP-0008.R7 — pando doctor reports the decision model and its consumers

`pando doctor` SHALL print a "Decision model" block with provider, effective base URL, model, timeout and which consumers are on (model auto mode, persona selector, context filter and memory filter with threshold, candidate cap and local/hosted scope). It SHALL say the block is unused when no consumer is enabled; otherwise it SHALL report config validation errors and warnings, fail with a hint when no model is configured, warn when a local-only filter points at a hosted provider, and print the live health verdict with the exact fix. The model auto mode and persona checks SHALL refer to this block rather than to a router of their own.

#### Scenario: Block contents
- GIVEN a configured Ollama decision model and the context filter enabled
- WHEN doctor runs
- THEN the block lists the provider, URL, model, timeout and the filter's threshold and scope

#### Scenario: Unused
- GIVEN no consumer enabled
- WHEN doctor runs
- THEN it prints that no feature needs a decision model and reports no problem

#### Scenario: Missing model
- GIVEN a consumer enabled and an empty model
- WHEN doctor runs
- THEN it reports a failure with a hint to set the provider and model

#### Scenario: Other checks refer to the block
- GIVEN model auto mode or persona auto-select enabled
- WHEN doctor runs
- THEN their sections point at the Decision model block
