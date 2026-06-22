---
created_at: 2026-06-22T08:02:04.078126793Z
updated_at: 2026-06-22T08:02:04.078126793Z
tags:
    - fix
    - tui
    - tokens
    - context-window
    - models
    - pando
---
# Fix: context window resolution and TUI token warning rendering

Date: 2026-06-22

## What changed

Adjusted context-window handling and token display in these files:
- `internal/tui/components/core/status.go`
- `internal/llm/models/fetcher.go`
- `internal/llm/models/registry.go`
- `internal/mesnada/acp/agent.go`
- `internal/llm/models/fetcher_context_window_test.go`

### TUI rendering
- Fixed the status-bar token label so high-usage warning state no longer replaces the token count.
- The TUI now keeps showing the used token total and appends the warning icon plus percentage, instead of showing only the warning percentage.

### Context window sourcing
- Gemini model discovery now reads `inputTokenLimit` from the provider models API and stores it as `ContextWindow`.
- OpenRouter model discovery now reads `context_length`, preferring `top_provider.context_length` when available.
- Dynamic model registration now consistently uses the shared `fetchedModelContextWindow()` fallback path instead of inlining a separate default.
- ACP usage snapshots no longer inject a fake `200000` context size when the session has no known context window; unknown stays unknown.

## Why

The previous TUI warning behavior hid the actual token usage near 100%, which made the counter misleading. Also, several providers were returning model metadata that included exact context-window information, but Pando was not ingesting it, so calculations could fall back to inaccurate defaults.

External review during this task found:
- Gemini models API documents `inputTokenLimit` / `outputTokenLimit` in model metadata.
- OpenRouter `/api/v1/models` returns `context_length` and can expose provider-specific values under `top_provider.context_length`.
- Groq already returns `context_window` in its models API and Pando was already consuming that.
- Anthropic/OpenAI model-list endpoints do not appear to expose context-window values directly in the listing response, so curated static metadata remains necessary there.

## Verification

Ran:
- `gofmt -w internal/tui/components/core/status.go internal/llm/models/fetcher.go internal/llm/models/registry.go internal/mesnada/acp/agent.go internal/llm/models/fetcher_context_window_test.go`
- `go test ./internal/llm/models ./internal/tui/components/core ./internal/mesnada/acp`

Added tests covering:
- Gemini `inputTokenLimit` -> `ContextWindow`
- OpenRouter `context_length` -> `ContextWindow`
- OpenRouter preference for `top_provider.context_length`
