---
id: PANDO-US-0098
type: story
title: "Relevance filter core: `rag.RelevanceFilter` driven by the decision model drops non-useful code/KB/event candidates and memories; fail-open, token-aware batching, Remembrances config"
status: in_review
priority: high
parent: PANDO-EP-0018
author: mcp
labels: [decision-model, remembrances, context-enrichment, memory, rag]
estimate: 8
created: 2026-10-01T16:19:43Z
updated: 2026-10-01T16:44:24Z
started: 2026-10-01T16:44:24Z
---

## Description

As a user with context enrichment on, I want the retrieved snippets and memories checked against my actual request by the decision model, so that the prompt only carries context that helps the task instead of everything that happens to be semantically close.

**Interface in `internal/rag`** (no import of `modelrouter`):

```go
type RelevanceCandidate struct { Source string /* code|kb|events|memory */; ID string; Text string; Score float64 }
type RelevanceFilter interface {
    Filter(ctx context.Context, prompt string, cands []RelevanceCandidate) (keep []bool, res FilterResult)
}
type FilterResult struct { Applied bool; Kept, Dropped int; Latency time.Duration; Reason string; Probabilities map[string]float64 }
```

`ContextEnricher` gets `SetRelevanceFilter(f)`; `EnrichContext` builds candidates from the three searches **after** `minScore`, calls the filter once, then formats only the kept ones so `*MaxChars`/`TotalMaxChars` apply to the filtered set. `BuildMemoryBlock` gains the same hook (memories from `GetMemoriesForInjection`; pinned scopes are never dropped). Candidate text is the same excerpt that would be injected (symbol name + location + docstring; KB path + chunk; event subject + content; memory line), capped at `maxCandidateChars` (default 400).

**Implementation in `internal/llm/modelrouter/relevance.go`** (wired from `internal/app`): state = prompt (+ `historyPrompts` from `modelAutoMode`, reused); one `choice` question per candidate, criteria `useful` / `not_useful` with instructions explaining "useful = needed to carry out the request"; `probabilities[useful] ≥ threshold` keeps. Batches of ≤ 64 questions, body ≤ 64 KiB, truncation to the model context budget with deterministic order code → KB → events → memory; candidates that do not fit are **kept** (never dropped unseen). Any error, timeout, malformed answer, or empty router model → `Applied=false`, everything kept.

**Config (`RemembrancesConfig`)**: `ContextEnrichmentDecisionFilterEnabled` (false), `ContextEnrichmentDecisionFilterThreshold` (0.60), `ContextEnrichmentDecisionFilterMaxCandidates` (32), `ContextEnrichmentDecisionFilterMaxCandidateChars` (400), `MemoryContextDecisionFilterEnabled` (false), `ContextEnrichmentDecisionFilterLocalOnly` (true: skip the filter when the provider is hosted unless the user opts in). Exposed through the existing remembrances settings REST/update functions and the JSON schema.

## Acceptance Criteria

- [ ] Filter off → `EnrichContext` and `BuildMemoryBlock` byte-identical to today (golden test).
- [ ] Filter on with `systemonetest`: candidates with `p(useful) < threshold` are absent from the output; kept ones keep today's formatting and order; budgets are computed on the filtered set.
- [ ] Fail-open tests: 500, timeout, 401, malformed JSON, empty router model, Ollama < 0.35, `MaxBodyBytes` exceeded → output equals the unfiltered output and `FilterResult.Applied == false` with the error class in `Reason`.
- [ ] Batching test: 70 candidates → two requests; `maxCandidates` cap keeps the top-N by embedding score and leaves the rest unfiltered-kept or dropped per a documented rule (default: candidates beyond the cap are dropped only if the block would exceed `TotalMaxChars`).
- [ ] Pinned memory scopes are never dropped (test).
- [ ] Agent-loop enricher (`agentLoopEnricher`) output is not filtered; its classic fallback path is.
- [ ] `LocalOnly=true` with provider `typesafe`/`custom` → filter skipped with `Reason="hosted_provider"`.
- [ ] Config defaults, validation (threshold in (0,1], caps ≥ 1), `UpdateRemembrances*` persistence and hot reload (`SetRelevanceFilter(nil)` when disabled) covered by tests in `internal/config` and `internal/rag`.
- [ ] `go test ./internal/rag ./internal/llm/modelrouter ./internal/config` green.

## Notes

- Anchors: `internal/rag/enricher.go:120-214` (`EnrichContext`), `:216-357` (formatters), `memory_enricher.go:19-80`, `internal/app/app.go:371-460`, `internal/app/memory_injector_adapter.go`, `config.go:514-581`.
- Why `choice` and not `score`: thresholds on `probabilities[choice]` are what auto mode and personas already use; the live benchmark (last story) may switch to `score` if better calibrated.
- Hits increment (`IncrementMemoryHits`) only for memories actually injected.
- Spec: "Relevance filter: semantics and fail-open".
