## How context enrichment works today

The current flow is very straightforward:

- In `internal/app/app.go`, if `cfg.Remembrances.ContextEnrichmentEnabled` is active:
  - `rag.NewContextEnricher(...)` is created
  - It is injected into the agent with `agent.SetContextEnricher(enricher)`

- In `internal/llm/agent/agent.go`, before sending the prompt to the LLM:
  - if `globalContextEnricher != nil`
  - it calls `globalContextEnricher.EnrichContext(ctx, content)`
  - and concatenates the result to the original prompt

- In `internal/rag/enricher.go`, `EnrichContext`:
  - launches in parallel:
    - `searchKB(ctx, query)`
    - `searchEvents(ctx, query)`
    - `searchCode(ctx, query)`
  - uses **the original prompt as-is** as the query in all three APIs
  - filters only by `minScore`
  - formats the complete results into a `<context source="remembrances">` block

## Issues with the current design

I see several reasons why results drift from the actual prompt and waste tokens:

### 1. The same raw query is reused for everything
Right now the system passes the **untransformed user prompt** to:

- KB search
- events search
- code hybrid search

This works poorly when the prompt contains:
- operational instructions: "do", "analyze", "change"
- irrelevant conversational context
- multiple mixed objectives
- long noisy text

Typical example:
- prompt: _"review how pando's context enrichment with remembrances works when enabled..."_
- query sent to Remembrances: exactly that

For semantic search, that query is not optimal. It mixes:
- action intent
- domain
- condition
- secondary request

This degrades recall/precision.

---

### 2. No different strategy per source
All three sources use the same query shape, but each needs something different:

- **KB**: better with a summarized conceptual query
- **Code**: better with technical terms, symbols, subsystems, file/feature names
- **Events**: better with area/project/topic and possibly a time window or specific subject

Today there is no specialization.

---

### 3. The filter only uses `minScore`
Even though `minScore` exists, it's not enough because:

- if the embedding/search backend returns "somewhat similar" results, they get included
- there is no cross-reranking
- there is no semantic deduplication
- there is no per-section budget beyond the result count
- there is no post-selection compression

Result: "valid" context from the engine's perspective enters, but is of little use for resolving the prompt.

---

### 4. Too much raw text is injected
`searchKB`, `searchEvents`, `searchCode` do simple truncation, but they still include:
- literal chunks
- long events
- source snippets of up to 400 chars per symbol

This can consume many tokens even if the result is marginal.

---

### 5. No query rewriting or contextual selection
An intermediate phase is missing, like:
- "What is the user really looking for?"
- "What concepts/symbols work best for each backend?"
- "What results deserve to enter the final prompt?"

That is exactly the gap you notice.

---

## What I would improve in direct Remembrances queries

I would split it into two levels: **improvements without a model** and **improvements with a dedicated subagent/model**.

---

# 1) Improvements without an additional model

These are cheap, safe, and useful even without adding a subagent.

## A. Minimal heuristic query rewriting
Before querying Remembrances, derive from the original prompt:

- `rawQuery`: original prompt
- `semanticQuery`: prompt cleaned up for semantic search
- `codeQuery`: query focused on code
- `eventsQuery`: query focused on history/project

Transformation example:
- remove generic execution verbs: "do", "analyze", "review"
- remove meta phrases: "I want you to", "please"
- preserve technical names:
  - `context enrichment`
  - `remembrances`
  - `pando`
  - `subagent`
  - `summary`
  - `title`
  - `fallback model`

Even a simple cleanup would improve things considerably.

---

## B. Per-source strategy

### KB
Use `semanticQuery`, shorter and more conceptual.

### Code
Use `codeQuery`, prioritizing:
- feature names
- exact config names
- symbols or technical concepts

In this case, something like:
- `"context enrichment remembrances ContextEnricher summary title fallback model"`

would be better than the full prompt.

### Events
Use a shorter, contextual query:
- `"context enrichment remembrances"`
or even
- `"context enricher"`

And if the subject is empty, consider a more restrictive default for this case (`project`), because general events tend to add noise.

---

## C. Per-section budget and total budget
In addition to `kbResults/codeResults/eventsResults`, add output limits:
- `ContextEnrichmentKBMaxChars`
- `ContextEnrichmentCodeMaxChars`
- `ContextEnrichmentEventsMaxChars`
- `ContextEnrichmentMaxChars`

Because the problem is not just how many results enter, but **how much final text they consume**.

---

## D. Deduplication and post-selection
After collecting results:
- deduplicate repeated paths/symbols
- if a code result already covers something very specific, lower the weight of redundant KB/events
- prioritize:
  1. code
  2. KB
  3. events
  for prompts clearly about engineering/code

Today the fixed order is not the main problem, but the lack of prioritization by task type is.

---

