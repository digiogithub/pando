# ACP phase 2 Claude compatibility implementation

Date: 2026-05-28
Project: pando

Implemented phase 2 ACP compatibility changes in Pando to improve informational/status message behaviour for ACP clients that already render claude-agent-acp well.

## Changes

### 1. Centralized system message normalization
Added `normalizeSystemMessage(...)` in `internal/mesnada/acp/agent.go` so ACP-visible informational events are normalized in one place.

### 2. Informational/status message handling in the ACP streaming path
In `internal/mesnada/acp/prompt_handler.go`, `AgentEventTypeSystemMessage` now passes through the normalizer before sending ACP updates.

### 3. Current normalization rules
- Persona-selection system messages are suppressed from ACP chat output.
- Compaction start messages are normalized to `Compacting...`.
- Compaction completion messages are normalized to `\n\nCompacting completed.` and emit a `usage_update` first when possible.
- Trimmed-history fallback warnings are normalized to `Context compaction failed; continuing with trimmed history.`
- Fallback-model retry messages are forwarded as normalized plain text.
- Other system messages fall back to trimmed plain text and are emitted as `agent_message_chunk`.

## Rationale
This keeps Pando closer to claude-agent-acp’s visible behaviour while preserving useful Pando-specific operational messages when they should still be shown to ACP clients.

## Verification
Ran:
- `go test ./internal/mesnada/acp ./internal/llm/agent ./internal/api`

All passed.

## Notes
There is an existing untracked `sdk/` directory in the repo working tree unrelated to this work.