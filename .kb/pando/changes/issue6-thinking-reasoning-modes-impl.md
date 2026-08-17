---
created_at: 2026-08-16T23:04:54.458525449Z
updated_at: 2026-08-16T23:04:54.458525449Z
---

# Issue #6 implementation — per-model reasoning-effort value sets

Implemented the 4 phases from `pando/analysis/issue6_thinking_reasoning_modes.md`. The
root cause was a single fixed `reasoning_effort` enum defaulting to `"medium"`, which
some models reject (e.g. `gpt-5-pro` accepts only `high`, `claude-sonnet-4-5` accepts
`low|high`). All consumers now resolve effort values against the actual model.

## Phase 1 — carry the value set on the model
- `internal/llm/models/models.go`: added `ReasoningEfforts []string` to `Model`.
- `internal/llm/models/modelsdev/catalog.go`: added `Model.ReasoningEffortValues()`
  to extract the `Type == "effort"` `ReasoningOptions` values; `SupportsReasoningEffort`
  is now derived from it.
- `internal/llm/models/modelsdev_enrich.go`: copies `ReasoningEffortValues()` into
  `Model.ReasoningEfforts` (only when not already set, honouring the
  "never overwrite known data" rule); tracks the slice in the change-detection return.

## Phase 2 — model-aware resolver (new file)
- `internal/llm/models/reasoning.go`:
  - `ReasoningEffortsFor(model) []string` — catalog value set first, else per-family
    fallback tables mirrored from opencode
    (`anthropicAdaptiveEfforts`, `anthropic46Efforts`, `anthropic45Efforts`,
    `openaiGPT51Efforts`, `openaiGPT52PlusEfforts`, `gpt5 pro/chat/codex`, etc.).
  - `DefaultReasoningEffort(model) string` — prefer `medium`, else `high`, else first
    value; never a value absent from the set.
  - `NormalizeReasoningEffort(model, value) string` — clamp/validate.

## Phase 3 — clamp instead of hardcoding `medium`
- `internal/llm/provider/openai.go`: `WithReasoningEffort` now clamps via a shared
  `clampReasoningEffort` (accepts `none|minimal|low|medium|high|xhigh`);
  `preparedParams` passes the validated value through verbatim.
- `internal/llm/provider/copilot.go`: `WithCopilotReasoningEffort` reuses
  `clampReasoningEffort`; `preparedParams` passes the value through verbatim.
- `internal/llm/provider/anthropic.go`: `WithAnthropicReasoningEffort` accepts
  `xhigh`; `thinkingBudgetTokens` handles `xhigh` (0.9 × maxTokens).
- `internal/llm/agent/session_overrides.go`:
  - `effectiveReasoningEffort` now takes the model, clamps override/config against the
    model, and falls back to `DefaultReasoningEffort(model)`.
  - `normalizeSessionReasoningEffort` accepts the union set (`none|minimal|low|medium|high|xhigh|max`).
- `internal/llm/agent/agent.go`: `createAgentProvider` passes `model` to
  `effectiveReasoningEffort` at all three provider sites.

## Phase 4 — drive ACP selectors from the resolver
- `internal/mesnada/acp/thinking_options.go`:
  - `normalizeReasoningEffortValue` accepts the union set.
  - `effectiveACPReasoningEffort(model, value)` and `parseReasoningEffortValue(model, value)`
    are model-aware (validate + default via the resolver).
- `internal/mesnada/acp/session_state.go`:
  - added `reasoningEffortNone/Minimal/XHigh/Max` constants.
  - `buildSessionConfigOptions` builds the effort selector from
    `models.ReasoningEffortsFor(selectedModel)` via new helpers
    `reasoningEffortConfigValues` / `reasoningEffortOption` (labels: None, Minimal,
    Low, Medium, High, X-High, Max).

## Tests
- Updated `modelsdev_enrich_test.go` and `agent_provider_test.go` for the new field
  and signature.
- Added `internal/llm/models/reasoning_test.go` for the resolver.

## Verification
- `go build ./...` clean.
- `go vet ./internal/llm/models/... ./internal/llm/agent/... ./internal/llm/provider/... ./internal/mesnada/acp/...` clean.
- `go test ./internal/llm/models/... ./internal/llm/agent ./internal/llm/provider/... ./internal/mesnada/acp ./internal/api` all pass.

Out of scope (per the analysis's step-5 note): the `internal/config/config.go`
reasoning-effort validation and the TUI/WebUI selector surfaces, both marked optional.
`Model.ReasoningEfforts` is exposed via its `json` tag for those surfaces when needed.