## E. Better context compression
Instead of inserting near-raw chunks:
- KB: title/path + 1-2 key sentences
- Events: 1 line on why that event is relevant
- Code: symbol + file + brief docstring, and only source if it adds value

In other words: move from "retrieval dump" to "retrieval summary".

---

## F. Adjust defaults
Current defaults:
- KB 3
- Code 5
- Events 3
- minScore 0.45

To avoid noise, I would try:
- `KBResults = 2`
- `CodeResults = 3`
- `EventsResults = 2`
- `MinScore = 0.55` or `0.60`

Especially while there is no reranking.

---

# 2) Improvements with a dedicated model/subagent

This part fits very well with your idea.

## Architectural viability

Yes, it is viable and the project **already has the pattern**.

In `internal/llm/agent/agent.go`:

- for titles:
  - `titleProvider`
  - created with `createAgentProvider(..., config.AgentTitle, ...)`

- for summaries:
  - `summarizeProvider`
  - `summarizeFallbackProvider`
  - and in `sendSummaryRequest(...)`:
    - first uses `summarizeProvider`
    - if it fails, falls back to `summarizeFallbackProvider` (the coder)

This is exactly the pattern you propose:
- specialized model
- fallback to coder on failure

## What I would propose

Add a new agent/model, for example:

- `AgentContextEnricher`
or
- `AgentRetriever`

with its own configuration in `config`.

### Suggested config
In `internal/config/config.go` and defaults:

- `AgentContextEnricher AgentName = "context-enricher"`

And in config:
- `agents.context-enricher.model`
- `agents.context-enricher.max_tokens`

Plus a Remembrances section:
- `ContextEnrichmentUseAgent bool`
- `ContextEnrichmentAgentFallbackToCoder bool`

Optionally:
- `ContextEnrichmentAgentEnabled bool`

---

## What that subagent/model would do

It should not do all the answering work. Just this:

### Phase 1: query planning
Given the original prompt, produce something structured like:

```json
{
  "intent": "analyze current context enrichment and propose improvements",
  "kb_query": "context enrichment remembrances pando",
  "code_query": "ContextEnricher remembrances context enrichment summary title fallback model",
  "events_query": "context enrichment remembrances",
  "preferred_sources": ["code", "kb", "events"],
  "keywords": ["ContextEnricher", "SetContextEnricher", "AgentSummarizer", "AgentTitle", "fallback"],
  "max_results": {
    "kb": 2,
    "code": 3,
    "events": 2
  }
}
```

### Phase 2: retrieval
The system executes the actual searches against Remembrances with those queries.

### Phase 3: contextual compression
The same subagent, or a simple local function, summarizes what was found into a short block:
- what is relevant
- why
- what to ignore

### Phase 4: inject
A compact context is injected, not a raw dump.

---

## Two possible designs

## Option A — subagent only for query rewriting
Simpler and more robust.

Flow:
1. original prompt
2. context-enrichment model generates `kb_query`, `code_query`, `events_query`
3. Go executes searches
4. Go formats context

### Advantages
- easier implementation
- lower risk
- the model doesn't need tool access
- simple fallback
- you control retrieval

### Disadvantage
- final compression is still relatively dumb if you don't improve it

**This is the option I would implement first.**

---

## Option B — subagent also summarizes retrieval
Flow:
1. original prompt
2. model generates queries
3. Go does retrieval
4. model receives raw results
5. returns "recommended final context"

### Advantages
- much better noise control
- better relevance/token ratio

### Disadvantages
- more latency
- more cost
- more complexity
- more failure points

I would leave this as phase 2.

---

# How it fits with the summary/title pattern

Perfectly.

## Reusable existing pattern
Today this already exists for summaries:

- `summarizeProvider`
- `summarizeFallbackProvider`
- `sendSummaryRequest(...)`
- if the dedicated model fails:
  - retry with the coder

This can be replicated for enrichment:

- `contextEnrichmentProvider`
- `contextEnrichmentFallbackProvider`

and a function like:

- `planContextEnrichmentQuery(...)`
or
- `buildEnrichmentPlan(...)`

with logic:

1. use `contextEnrichmentProvider`
2. if it fails:
   - log warning
   - use fallback coder
3. if that also fails:
   - degrade to the current heuristic/direct mode

This gives you **triple resilience**:
- dedicated model
- fallback coder
- heuristic fallback without a model

Very good design.

---

# Concrete recommended design

## New component
Instead of putting everything in `rag.ContextEnricher`, I would separate:

- `ContextEnricher` = orchestrator
- `QueryPlanner` = generates queries
- `ContextFormatter` = compresses/synthesizes output

### Suggested interfaces
Something like:

```go
type EnrichmentQueryPlanner interface {
    Plan(ctx context.Context, prompt string) (*EnrichmentPlan, error)
}
```

