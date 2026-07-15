---
created_at: 2026-07-16T18:59:35.229013581Z
updated_at: 2026-07-16T18:59:35.229013581Z
tags:
    - pando
    - plan
    - tokens
    - context-window
    - compaction
    - copilot
---

# Plan: Fix context-window sizing + richer token panel (TUI/WebUI), inspired by opencode

## Root cause found: premature auto-compact on Copilot GPT-5.x models

`internal/llm/models/fetcher.go:fetchCopilotModels` (line ~234) calls
`https://api.githubcopilot.com/models` but its response struct only decodes
`id`, `name`, `version`, `model_picker_enabled`, `policy.state`. It **drops
the `capabilities.limits` object entirely**, even though the same endpoint
returns `capabilities.limits.max_context_window_tokens` and
`max_output_tokens` per model.

Because `FetchedModel.ContextWindow` is therefore always `0` for every
Copilot model, `internal/llm/models/registry.go:fetchedModelContextWindow`
(line 353) falls back to a **hardcoded 128_000** for every Copilot model
regardless of its real window — so `gpt-5.3`/`gpt-5.4` (400k real window)
get treated as 128k. `internal/llm/agent/agent.go:shouldCompact` (line 1827)
then computes `compactAt = (contextWindow - reserved) * threshold` off that
wrong 128k base, explaining the observed premature compaction just above
~150k tokens instead of near 400k.

