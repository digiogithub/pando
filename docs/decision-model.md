# Decision model

A **decision model** is a small, fast model that answers cheap multiple-choice or yes/no
questions for Pando through the System One endpoint (`POST {base}/v1/systemone`). It does not
write code or text: it classifies. Pando configures it **once**, in the top-level `[DecisionModel]`
block, and every feature that needs a quick decision reuses it.

| Consumer | Question it asks | Switch | Doc |
| --- | --- | --- | --- |
| Model auto mode | Which task route fits this prompt? | `[ModelAutoMode] Enabled` | [model-auto-mode.md](model-auto-mode.md) |
| Persona auto-select | Which persona fits this prompt? | `useDecisionModel` on the `persona-selector` agent | [model-auto-mode.md](model-auto-mode.md#decision-providers) |
| Context relevance filter | Is each retrieved KB/code/event snippet useful for this prompt? | `[Remembrances] ContextEnrichmentDecisionFilterEnabled` | [below](#context-relevance-filter) |
| Memory filter | Is each memory about to be injected useful for this prompt? | `[Remembrances] MemoryContextDecisionFilterEnabled` | [below](#context-relevance-filter) |

Every consumer is **off by default** and **fail-open**: if the decision model is missing, slow or
broken, Pando keeps working exactly as if the feature were off. Pando never installs Ollama or
pulls models for you.

Sources: [Ollama System One API](https://docs.ollama.com/api/systemone) ·
[OpenRouter Jev guide](https://openrouter.ai/docs/guides/community/jev) ·
[LiteLLM Jev blog post](https://docs.litellm.ai/blog/typesafe_jev).

## Providers and setup

### Ollama (local, default)

Needs Ollama **0.35.0 or newer** and a model with the `decision` capability:

```sh
ollama pull tev1:0.8b
```

then set `Model = 'tev1:0.8b'` (Settings > Decision model, `pando_setup decision-model set --model
tev1:0.8b`, or the TOML below). Notes:

- The model list only shows models whose `/api/tags` capabilities contain `decision`. If it is
  empty or unfiltered, your Ollama is older than 0.35 or the model is not a decision model.
- `tev1:0.8b` loads with `num_ctx` 2050, so the prompt and snippets Pando sends are truncated to
  fit; the request body is capped at 64 KiB (HTTP 413 above it).
- `:cloud` models are rejected by Ollama for System One: use a local model.
- `KeepAlive = '30m'` keeps the model loaded; calls then take tens to a few hundred milliseconds.

### TypeSafe Jev (hosted)

`Provider = 'typesafe'` uses `https://api.typesafe.ai`, model `jev-latest`. The key comes from
`Router.APIKey` or `$TYPESAFE_API_KEY`.

### Custom gateway

`Provider = 'custom'` with `BaseURL` set to the gateway root (Pando appends `/v1/systemone` and
`/v1/models`): OpenRouter (`https://openrouter.ai/api`, model `typesafe/jev-1.13`), a LiteLLM proxy
route, or any Jev-compatible server. Add `Headers` for extra headers and `APIKey` for bearer auth.

### Privacy

With a **hosted** provider (`typesafe` or `custom`) the decision questions leave your machine:

- model auto mode and persona auto-select send the user prompt, a few recent prompts and
  attachment names;
- the **context filter additionally sends the retrieved snippets** (code symbols, KB excerpts,
  events, memories, each capped at 400 characters by default).

For that reason the filter only uses **local (Ollama)** providers unless you explicitly allow
hosted ones (`ContextEnrichmentDecisionFilterAllowHosted = true`, "Allow hosted decision
providers" in the settings). API keys are stored encrypted in the config (or as a `$ENV_VAR`
reference), masked in the API and UI, and never logged.

## Configuration

```toml
[DecisionModel]
TimeoutMs = 0             # per-call bound; 0 = 1500 ms (ollama) / 3000 ms (remote)

[DecisionModel.Router]
Provider  = 'ollama'      # ollama | typesafe | custom (default ollama)
BaseURL   = ''            # empty = provider default (Ollama localhost:11434, TypeSafe API); required for custom
Model     = 'tev1:0.8b'
KeepAlive = '30m'
APIKey    = ''            # encrypted at rest; "$ENV_VAR" allowed; typesafe falls back to $TYPESAFE_API_KEY
# [DecisionModel.Router.Headers]
# X-Example = 'value'
```

The block has a JSON schema entry like the other config sections, is hot-reloaded (the next
prompt uses the change in all consumers, no restart) and honours enterprise locks.
`pando init` writes the empty block.

### Migrating from `modelAutoMode.router`

Before this block existed, the provider lived inside model auto mode as `[ModelAutoMode.Router]`
(and `ModelAutoMode.TimeoutMs`). On load, if `DecisionModel.Router.Model` is empty and the legacy
router has a model, Pando **copies it into `[DecisionModel]`, rewrites the config file and logs one
info line**. The legacy keys are still read (no startup error) but are never written again. The
migration is idempotent; if the file is locked by an enterprise policy the migrated value stays
in memory only and a warning is logged. `[ModelAutoMode]` keeps only the routing policy (`Enabled`,
`DefaultAuto`, `Threshold`, `MinConfidence`, `HistoryPrompts`, `Routes`).

## Configuring it

- **WebUI / desktop:** Settings > Decision model (AI group): provider, base URL, key, headers,
  keep-alive, model discovery, **Pull** suggested Ollama models, **Test connection**, privacy
  note. Model auto mode, the `persona-selector` agent and Remembrances show a read-only "decision
  model in use" row linking there, with a warning when their option is on and no model is set.
- **TUI:** Settings > Decision model with the same fields and actions; the Auto mode,
  persona-selector and Remembrances sections show the read-only line.
- **ACP / CLI:** `pando_setup decision-model [show|set|test|models|clear-key]` (see
  [pando-setup.md](pando-setup.md#decision-model)) and the ACP `/decision-model [test]` command
  (see [acp.md](acp.md#decision-model)).
- **`pando doctor`:** a "Decision model" block prints the provider, effective URL, model, timeout,
  which consumers are on (and the filter's threshold, candidate cap and local/hosted scope), the
  config validation and a live health verdict with the exact fix (for example
  `ollama pull tev1:0.8b` or "Upgrade Ollama to >= 0.35"). It reports "not used" when no consumer
  is enabled.

### REST endpoints

All under `/api/v1` and your normal WebUI auth.

| Endpoint | Purpose |
| --- | --- |
| `GET/PUT /config/decision-model` | read/save the block (key masked; an empty or masked key keeps the stored one; `clearApiKey` removes it; field errors return 400) |
| `DELETE /config/decision-model/api-key` | remove the stored key |
| `GET/POST /decision-model/router/models` | list decision models (POST accepts a draft router) |
| `POST /decision-model/router/test` | test connection, optional draft router |
| `GET /decision-model/router/health` | cached health |
| `POST /decision-model/router/pull`, `GET .../pull/{id}` | start and follow an `ollama pull` of a suggested model |

The old `/api/v1/model-auto-mode/router/*` paths remain as **deprecated aliases for one release**
(they log a deprecation warning once). `GET /config/model-auto-mode` returns `router` only as a
read-only copy of the decision model, and a `PUT` that still sends one forwards it to the decision
model with a deprecation warning.

## Context relevance filter

Context enrichment (KB, events and code snippets appended to the user message) and memory
injection (a `<memories>` block in the system prompt) select snippets by embedding score, which
says a text is *similar*, not that it is *useful for this task*. With the filter on, after
retrieval Pando asks the decision model one question per candidate ("is this item useful to carry
out the user's request?", answers `useful` / `not_useful`) and drops the candidates whose
probability of `useful` is below the threshold, **before formatting and before the character
budgets are applied**.

```toml
[Remembrances]
ContextEnrichmentDecisionFilterEnabled = false   # default off: KB/events/code snippets
MemoryContextDecisionFilterEnabled     = false   # default off: injected memories
ContextEnrichmentDecisionFilterThreshold = 0.60  # keep when p(useful) >= threshold
ContextEnrichmentDecisionFilterMaxCandidates = 32      # candidates judged per turn
ContextEnrichmentDecisionFilterMaxCandidateChars = 400 # text sent per candidate
ContextEnrichmentDecisionFilterAllowHosted = false     # false = local providers only
```

Semantics:

- **Fail-open.** An unreachable or old Ollama (< 0.35), timeout, HTTP error, malformed or
  unexpected answer, an empty router model, or a hosted provider with `AllowHosted = false`
  keeps every candidate. When only some requests of a turn fail, the failed ones are kept and the
  rest still apply (reported as partial).
- **Unasked candidates are kept.** Only the first `MaxCandidates` non-pinned candidates, in the
  fixed order code, KB, events, memories, are asked. Candidates beyond the cap, or that do not fit
  the model's token budget or the 64 KiB body, are kept unseen, never dropped blind. Up to 64
  candidates travel in one request; more are split into several concurrent requests.
- **Pinned memories are never dropped.** Memories from `MemoryPinnedScopes` are not even asked.
  Hit counters only move for memories that are actually injected.
- **Eligibility.** The filter runs on the main coder agent for user-initiated turns. It is skipped
  for delegated subagents and child sessions, clean-mode and system-initiated runs; those turns
  inject context unfiltered. The output of the agent-loop context enricher (already curated) is
  never filtered.
- **Latency bound.** Calls run after the parallel searches and are bounded by
  `DecisionModel.TimeoutMs` (plus a small grace); the shared decision model is also used for
  model auto mode and persona auto-select, so those add their own calls in the same turn.
- **Notice.** Only when something was dropped, every client (WebUI, desktop, TUI, ACP, AG-UI)
  shows a status line like
  `Context filter: kept 4/9 (38 ms) — code 1/3, kb 2/4, events 1/2` (only sources that had
  candidates are listed). Fail-open conditions show a single warning per session and error class,
  for example `Context filter unavailable (<reason>): context injected unfiltered`. Nothing is shown
  when the filter ran and dropped nothing.
- **Observability.** A debug log per turn with kept/dropped per source, latency and reason; a
  `context_filter` extension event and counter log records. None of them contain prompt or snippet
  text.

### Is it worth enabling? Benchmark summary

The filter ships **default-off**. On a labelled set of 72 candidates (38 useful, 34 close
distractors; LLM-authored, so indicative only) with `tev1:0.8b` on Ollama 0.35 and a GPU, the
default threshold 0.60 gave precision 0.833 and recall 0.658, below the ship gate of precision
>= 0.85 and recall >= 0.80. Threshold 0.50 gave precision 0.744 and recall 0.763. Latency was
about 160 ms p50 per request of 4 candidates. The filter was strongest on code candidates and weak
on KB and memory ones. See [tests/decision_model/REPORT.md](../tests/decision_model/REPORT.md) for the numbers,
caveats and recommendation. If you enable it, start with a threshold around 0.50 and watch the
notices.

Reproduce with `python3 tests/decision_model/relevance_bench.py` and
`PANDO_LIVE_OLLAMA=1 go test ./internal/llm/modelrouter -run RelevanceLive -v`.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| "Context filter: no decision model is configured" | Set `DecisionModel.Router.Model` (Settings > Decision model) |
| No notice ever appears | The filter is off, the turn is not eligible, or nothing was dropped (the notice only shows drops) |
| Filter never applies with TypeSafe/custom | `AllowHosted` is false; enable it only if sending snippets to that provider is acceptable |
| "Upgrade Ollama to >= 0.35" | Update Ollama; `/v1/systemone` does not exist before 0.35 |
| Useful context disappears | Lower the threshold (0.50), or turn the filter off; the benchmark shows 0.60 drops about a third of useful candidates with `tev1:0.8b` |
| HTTP 401/403/413, `:cloud` model rejected | See [model-auto-mode.md](model-auto-mode.md#troubleshooting) |
