---
created_at: 2026-10-29T16:05:27.420306543Z
updated_at: 2026-10-29T16:05:27.420306543Z
tags:
    - fix
    - agent
    - max_tokens
    - copilot
    - autocompact
---
# Coder output budget from model limit, max_tokens continuation, always-on auto-compact (2026-10-29)

Fixes sessions that "froze" while saving a plan: see [[2026-10-29-session-stops-saving-plan-max-tokens]].

## Changes
1. **Coder Auto output budget** (`internal/config/config.go`)
   - New `coderAutoBudget(model)`: when `MaxTokens = 0`, coder uses the model's reported output limit (`model.DefaultMaxTokens`, e.g. Copilot `capabilities.limits.max_output_tokens`), capped at `CoderAutoMaxTokensCap = 32_000` and at a quarter of the context window, never below the role default (8192). Models without a reported limit keep 8192 (still clamped to the model limit).
   - Coder ceiling `coderMaxOutputTokensCeiling = 64_000` (was 16384) for manual overrides.
   - Mirrors the VS Code Copilot extension, which sends `max_tokens = maxOutputTokens` from model limits.
   - TUI hint (`agentTokensHint`) and init templates (`internal/config/init.go`, `cmd/init.go`) updated.
2. **max_tokens continuation** (`internal/llm/agent/agent.go`, processGeneration loop)
   - When a response finishes with `FinishReasonMaxTokens`, the agent emits a visible system notice, persists a user turn from `outputLimitContinuationPrompt(limit)` (says the cut tool call was discarded and NOT executed, asks to split large writes) and continues. Bounded by `maxOutputLimitContinuations = 2` per run; after that a final "stopped" notice is shown instead of ending silently.
3. **Coder auto-compact always on**
   - `config.ResolveAutoCompact(name, global, agent)` now takes the agent name and returns true for `AgentCoder`. Callers updated: `agent.shouldCompact` (also works when the coder has no explicit config entry), `internal/tui/tui.go`, `internal/api/handlers_config.go` (PUT ignores the coder's autoCompact payload).
   - Per-agent Auto-compact toggle removed from TUI settings (`internal/tui/page/settings.go`) and WebUI (`web-ui/src/components/settings/AgentsSettings.tsx`); Compact threshold stays. Global `AutoCompact` still applies to other agents.

## Verification
- New tests: `TestResolveAgentMaxTokensCoderAuto`, updated `TestResolveAutoCompactPrecedence` (`internal/config/agent_persistence_test.go`); `TestOutputLimitContinuesTurn`, `TestOutputLimitContinuationIsBounded` (`internal/llm/agent/output_limit_continuation_test.go`).
- `go build ./...`; `go test ./internal/config ./internal/llm/... ./internal/api ./internal/tui/... ./cmd/...` all ok; web-ui vitest 133 passed, `bun run build` ok.