opencode's reference implementation
(`packages/opencode/src/plugin/github-copilot/models.ts`) parses exactly
this: `limit.context = capabilities.limits.max_context_window_tokens ??
max_prompt_tokens`, `limit.output = max_output_tokens`. Pando's own comment
in `fetchCopilotModels` ("Matches the opencode reference implementation
filter logic") shows the picker/policy filter was ported from opencode but
the limits parsing was not.

### Fix (P0, small/contained)
1. In `internal/llm/models/fetcher.go`, extend the Copilot response struct
   with `Capabilities.Limits.MaxContextWindowTokens` and
   `MaxOutputTokens` (mirror opencode's schema) and populate
   `FetchedModel.ContextWindow` from it (fallback to `max_prompt_tokens` if
   `max_context_window_tokens` absent, matching opencode).
2. Add a `MaxOutputTokens` field to `FetchedModel` and thread it through
   `registry.go` (`modelFromFetchedAccountModel`,
   `RefreshProviderModels`) into `Model.DefaultMaxTokens` — currently
   `maxTokens` is a guessed `contextWindow/2` fallback instead of the
   provider's real cap.
3. Keep `fetchedModelContextWindow`'s 128_000 default only as the
   last-resort fallback when the API genuinely omits limits (should now be
   rare).
4. Add regression test mirroring
   `fetcher_context_window_test.go` (`TestFetchGeminiModelsIncludesInputTokenLimitAsContextWindow`)
   for Copilot: `TestFetchCopilotModelsIncludesCapabilitiesLimitsAsContextWindow`.

This is the only change needed to fix the actual "compacts too early"
symptom the user hit — no changes to `shouldCompact`'s trigger math are
needed once the window value is correct.

## Architecture comparison: how opencode sources context window vs pando

**opencode**: single source of truth is the model's own provider payload
(`limit.context` / `limit.output`), sourced either from `models.dev`'s
catalog (`fromModelsDevModel`, `packages/opencode/src/provider/provider.ts:1207`)
for providers without a rich models endpoint, or parsed directly from a
provider's native `/models` response when it's richer (Copilot). Compaction
(`packages/core/src/session/compaction.ts:compactIfNeeded`) triggers when
`estimate(system+messages+tools) > context - max(output, buffer=20_000)` —
no fixed percentage threshold, buffer is an absolute token reservation, and
`output` is the model's real max-output limit, not a config guess.

**pando**: static per-provider Go tables (`internal/llm/models/anthropic.go`,
`gemini.go`, `azure.go`, `vertexai.go`, ...) for providers without listing
APIs, and `FetchModelsFromProvider`/`RefreshProviderModels` for providers
that expose one (Copilot, OpenAI, Gemini, OpenRouter, ...). The bug above is
that the Copilot path silently discards the real limits it already fetches.
Compaction is a percentage of `(contextWindow - reserved) * AutoCompactThreshold`
(default 0.85, config `internal/config/config.go:Agent.AutoCompactThreshold`,
per-agent override in `.pando.toml`), which is reasonable and roughly
equivalent to opencode's method once the window value itself is correct.

## Token panel comparison

opencode's right sidebar (`packages/app/src/components/session/session-context-tab.tsx`
+ `session-context-metrics.ts` + `session-context-usage.tsx`) shows, per
session: provider/model label, context limit, total tokens, usage %, input
tokens, output tokens, **reasoning tokens**, **cache read/write tokens**,
user/assistant message counts, total cost, session created/last-activity
timestamps, plus a colored breakdown bar (system/user/assistant/tool/other)
and the raw system prompt. A small `ProgressCircle` + tooltip (cost/usage%/
total tokens) lives in the button that opens the tab.

pando's TUI status bar (`internal/tui/components/core/status.go`) and
WebUI (`web-ui/src/stores/sessionStore.ts`) only ever carry/display
`PromptTokens + CompletionTokens` vs `ContextWindow`, rendered as a single
"Context: 110K (82%)" badge with a warning color past 80%. There is no
expandable panel, no cache read/write, no reasoning-token count, no cost,
no per-role breakdown — even though the underlying data mostly already
exists: `provider.TokenUsage` (`internal/llm/provider/provider.go`) has
`InputTokens`/`OutputTokens`/`CacheReadTokens`/`CacheCreationTokens`, and
`session.Session` has `Cost`. It's `AgentEvent.TokenUsage` /
`TokenUsageInfo` (`internal/llm/agent/agent.go:189`) and
`TokenUsageMsg` (`status.go:70`) that flatten everything down to just
prompt+completion before it ever reaches the UI, so the richer numbers
never make it out of the agent layer.

### Fix (P1, larger, UI-visible)
1. Extend `session.Session` (already has `Cost`) — confirm cache
   read/creation and reasoning tokens are persisted per-session (check
   `TrackUsage` in `agent.go:1699`; currently `CompletionTokens =
   OutputTokens + CacheReadTokens` and `PromptTokens = InputTokens +
   CacheCreationTokens`, which already conflates cache into prompt/completion
   and loses the split — needs separate fields, e.g. `CacheReadTokens`,
   `CacheCreationTokens`, `ReasoningTokens` on `session.Session`).
2. Widen `TokenUsageInfo` / `TokenUsageMsg` / the WebUI SSE payload
   (`internal/api/handlers_chat.go:dispatchSSEEvent`) and
   `web-ui/src/stores/sessionStore.ts` to carry the split fields plus cost.
3. TUI: add a togglable expanded panel (reuse the existing
   `internal/tui/components/chat/sidebar.go` `sidebarCmp`, per
   `[[project_tui_chat_info_sidebar_plan]]`) showing the opencode-style
   stat grid (provider/model, limit, total, usage%, input/output/reasoning/
   cache read+write, message counts, cost, timestamps) instead of only the
   compact status-bar badge.
4. WebUI: add an equivalent panel/tab component consuming the widened
   store fields; a simple stat grid, no need to replicate the breakdown bar
   or raw-message accordion initially.
5. Non-goal for this pass: the system/user/assistant/tool token breakdown
   bar and raw-message JSON viewer — cosmetic, can follow later if wanted.

## Verification plan
- Unit test for the Copilot fetcher fix (see P0.4).
- `go test ./internal/llm/models ./internal/llm/agent ./internal/api` after
  wiring changes.
- Manual: point a real Copilot account with a `gpt-5.3`/`gpt-5.4` model at
  pando, open TUI status bar / new sidebar panel, confirm the shown context
  window is 400k-class and auto-compact no longer triggers until near that
  real limit.

## Cross-references
- [[fix_macos_wasm_hardened_runtime_jit_kill]] unrelated but same repo.
- [[project_tui_chat_info_sidebar_plan]] — the orphaned `sidebarCmp` this
  plan proposes reusing for the expanded token panel.
- opencode research doc: `opencode/research/tui_sidebar_info_column.md`
  (plugin slot system reference, not directly reusable since pando's TUI is
  Go/Bubbletea not SolidJS, but useful for what fields to surface).
