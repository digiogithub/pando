---
created_at: 2026-08-16T22:21:52.246563335Z
updated_at: 2026-08-16T22:21:52.246563335Z
---
# Issue #6 — "thinking or reasoning mode is different by model, default value gets errors" (analysis, 2026-08-17)

GitHub issue #6 body: "Pando uses medium as reasoning mode, but the enum values are different by model or family model, check vscode copilot API and OpenRouter integration in opencode".

Read-only analysis. No code changed yet.

## Root cause

Pando models reasoning/thinking as a **single fixed enum** and defaults it to `"medium"`, but the real accepted values differ per model family. When the configured/default `"medium"` is not in a model's actual value set, the provider returns an error.

### Where the fixed enum + "medium" default live

- `internal/llm/agent/session_overrides.go`
  - `effectiveReasoningEffort` returns the configured value verbatim (empty if unset).
  - `normalizeSessionReasoningEffort` / `normalizeSessionThinkingMode` only accept
    `low|medium|high` (effort) and `disabled|low|medium|high` (thinking), rejecting
    anything else.
- `internal/llm/agent/agent.go:2577` (`createAgentProvider`)
  - Anthropic path defaults `ThinkingMode` to `config.ThinkingMedium` via
    `defaultAnthropicThinkingMode` (`agent.go:2613`).
  - Passes `effectiveReasoningEffort(...)` into `WithReasoningEffort` /
    `WithCopilotReasoningEffort` / `WithAnthropicReasoningEffort`.
- `internal/llm/provider/copilot.go:1100` `WithCopilotReasoningEffort`
  - hardcodes `defaultReasoningEffort := "medium"` and only maps `low|medium|high`.
- `internal/mesnada/acp/session_state.go:39-40`
  - `defaultACPReasoningEffort = "medium"`, `defaultACPThinkingMode = "medium"`.
- `internal/mesnada/acp/thinking_options.go`
  - `normalizeReasoningEffortValue` / `normalizeThinkingModeValue` accept only the
    same fixed sets.
- `internal/mesnada/acp/session_state.go:236-265` (`buildSessionConfigOptions`)
  - The ACP selector always lists `Low/Medium/High` (effort) and
    `Disabled/Low/Medium/High` (thinking), regardless of the selected model.

### Why "medium" is wrong for many models

Reference from opencode (`packages/opencode/src/provider/transform.ts`), which
models this correctly as a per-model-family value set:

- OpenAI `reasoning_effort`:
  - widely supported: `low, medium, high`
  - `gpt-5.1` → `none, low, medium, high`
  - `gpt-5.2+` → `none, low, medium, high, xhigh`
  - `gpt-5 codex` (`3+`) → `none, low, medium, high, xhigh`
  - `gpt-5 chat` → `medium` only
  - `gpt-5 pro` → `high` only  ← **default "medium" is invalid here**
- Anthropic adaptive thinking (`opus-4.7+`, `sonnet-5+`, `fable-5`):
  `low, medium, high, xhigh, max`
- Anthropic `opus-4.6`/`sonnet-4.6`: `low, medium, high, max`
- Anthropic `opus-4.5`: effort `low, medium, high`
- Gemini `thinkingLevel`: `low, high` (gemini-3 non-image), `minimal, low, medium, high`
  (flash), `high` (pro-image)
- OpenRouter `reasoning.effort`: `low, medium, high`
- Copilot reasoning_effort: `low, medium, high` plus `xhigh` for newer gpt-5 models.

opencode exposes these through a `variants(model)` → per-effort provider-option
map and `reasoningEffort(model, effort)` → concrete request body, keyed by
`model.api.npm` (the concrete SDK) + model family regexes.

## Data source already present in Pando (but discarded)

`internal/llm/models/modelsdev/catalog.go` already parses the models.dev
`ReasoningOptions` (`[]ReasoningOption{Type string, Values []string}`) and even
ships a real example where Ancair `claude-sonnet-4-5` has effort values
`["low", "high"]` — no `"medium"`.

But `internal/llm/models/modelsdev_enrich.go:122` collapses this to a single bool:

```go
if entry.SupportsReasoningEffort() {
    model.SupportsReasoningEffort = true
}
```

`ModelsDevMetadata` (`modelsdev_enrich.go:34`) returns the full `modelsdev.Model`
including `ReasoningOptions`, but `applyModelsDevMetadata` drops `Values`.

`models.Model` (`internal/llm/models/models.go:15`) has only `CanReason` and
`SupportsReasoningEffort bool` — there is no slot for the ordered list of valid
values, so the selector cannot render per-model options.

## proposed fix

1. **Carry the valid value set on `models.Model`**
   - Add `ReasoningEfforts []string` (and, if needed for anthropic-adaptive
     `xhigh/max`, a `thinking_mode` analog) to `models.Model`.
   - In `applyModelsDevMetadata`, copy `ReasoningOptions` values into these fields
     (keyed by `Type == "effort"` and `Type == "thinking"|"thinking_budget"`),
     keeping the existing "only raise, never clear" rule and keeping
     `SupportsReasoningEffort` derived from `len(values) > 0`.

2. **Add a model-aware resolver** (single source of truth)
   - `models.ReasoningEffortsFor(model) ([]string, bool)` returns the allowed,
     ordered values for the model (from the stored list, else a provider/family
     fallback table for models the catalog misses, mirroring opencode's
     `openaiReasoningEfforts` / `anthropicAdaptiveEfforts` / `googleThinkingLevelEfforts`).
   - `models.DefaultReasoningEffort(model) string`: prefer `"medium"` when present
     in the list, else `"high"` (the most common safe default), else first value.
     Never return a value absent from the list.

3. **Validate/clamp instead of hardcoding "medium"**
   - `WithReasoningEffort` / `WithCopilotReasoningEffort` /
     `WithAnthropicReasoningEffort`: accept the full per-model set; pass through
     any value the resolver reports as valid; log+fallback to the model's
     default when invalid (instead of always defaulting to `"medium"`).
   - `effectiveReasoningEffort` / `effectiveAnthropicThinkingMode`: compute the
     default through the resolver for the *resolved* model, not a constant.

4. **Drive the selectors from the resolver**
   - ACP `buildSessionConfigOptions`: build the efffort/thinking `configOptionValue`
     list from `models.ReasoningEffortsFor(selectedModel)` (map to human labels;
     keep `xhigh`→"X-High", `max`→"Max", `none`/`minimal` as-is).
   - ACP `normalize*` / `parse*` / `effective*` in `thinking_options.go`: validate
     against the model's value set instead of the fixed enum; `effectiveACPReasoningEffort`
     defaults to the model's default rather than the constant.
   - TUI/WebUI config surface: render the same per-model set for the reasoning/
     thinking selector so it matches the API's real enum.

5. **Scope**
   - `internal/llm/models` (struct + resolver + enrichment)
   - `internal/llm/agent` (`createAgentProvider`, `session_overrides.go`)
   - `internal/llm/provider` (`openai.go`, `copilot.go`, `anthropic.go`)
   - `internal/mesnada/acp` (`thinking_options.go`, `session_state.go`)
   - optional: `internal/api/handlers_models.go` / TUI to expose the value list in
     the model info used to render the selector.

## Open question

- Confirm exact models.dev `ReasoningOptions.Type` strings (`effort`, `thinking`,
  `thinking_budget`, `thinking_level`) so the mapping in step 1 covers Anthropic
  and Gemini as well as OpenAI. If the catalog omits `xhigh/max` for adaptive
  Anthropic, the fallback table must supply them (mirror opencode).

## Verification

Static tracing + opencode cross-reference. No build/test run yet (no code changed).