```go
type EnrichmentPlan struct {
    Intent           string
    KBQuery          string
    CodeQuery        string
    EventsQuery      string
    PreferredSources []string
    KBResults        int
    CodeResults      int
    EventsResults    int
    Keywords         []string
}
```

Then `ContextEnricher.EnrichContext(...)` would:

1. `plan := planner.Plan(...)`
2. if it fails → fallback planner
3. execute searches
4. filter
5. summarize
6. return `<context>` block

---

## Planner implementations

### 1. Heuristic planner
No LLM, always available.

### 2. LLM planner
Uses dedicated provider:
- `config.AgentContextEnricher`
- fallback to coder

---

# What would need to change in code

## 1. Config
### `internal/config/config.go`
Add:
- `AgentContextEnricher`
- model defaults per provider
- appropriate budgets

And in `RemembrancesConfig`, new flags like:
- `ContextEnrichmentUseQueryPlanner`
- `ContextEnrichmentPlannerAgentEnabled`
- `ContextEnrichmentPlannerFallbackToCoder`

Optional:
- `ContextEnrichmentMaxChars`
- `ContextEnrichmentUseCompression`

---

## 2. Agent
### `internal/llm/agent/agent.go`
If you want to follow the summary/title pattern within the main agent:
- add `contextEnrichmentPlannerProvider`
- add `contextEnrichmentPlannerFallbackProvider`

But honestly, for this feature **it's better not to couple it to the main agent**, because enrichment happens before the final prompt and belongs more to `rag` or a context preparation layer.

I would move the dedicated provider logic to a component in the `rag` or `llm/context` package.

---

## 3. RAG
### `internal/rag/enricher.go`
Refactor to:
- use derived plan
- not use the raw prompt directly for everything
- apply budgets and compression

---

## 4. Prompt/template
Optionally, you could mark the context with a more explicit structure:

```xml
<context source="remembrances">
  <intent>...</intent>
  <retrieval_summary>...</retrieval_summary>
  <code>...</code>
  <kb>...</kb>
  <events>...</events>
</context>
```

This usually helps the final model use the context better.

---

# Concrete retrieval improvements I would do first

If I had to prioritize, I would do this:

## Phase 1 — immediate improvement, low risk
1. Add heuristic planner
2. Different query per source
3. Increase `minScore`
4. Reduce result defaults
5. Add per-char budget
6. Better output compaction

With this you should already notice much less garbage.

## Phase 2 — dedicated model with fallback
1. New `context-enricher` agent
2. LLM-based planner
3. Fallback to coder
4. If everything fails, heuristic planner

## Phase 3 — LLM-assisted compression
1. Retrieval with rewritten queries
2. Final context summary by the dedicated/fallback model

---

# Risks and considerations

## Latency
Each extra model call adds latency before responding.
Mitigations:
- very short planner, low max tokens
- fast/cheap model
- short timeout
- immediate fallback

## Cost
If you enable the planner on every prompt, cost increases.
Mitigations:
- only when the prompt exceeds a certain complexity threshold
- or when Remembrances are active and there are enough sources
- or cache the plan per turn

## Fragility
If the planner returns malformed JSON:
- fallback to coder
- if that fails too, heuristic fallback

## Over-engineering
It's not worth launching a full Mesnada-style subagent for this if a dedicated provider suffices.
The analogy with `summary/title` suggests better:
- **specific provider**, not a full subprocess/orchestrated agent

Unless you explicitly want to use tools from that subagent, which doesn't seem necessary in the first version.

---

# My final assessment

## Yes, direct Remembrances queries can clearly be improved
Mainly because today:
- they use the full prompt without rewriting
- they don't differentiate by source
- they don't do real reranking/compression
- they inject too much raw text

## Yes, a dedicated model/subagent with fallback to the coder can be added
And the project already has the pattern implemented in `summarizer`, which is the best reference.

## Design recommendation
The best practical solution would be:

1. **Don't query Remembrances with the raw prompt directly**
2. Introduce an **enrichment planner**
3. That planner can be:
   - heuristic by default
   - or LLM-based with a dedicated model
4. If the dedicated model fails:
   - fallback to coder
5. If the coder also fails:
   - heuristic fallback
6. Maintain strict budgets and final context compression

---

If you want, the next step I can do in two formats:

1. **more code-grounded technical analysis**, with a proposal for structs, interfaces, and exact files to modify
2. **direct implementation** of a first minimal version:
   - heuristic planner
   - per-source queries
   - fallback path prepared to later add the dedicated model

## User

Create an implementation plan for phase 1 and phase 2 as indicated, plus a new toggle in ACP to enable or disable context enrichment manually, also through a command in TUI and in web-ui. Save the implementation plan you generate in a document in kb

## User

Continue generating the implementation plan
