---
created_at: 2026-08-16T22:07:52.506955038Z
updated_at: 2026-08-16T22:07:52.506955038Z
---
# Fix: Issue #5 — "Error no auto compact in ACP mode"

Implemented the minimal fixes identified in [[acp_no_autocompact_issue5]] (analysis doc).

## Changes

1. `internal/llm/agent/agent.go` — `shouldCompact` (line ~2033): now enables auto-compaction when EITHER the global `cfg.AutoCompact` OR the per-agent `agentCfg.AutoCompact` is set. Previously it returned false whenever `agents.coder.AutoCompact` was false, which the generated template and default configs serialize as `false`, so compaction never fired in ACP mode (the TUI had a separate 95% net, ACP had none).

2. `internal/llm/agent/agent.go` — `processGeneration` (line ~1024): the 40% `trimMessagesToContextBudget` now emits an `AgentEventTypeSystemMessage` (via `emitStatus`) when it actually drops messages, so ACP/WebUI surfaces see "Message history trimmed (N messages dropped)" instead of a silent history loss.

3. `internal/mesnada/acp/prompt_handler.go` — `mapFinishReasonToStopReason`: added a case mapping `message.FinishReasonError` → `acpsdk.StopReasonRefusal`. Previously errors fell into the `default` `StopReasonEndTurn`, masking failed turns as clean completions. (`StopReasonError` does not exist in acp-go-sdk v0.15.1; `StopReasonRefusal` is the available non-end_turn signal.)

4. `internal/llm/tools/cache_registry.go` — added idempotent `EnsureSessionCache(sessionID)` that returns the existing cache without wiping it (unlike `RegisterSessionCache`, which always overwrites).

5. `internal/session/session.go` — `Get` now calls `EnsureSessionCache(session.ID)`, so tool-response pagination works for loaded/resumed sessions (ACP `LoadSession`/`ResumeSession` path), not only newly created ones.

6. `internal/llm/tools/cache_interceptor.go` — removed `"diagnostics": true` from `cacheBypassTools` so large LSP diagnostics output can be auto-cached like other large tool responses.

## Verification

- `go build` of affected packages: clean.
- `go test ./internal/llm/agent ./internal/session/... ./internal/llm/tools/...` — all pass.
- `go test ./internal/mesnada/acp/... ./internal/config/...` — all pass.
- `gofmt -l` on all edited files: no output (properly formatted).