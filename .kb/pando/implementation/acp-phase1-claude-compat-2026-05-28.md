# ACP phase 1 Claude compatibility implementation

Date: 2026-05-28
Project: pando

Implemented phase 1 ACP compatibility changes in Pando to better match claude-agent-acp client-visible behavior.

## Changes

### 1. Compaction system messages mapped to Claude-style ACP output
In `internal/mesnada/acp/prompt_handler.go`, `AgentEventTypeSystemMessage` is now translated into ACP `agent_message_chunk` updates with Claude-compatible wording:
- messages indicating compaction start are normalized to `Compacting...`
- messages indicating compaction completion are normalized to `\n\nCompacting completed.`

### 2. Usage update emitted on compaction completion
When a compaction-completed system message is observed, Pando now emits an ACP `usage_update` before the completion text message. This uses the current session token snapshot and defaults the context window to 200000 when unknown.

### 3. Shared usage snapshot helper
In `internal/mesnada/acp/agent.go`, added `currentUsageSnapshot(...)` so both normal prompt completion and compaction-complete events use the same usage calculation logic.

### 4. Plan compatibility preserved
Existing TodoWrite -> ACP `plan` snapshot behavior was confirmed and kept intact in:
- streaming path (`processAgentEventStream`)
- assembled response path (`processAgentResponse`)
- session history replay (`streamSessionHistory`)

This means clients that render Claude ACP plans should already render Pando plans consistently when TodoWrite is used.

## Verification
Ran:
- `go test ./internal/mesnada/acp ./internal/llm/agent ./internal/api`

All passed.

## Notes
There is an untracked `sdk/` directory in the repo working tree unrelated to this implementation and not modified by this work.