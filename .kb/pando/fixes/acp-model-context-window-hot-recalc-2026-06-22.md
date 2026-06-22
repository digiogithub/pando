---
created_at: 2026-06-22T08:06:08.722081838Z
updated_at: 2026-06-22T08:06:08.722081838Z
tags:
    - fix
    - acp
    - context-window
    - models
    - provider
    - pando
---
# Fix: ACP uses provider model context window and recalculates on hot model changes

Date: 2026-06-22

## What changed

Updated ACP context-window handling so the maximum context window used in usage calculations is resolved from the active provider model whenever possible, and recalculated immediately when the ACP client changes model.

Touched files:
- `internal/mesnada/acp/agent.go`
- `internal/mesnada/acp/session_state.go`
- `internal/mesnada/acp/agent_pando_test.go`

## Details

### ACP usage snapshot logic
- `currentUsageSnapshot()` no longer relies only on `sessionInfo.ContextWindow`.
- Added `maxContextWindowForSession()` to compute the effective **maximum model context window** for ACP usage updates.
- Resolution order now is:
  1. current ACP session selected model -> resolved in `llmmodels.SupportedModels`
  2. persisted `sessionInfo.ContextWindow`
  3. current agent default model from `AgentService`
  4. `0` if still unknown

This separates:
- **current usage in the conversation** = `PromptTokens + CompletionTokens`
- **maximum model context window** = provider/model capability used as the denominator for ACP usage display

### Hot model changes
- `setSessionModel()` now:
  - updates the ACP session model
  - re-normalizes thinking settings for the newly selected model
  - reapplies session LLM overrides immediately
  - sends a fresh `usage_update` right away so ACP clients receive the recalculated max context window without waiting for the next prompt round

### Model resolution
- `selectedACPModel()` now also tries `llmmodels.ResolveModelID()` so ACP can resolve aliases / non-canonical IDs to registered provider models before reading `ContextWindow`.

## Why

ACP must distinguish between the live token usage of the current conversation and the maximum context window supported by the selected provider model. The previous implementation could keep stale/unknown max sizes in ACP and did not force a recalculation when the model changed mid-session.

## Verification

Ran:
- `gofmt -w internal/mesnada/acp/agent.go internal/mesnada/acp/session_state.go internal/mesnada/acp/agent_pando_test.go`
- `go test ./internal/mesnada/acp ./internal/llm/models`

Added tests for:
- session model override propagation after `SetSessionModel`
- usage snapshot preferring selected model context window over persisted session value
- fallback to persisted session context window when the model cannot be resolved
