# ACP thinking Phase 1 note

Task: `task-796c14d9`

## Decision

- ACP grouped thinking streaming is implemented in two narrow pieces:
  1. `internal/llm/agent/agent.go` now forwards `ThinkingDelta` events onto the ACP-facing event channel.
  2. `internal/mesnada/acp/prompt_handler.go` buffers those deltas and emits grouped thought chunks instead of one ACP update per raw delta.

## Grouping behavior

- Grouped thinking flushes when either threshold is reached:
  - `350ms` since the first pending delta or last grouped flush
  - `450` buffered characters
- Pending thinking is also force-flushed before major visible events:
  - final response
  - tool call
  - tool result
  - system/status message
  - summarize update
  - error

## Important nuance

- `sentThinkingDeltas` flips to true only after a grouped ACP thought update is actually sent.
- That preserves the existing anti-duplication behavior: once grouped thought chunks were streamed, the final full reasoning blob is skipped in `processAgentResponse(...)`.
- The grouped buffer is only cleared after a successful ACP send, so a transient send failure does not silently discard pending reasoning text.
