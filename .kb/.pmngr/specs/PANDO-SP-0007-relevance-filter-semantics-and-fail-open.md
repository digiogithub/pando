---
id: PANDO-SP-0007
type: spec
title: "Relevance filter: semantics and fail-open"
status: backlog
priority: high
author: mcp
labels: [decision-model, remembrances, context-enrichment, PANDO-EP-0018]
created: 2026-10-01T17:06:48Z
updated: 2026-10-01T17:12:19Z
requirements:
  R1:
    status: backlog
    trace:
      code:
        - internal/config/decision_filter.go
        - internal/config/config.go
        - internal/api/handlers_config.go
      tests:
        - internal/config/decision_filter_test.go#TestDecisionFilterDefaults
        - internal/config/decision_filter_test.go#TestValidateDecisionFilter
        - internal/config/decision_filter_test.go#TestUpdateRemembrancesRejectsInvalidDecisionFilter
        - internal/config/decision_filter_test.go#TestDecisionFilterPersistenceAndHotReloadEvent
        - internal/api/handlers_decision_model_test.go#TestConfigServicesExposesAndValidatesDecisionFilter
    verified: {rev: "sha256:9a8a42d42af5631f", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R2:
    status: backlog
    trace:
      code: [internal/llm/modelrouter/relevance.go]
      tests:
        - internal/llm/modelrouter/relevance_test.go#TestRelevanceDropsBelowThreshold
    verified: {rev: "sha256:0562962a77315178", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R3:
    status: backlog
    trace:
      code:
        - internal/llm/modelrouter/relevance.go
        - internal/llm/modelrouter/engine.go
      tests:
        - internal/llm/modelrouter/relevance_test.go#TestRelevanceFailOpen
        - internal/llm/modelrouter/relevance_test.go#TestRelevanceFailOpenOnOllamaOlderThan035
    verified: {rev: "sha256:73071549abf8ddbf", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R4:
    status: backlog
    trace:
      code: [internal/llm/modelrouter/relevance.go]
      tests:
        - internal/llm/modelrouter/relevance_test.go#TestRelevanceBatchesSeventyCandidatesIntoTwoRequests
        - internal/llm/modelrouter/relevance_test.go#TestRelevanceBeyondCapIsKeptUnseen
        - internal/llm/modelrouter/relevance_test.go#TestRelevanceCandidateThatCannotFitIsKept
        - internal/llm/modelrouter/relevance_test.go#TestRelevancePartialBatchFailureKeepsFailedBatch
    verified: {rev: "sha256:82f902a0634eb010", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R5:
    status: backlog
    trace:
      code:
        - internal/llm/modelrouter/relevance.go
        - internal/config/decision_filter.go
      tests:
        - internal/llm/modelrouter/relevance_test.go#TestRelevanceLocalOnlySkipsHostedProviders
    verified: {rev: "sha256:f01787297489ece3", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R6:
    status: backlog
    trace:
      code:
        - internal/llm/modelrouter/relevance.go
        - internal/rag/relevance.go
        - internal/rag/memory_enricher.go
      tests:
        - internal/llm/modelrouter/relevance_test.go#TestRelevancePinnedCandidatesAreNotAskedAndKept
        - internal/rag/relevance_test.go#TestFilterMemoriesPinnedNeverDropped
        - internal/rag/relevance_store_test.go#TestBuildMemoryBlockWithResultAgainstRealStore
    verified: {rev: "sha256:70c792d24fa71396", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R7:
    status: backlog
    trace:
      code: [internal/rag/enricher.go, internal/rag/relevance.go]
      tests:
        - internal/rag/relevance_test.go#TestAssembleFilterOffIsGolden
        - internal/rag/relevance_test.go#TestAssembleKeepAllFilterMatchesGolden
        - internal/rag/relevance_test.go#TestAssembleFilterDropsCandidatesAndSections
        - internal/rag/relevance_test.go#TestAssembleBudgetsApplyToFilteredSet
        - internal/rag/relevance_test.go#TestAssembleMalformedMaskKeepsEverything
        - internal/rag/relevance_test.go#TestSetRelevanceFilterNilTurnsItOffAndIsRaceFree
        - internal/rag/relevance_store_test.go#TestEnrichContextWithResultAgainstRealStores
    verified: {rev: "sha256:53337bd1a0fe6772", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R8:
    status: backlog
    trace:
      code: [internal/rag/memory_enricher.go]
      tests:
        - internal/rag/relevance_store_test.go#TestBuildMemoryBlockWithResultAgainstRealStore
        - internal/rag/relevance_test.go#TestFilterMemoriesPinnedNeverDropped
    verified: {rev: "sha256:5b6dd373842db4bc", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R9:
    status: backlog
    trace:
      code: [internal/app/relevance_filter.go, internal/app/app.go]
      tests:
        - internal/app/relevance_filter_test.go#TestRelevanceTargetsApplySetsAndUnsetsPerFlag
    verified: {rev: "sha256:db7014cef682df5a", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R10:
    status: backlog
    trace:
      code:
        - tests/decision_model/relevance_bench.py
        - tests/decision_model/dataset.jsonl
        - tests/decision_model/REPORT.md
        - internal/llm/modelrouter/relevance.go
      tests:
        - internal/llm/modelrouter/relevance_live_test.go#TestRelevanceLiveOllama
    verified: {rev: "sha256:1047fbc6591767be", commit: 0e2650e5988354fa936cff38a4ec95b91bad7e4b, at: 2026-10-01T17:12:14Z, by: mcp}
---

## Purpose

This spec defines the opt-in relevance filter that asks the decision model, after retrieval, whether each candidate (code symbol, KB chunk, event, memory) is useful for the user's prompt, and drops the ones that are not before formatting. It must never make a turn worse than running without it: every failure keeps the unfiltered result.

## Scope

In scope:
- `[Remembrances]` filter options (defaults, validation)
- `internal/llm/modelrouter/relevance.go` (`NewRelevanceFilter`)
- `internal/rag` filter hooks (`enricher.go`, `memory_enricher.go`, `relevance.go`)
- wiring in `internal/app/relevance_filter.go`
- live validation and the precision/recall benchmark

Out of scope: notices, events, telemetry, eligibility and doctor (spec "Relevance filter: observability and eligibility").

Epic: PANDO-EP-0018. Stories: PANDO-US-0098, PANDO-US-0096.

## Requirements

### PANDO-SP-0007.R1 — Filter options: defaults, validation, persistence and REST

The system SHALL expose these `[Remembrances]` options, all defaulting to the safe value: `ContextEnrichmentDecisionFilterEnabled=false`, `MemoryContextDecisionFilterEnabled=false`, `ContextEnrichmentDecisionFilterThreshold=0.60`, `ContextEnrichmentDecisionFilterMaxCandidates=32`, `ContextEnrichmentDecisionFilterMaxCandidateChars=400` and `ContextEnrichmentDecisionFilterAllowHosted=false` (local providers only; stored inverted so that a missing key stays safe).

- A zero value SHALL mean "use the default"; a threshold outside (0,1] or a negative count SHALL be rejected.
- Valid changes SHALL persist, publish a config-reload event and be returned by the services config endpoint, which SHALL answer an invalid value with a 400.

#### Scenario: Defaults
- GIVEN an untouched config
- WHEN the filter options are read
- THEN both filters are off, the threshold is 0.60, the cap is 32, the text cap is 400 and hosted providers are not allowed

#### Scenario: Out-of-range threshold
- GIVEN `ContextEnrichmentDecisionFilterThreshold=1.5`
- WHEN the remembrances config is updated
- THEN the update is rejected and the config is unchanged

#### Scenario: Persistence and REST
- GIVEN a valid update enabling the context filter
- WHEN the config is reloaded and the services config endpoint is read
- THEN the options are present
- AND an invalid PUT returns 400

### PANDO-SP-0007.R2 — One question per candidate; threshold on p(useful)

The filter SHALL send the user prompt as state and one `choice` question per candidate with criteria `useful` and `not_useful`, whose instructions carry the candidate source and text (capped by the configured character limit). A candidate SHALL be kept when `probabilities[useful] >= threshold` (exactly the threshold is kept) and dropped otherwise. The result SHALL report applied, kept and dropped counts per source, the probability per candidate and the latency.

#### Scenario: Threshold boundary
- GIVEN threshold 0.60 and probabilities 0.9, 0.2 and 0.6 for three candidates
- WHEN the filter runs
- THEN the candidate at 0.2 is dropped and the others are kept
- AND one request is sent whose body contains the prompt and the candidate text

### PANDO-SP-0007.R3 — Fail-open: any decision-model problem keeps every candidate

The filter SHALL be fail-open. On a 500, timeout, 401, malformed body, missing answers, an answer whose choice is not `useful`/`not_useful`, an empty router model, an Ollama older than 0.35 that does not serve `/v1/systemone`, or an engine build failure, it SHALL keep every candidate, report `Applied=false` with the error class in `Reason` (or `no_router`), drop nothing and report no probabilities.

#### Scenario: Provider and answer failures
- GIVEN a provider answering 500, timing out, answering 401, returning malformed JSON, missing answers or an unknown choice
- WHEN the filter runs over three candidates
- THEN all three are kept, `Applied=false` and `Reason` is the matching class

#### Scenario: No router model
- GIVEN an empty `router.model`
- WHEN the filter runs
- THEN everything is kept with `Reason=no_router` and no request is sent

#### Scenario: Ollama older than 0.35
- GIVEN an Ollama 0.32 that answers `/api/version` but returns 404 for `/v1/systemone`
- WHEN the filter runs
- THEN everything is kept, `Applied=false`, `Reason` is non-empty and there are no probabilities

### PANDO-SP-0007.R4 — Candidate cap, batching and "unasked is kept"

The filter SHALL ask only the first `MaxCandidates` non-pinned candidates in the fixed order code, kb, events, memory. Candidates beyond the cap, and candidates that cannot fit any request (token budget or 64 KiB body), SHALL be KEPT and never dropped unseen. At most 64 questions SHALL travel in one request and more SHALL be split into several requests whose bodies never exceed the cap. When only some requests fail, the failed batch SHALL be kept, the others SHALL still apply, and the result SHALL be `Applied=true` with `Reason=partial:<class>`.

#### Scenario: 70 candidates
- GIVEN 70 candidates and a cap of 100
- WHEN the filter runs
- THEN two requests are sent, none above the body cap, and a candidate in the second request can be dropped

#### Scenario: Beyond the cap
- GIVEN a cap of 3, three code and two memory candidates, and a router that rejects everything
- WHEN the filter runs
- THEN the three code candidates are dropped and the two memories are kept

#### Scenario: Oversized candidate
- GIVEN a candidate far larger than the body cap and a small one
- WHEN the filter runs
- THEN the huge candidate is kept unseen and the small one is judged

#### Scenario: Partial failure
- GIVEN the second of two requests returns an unusable answer
- WHEN the filter runs
- THEN the first 64 candidates are judged, the other 6 are kept and `Reason=partial:malformed`

### PANDO-SP-0007.R5 — Local-only by default: hosted providers never see snippets unless allowed

The filter SHALL NOT contact a hosted decision provider (`typesafe` or `custom`) while `ContextEnrichmentDecisionFilterAllowHosted` is false: it SHALL keep every candidate with `Reason=hosted_provider` and send no request. With the flag on, the hosted provider SHALL be used.

#### Scenario: Hosted skipped
- GIVEN a `custom` provider and local-only on
- WHEN the filter runs
- THEN everything is kept with `Reason=hosted_provider` and the provider received zero requests

#### Scenario: Hosted allowed
- GIVEN the same provider with hosted allowed
- WHEN the filter runs
- THEN the provider is called and candidates below the threshold are dropped

### PANDO-SP-0007.R6 — Pinned memories are never asked and never dropped

Candidates marked pinned (memories from `MemoryPinnedScopes`) SHALL NOT be sent to the decision model and SHALL always be kept, even when the filter answers that they are not useful.

#### Scenario: Pinned not asked
- GIVEN two memory candidates, one pinned
- WHEN the filter runs against a router that rejects everything
- THEN only one question is sent, the pinned candidate is kept and the other is dropped

#### Scenario: Pinned survives a hostile filter
- GIVEN a filter that asks to drop a pinned memory
- WHEN memories are filtered
- THEN the pinned memory is still injected

### PANDO-SP-0007.R7 — Enricher integration: filter before formatting, budgets on the filtered set

When a filter is installed, `ContextEnricher` SHALL pass the code, KB and event candidates (in that order) to it once, before formatting, and SHALL format and apply the per-source and total character budgets to the kept candidates only; a source whose candidates are all dropped SHALL disappear from the output. With no filter (nil), after `SetRelevanceFilter(nil)` or under `WithoutRelevanceFilter` the output SHALL be byte-identical to the unfiltered golden output. A keep mask of the wrong length SHALL keep everything. `EnrichContextWithResult` SHALL return the filter result (zero when no filter ran). Installing and removing the filter while turns run SHALL be race-free.

#### Scenario: Golden output
- GIVEN no filter, or a filter that keeps everything
- WHEN the context is assembled
- THEN the text equals the golden unfiltered output

#### Scenario: Drops and sections
- GIVEN a filter that drops one code symbol, one KB document and the only event
- WHEN the context is assembled
- THEN the dropped items and the empty events section are absent and the counts per source are reported

#### Scenario: Budgets after filtering
- GIVEN a KB character budget that cut the section without a filter
- WHEN the filter drops the first KB entry
- THEN the second entry now fits the budget

#### Scenario: Real stores end to end
- GIVEN real migrated SQLite KB and events stores
- WHEN `EnrichContextWithResult` runs with a filter that drops one KB document
- THEN the output keeps the other KB document and the event, drops the chosen one, and reports `Dropped=1` with per-source counts
- AND a context built with `WithoutRelevanceFilter` returns everything unfiltered

#### Scenario: Malformed mask
- GIVEN a filter returning a mask of the wrong length
- WHEN the context is assembled
- THEN everything is kept

### PANDO-SP-0007.R8 — Memory block: filter before injection, hit counters only for injected memories

`BuildMemoryBlockWithResult` SHALL pass the memories selected for injection to the filter (nil filter: output byte-identical to `BuildMemoryBlock`), SHALL drop the memories the filter rejects (never pinned-scope ones), SHALL increment hit counters only for memories that are actually injected, and SHALL return the filter result.

#### Scenario: Filter off
- GIVEN no filter
- WHEN the block is built
- THEN it equals the `BuildMemoryBlock` output and the result is not applied

#### Scenario: Real store with a filter
- GIVEN three memories in a real migrated SQLite store, one in a pinned scope, and a filter asking to drop the unrelated one and the pinned one
- WHEN the block is built
- THEN the unrelated memory is absent, the other two (including the pinned one) are present, and the result reports one dropped and two kept

#### Scenario: Hit counters
- GIVEN the previous build
- WHEN counters are read
- THEN injected memories (including the pinned one) were incremented and the dropped one was not

### PANDO-SP-0007.R9 — Wiring: one shared filter installed per flag and kept in sync on reload

The app SHALL install one shared relevance filter on the context enricher only while `ContextEnrichmentDecisionFilterEnabled` is on and on the memory injector only while `MemoryContextDecisionFilterEnabled` is on, and SHALL remove it when the flag goes off. The filter SHALL read the decision model and thresholds from the live config on every call, and the wiring SHALL re-apply on `remembrances` and `decisionModel` config-change events, so changes take effect on the next prompt without a restart.

#### Scenario: Flags control installation
- GIVEN the memory flag on and the enrichment flag off
- WHEN the targets are applied
- THEN only the memory injector has the filter
- AND turning the memory flag off removes it

### PANDO-SP-0007.R10 — Live validation against Ollama tev1:0.8b and the precision/recall benchmark

Against a real Ollama 0.35 serving `tev1:0.8b`, the relevance filter SHALL apply (not fail open), finish within the decision timeout plus its grace, keep an obviously useful candidate and drop an obviously unrelated one. The repository SHALL contain a reproducible benchmark (`tests/decision_model/relevance_bench.py` with its labelled `dataset.jsonl`) that sends requests shaped exactly like the Go filter and reports precision, recall and F1 at thresholds 0.5, 0.6 and 0.7, p50/p95 latency and token usage, and `tests/decision_model/REPORT.md` SHALL record the measured numbers and the ship-gate verdict (precision >= 0.85 and recall >= 0.80 at 0.60 required to ship the filter on by default; otherwise it stays opt-in).

The live Go test is skipped unless `PANDO_LIVE_OLLAMA=1`.

#### Scenario: Live test
- GIVEN `PANDO_LIVE_OLLAMA=1` and a local Ollama 0.35 with `tev1:0.8b`
- WHEN `TestRelevanceLiveOllama` runs
- THEN the filter is applied with no failure reason, within the time bound
- AND the useful code candidate is kept and the recipe candidate is dropped

#### Scenario: Benchmark report
- GIVEN the benchmark was run
- WHEN `REPORT.md` is read
- THEN it holds the model and Ollama versions, hardware, the metric tables and whether the ship gate was met
