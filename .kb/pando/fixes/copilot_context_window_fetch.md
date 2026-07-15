---
created_at: 2026-07-16T19:06:11.372986177Z
updated_at: 2026-07-16T19:06:11.372986177Z
tags:
    - pando
    - fix
    - tokens
    - context-window
    - compaction
    - copilot
---
# Fix: GitHub Copilot fetcher discarded real context window / output limit

## Problem
Copilot models (e.g. GPT-5.3/5.4, real window 400k) were auto-compacting near
~150k tokens. Root cause: `fetchCopilotModels` in
`internal/llm/models/fetcher.go` never parsed `capabilities.limits` from the
GitHub Copilot `/models` response, so `FetchedModel.ContextWindow` was always
0. `fetchedModelContextWindow` in `internal/llm/models/registry.go` then fell
back to a hardcoded `128_000` for every dynamically-fetched Copilot model,
and `shouldCompact` (`internal/llm/agent/agent.go`) applied its 0.85
percentage threshold against that wrong, too-small window.

Cross-checked against opencode's own Copilot plugin
(`packages/opencode/src/plugin/github-copilot/models.ts`), which hits the
identical `${baseURL}/models` endpoint and correctly extracts
`capabilities.limits.max_context_window_tokens` (fallback
`max_prompt_tokens`) as context, and `max_output_tokens` as the output limit.

## Fix
[[project_tui_chat_info_sidebar_plan|Plan]] doc: `pando/plans/token_panel_context_window_plan.md`.

- `internal/llm/models/fetcher.go`:
  - `FetchedModel` gained `MaxOutputTokens int64`.
  - `copilotModelsURL` var introduced (was a hardcoded string) so it can be
    overridden in tests, mirroring `geminiModelsURL`/`openRouterModelsURL`.
  - `fetchCopilotModels` now parses `capabilities.limits.max_context_window_tokens`
    (falling back to `max_prompt_tokens`) into `ContextWindow`, and
    `capabilities.limits.max_output_tokens` into `MaxOutputTokens`.
- `internal/llm/models/registry.go`:
  - New helper `fetchedModelMaxOutputTokens(maxOutputTokens, contextWindow)`:
    returns the provider-reported value when > 0, else the old guessed
    default (`min(4096, contextWindow/2)`).
  - Both `RefreshProviderModels` and `modelFromFetchedAccountModel` now call
    this helper instead of inlining the guess, so `DefaultMaxTokens` is
    accurate for any provider that reports it (currently Copilot).

Pando's compaction algorithm itself (percentage-of-window,
`Agent.AutoCompactThreshold` default 0.85 in `shouldCompact`) was not
changed — it's architecturally sound; only the underlying context-window
*value* was wrong for Copilot. This is unlike opencode's absolute-buffer
compaction strategy, which was compared but intentionally not adopted.

## Verification
- `go build ./...` — clean.
- `go test ./internal/llm/models/...` — new tests pass:
  - `TestFetchCopilotModelsIncludesCapabilitiesLimitsAsContextWindow`
  - `TestFetchCopilotModelsFallsBackToMaxPromptTokens`
  - (mirrors existing `TestFetchGeminiModelsIncludesInputTokenLimitAsContextWindow`,
    `TestFetchOpenRouterModelsIncludesContextLength` pattern in
    `fetcher_context_window_test.go`)
- `go test ./internal/llm/agent ./internal/api` — verified command, ok.

## Not done (still pending, larger UI scope)
P1 from the plan doc — richer token panel (cache read/write, reasoning,
cost, per-message breakdown) in TUI `status.go` / `sidebarCmp` and WebUI
`sessionStore.ts` — not implemented in this pass, only the P0 context-window
fetch fix.
