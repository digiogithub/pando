# Model auto mode

Auto mode lets Pando pick the model for each prompt. A tiny **decision model**
(configured once in the shared `[DecisionModel]` block, see [decision-model.md](decision-model.md))
reads the
prompt once, answers a single multiple-choice question, and Pando sends the turn
to the model you configured for the winning route. Expensive models are used for
hard work, cheap or local ones for quick questions, and you choose the rules.

It is **off by default**. Pando never installs Ollama or pulls models for you.

Sources: [Ollama System One API](https://docs.ollama.com/api/systemone) ·
[OpenRouter Jev guide](https://openrouter.ai/docs/guides/community/jev) ·
[LiteLLM Jev blog post](https://docs.litellm.ai/blog/typesafe_jev).

## How routing works

1. You select **Auto** as the model (WebUI, TUI, ACP, `pando_setup`).
2. On each **user prompt** (once per prompt, not per tool step), the router builds
   one `choice` question whose criteria are your route descriptions plus an
   implicit `none`, and sends it to `POST {router}/v1/systemone` together with the
   prompt and a few recent user prompts as state.
3. The answer contains `choice` and `probabilities`. The route wins only if
   `probabilities[choice] >= Threshold` (default `0.60`). `MinConfidence`
   (default `0`, off) is an optional extra gate.
4. **No match** (choice is `none`, probability below threshold, router
   unreachable or errors, no routes): the turn runs on the normal **coder** model.
   The router never blocks a turn.
5. **Fallbacks and failover:** a route has a primary `Model` and up to 2
   `Fallbacks`. If the provider fails with a retryable class (rate limit, server
   error, network, auth, model not found) the same turn is retried on the next
   candidate and the notice says so. Context-length, content-policy, bad-request
   and tool-execution errors never fail over. Candidates that are unknown,
   disabled, lack attachment support for the current prompt, or whose context
   window is too small are skipped; if none is usable the turn runs on the coder.
6. **Subagents are unaffected.** Delegated tasks keep their own configured model.
7. **Prompt cache:** switching models between prompts invalidates provider prompt
   caches for that conversation. Within one prompt (tool loops) the model stays
   fixed, so caching still works there. Prefer few routes with stable targets.

### Confidence is not correctness

The API's `confidence` is `1 - H(p)/ln(N)`: how concentrated the distribution is,
not whether the answer is right. Pando routes on `p(choice)`, which is what the
threshold applies to. Leave `MinConfidence` at `0` unless you have measured a need.

## Decision providers

The decision provider (Ollama `tev1:0.8b`, TypeSafe Jev or a custom Jev-compatible gateway), its
API key, timeout, privacy implications and the migration from the old `[ModelAutoMode.Router]`
block are documented in **[decision-model.md](decision-model.md)**. Model auto mode, persona
auto-select and the context relevance filter all share that one provider. Configure it under
Settings > Decision model, with `pando_setup decision-model`, or in `[DecisionModel]`; auto mode
only keeps the routing policy below.

## Configuration reference

Routing policy only; the provider lives in [`[DecisionModel]`](decision-model.md#configuration).

```toml
[ModelAutoMode]
Enabled        = true     # default false
DefaultAuto    = true     # new sessions start in Auto when enabled (default true)
Threshold      = 0.60     # min p(choice) to route (default 0.60)
MinConfidence  = 0.0      # optional extra gate (default 0 = off)
HistoryPrompts = 0        # recent user prompts sent as context; 0 = built-in default
# The decision provider is configured in [DecisionModel] (docs/decision-model.md).

[[ModelAutoMode.Routes]]
ID          = 'quick'
Description = 'Short question or explanation about code, a concept, an error message or a command; no code changes needed.'
Model       = 'ollama.qwen2.5-coder:7b'
Fallbacks   = []

[[ModelAutoMode.Routes]]
ID          = 'implementation'
Description = 'Write, modify, refactor or fix code across one or more files, including adding tests.'
Model       = 'anthropic.claude-sonnet-4'
Fallbacks   = ['copilot.gpt-5.4']

[[ModelAutoMode.Routes]]
ID          = 'planning'
Description = 'Design, architecture, trade-off analysis or planning a feature before implementing it.'
Model       = 'anthropic.claude-opus-4'
Fallbacks   = ['anthropic.claude-sonnet-4', 'copilot.gpt-5.4']
```

Constraints: at most 25 routes, 2 fallbacks per route, descriptions up to 500
characters, route IDs unique and not `none` (reserved). `Disabled = true` on a
route keeps it in the file but out of the question. Model IDs are the same ids
shown in the model selector. The WebUI and `pando doctor` report unknown models.

## Using Auto

- **WebUI / desktop:** Auto is the first entry of the model switcher; while a turn
  runs, the switcher shows `Auto · <model picked>`.
- **TUI:** Auto is the first entry of the model dialog.
- **ACP (Zed, Xcode, ...):** Auto appears as the first model in the session model
  list; the routing notice is sent as a system message.
- **`pando_setup`:** `model auto` selects Auto, a concrete model leaves it.
- Picking a concrete model turns Auto off for that session until you select it again.

### Settings UI

Settings > Model auto mode (WebUI) and the TUI equivalent edit the routing policy above:
routes with fallbacks, threshold. A read-only row shows the decision model in use and links
to Settings > Decision model, where the provider, model picker (filtered by the `decision`
capability) and **Test connection** live. The **playground**
routes a sample prompt against the current (even unsaved) draft and shows the
chosen route, probabilities, candidates and why other candidates were skipped,
without sending anything to an LLM.

### REST endpoints

All under `/api/v1` and your normal WebUI auth.

| Endpoint | Purpose |
| --- | --- |
| `GET/PUT /config/model-auto-mode` | read/save the routing policy (`router` is a read-only copy of the decision model; a legacy `router` in a PUT is forwarded to the decision model with a deprecation warning) |
| `/decision-model/router/*`, `/config/decision-model` | provider, model discovery, test and health: see [decision-model.md](decision-model.md#rest-endpoints) (the old `/model-auto-mode/router/*` paths are deprecated aliases) |
| `POST /model-auto-mode/playground` | route a prompt against the saved or a draft config |
| `GET /models`, `PUT /models/active` | `auto` entry and selection |

### `pando doctor`

`pando doctor` includes a Model auto mode section (routes and their models known) and a
separate Decision model section with provider reachability, Ollama version (must be >= 0.35)
and decision model presence. It prints the exact fix, for example `ollama pull tev1:0.8b` or
"Upgrade Ollama to >= 0.35".

## Routing notices

Every routing decision adds a one-line notice in each client (chat, TUI, ACP):

```
Auto: implementation → anthropic.claude-sonnet-4 (p=0.93, 38 ms via ollama/tev1:0.8b)
Auto: no confident match (best quick p=0.41) → <coder>
Auto: router unavailable (<class>) → <coder>
Auto: <A> failed (<class>), retrying on <B>
Auto: route <id> has no usable model → <coder>
```

The WebUI also receives the structured routing info over SSE.

## Telemetry

The extension topic `model_route` (event `routed`) carries model, route id,
fallback flag, reason, probability, confidence, router provider/model, latency and
optional cost. **It never includes prompt text.**

## Writing good route descriptions

Benchmark lessons with `tev1:0.8b` (30 labelled prompts, 4 routes + `none`,
accuracy 0.90, p50 about 60 ms locally):

- Plain "quick question" prompts score only p about 0.64-0.70, which is why the
  default threshold is `0.60`. Raising it to 0.75 sends most quick questions to
  the coder. Lower only if you accept more misroutes.
- Short follow-ups ("ok continue", "sí, hazlo", "do it") land on `none` with
  p about 0.7-0.9: they stay on the coder model, which is usually what you want.
- Weak spots: `translate this paragraph...` tends to `none` and `write the commit
  message` tends to `implementation`. Name these explicitly in the description
  ("documentation, README, commit messages, translations") or leave them on the coder.
- One sentence per route, describing the *task*, with concrete verbs and nouns.
  Make routes mutually exclusive; overlapping descriptions split probability and
  fall below the threshold.
- Use few routes (3-5). Each extra route dilutes the probabilities.
- Validate with `tests/model_auto_mode/bench_router.py` and the playground.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| Everything goes to the coder | Open the playground; check `reason`. `low_probability`: lower `Threshold` or sharpen descriptions. `router_error`: see Test connection |
| "Upgrade Ollama to >= 0.35" | Update Ollama; `/v1/systemone` does not exist before 0.35 |
| Model list not filtered / empty | Model has no `decision` capability; `ollama pull tev1:0.8b` |
| HTTP 400 on a `:cloud` model | System One is local-only on Ollama; use a local model or TypeSafe |
| HTTP 401 / 403 | Wrong or missing API key (`$TYPESAFE_API_KEY`) |
| HTTP 413 | State over 64 KiB; shorten prompt/attachments |
| Timeouts on first request | Model cold start; raise `DecisionModel.TimeoutMs`, keep `KeepAlive` |
| "route has no usable model" | All candidates unknown/disabled/too small or without attachment support |

## Validation scripts

```sh
python3 tests/model_auto_mode/bench_router.py --model tev1:0.8b --min-accuracy 0.8
PANDO_LIVE_OLLAMA=1 python3 tests/model_auto_mode/live_providers.py
PANDO_E2E_BASE_URL=http://127.0.0.1:8765 python3 tests/model_auto_mode/test_playground_live.py
```

They skip cleanly when the server or credentials are absent.
