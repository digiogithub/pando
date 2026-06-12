# Plan: Match real-time ACP tool call display with session history display

## Problem

When using Pando via ACP in real-time operation mode (streaming), tool calls are displayed with incomplete metadata: `title: "bash"`, `rawInput: {}`, no `locations`, no `content`. However, when loading an ACP session history, the same tool calls are rendered with enriched data: `title: "ls -la /tmp"`, `rawInput: {"command": "ls -la /tmp"}`, with correct `locations` and `content`.

## Root Cause Analysis

### Flow 1: Real-time streaming (`processPromptWithAgent` → `prompt_handler.go`)

| LLM Event | ToolCall.Input | ACP Action | Displayed State |
|---|---|---|---|
| `EventToolUseStart` | `""` (empty) | `StartToolCall` with `rawInput: {}`, `title: "bash"` | ❌ Not enriched |
| `EventToolUseDelta` | accumulates in memory | **No event to frontend** | — |
| `EventToolUseStop` | complete JSON | `UpdateToolCall` with real data | ✅ Enriched |

**Identified problems:**
1. The initial `StartToolCall` has `rawInput: {}` and `title: "bash"` because it's sent before
   the input is available, and the ACP client renders it as-is.
2. `EventToolUseDelta` does not emit events to the ACP client, so during input accumulation
   the client doesn't see any updates.
3. If the 256-slot buffer fills up and `EventToolUseStop` is missed, the correction arrives
   later in `processAgentResponse`, but there's a visible window with incomplete data.

### Flow 2: Non-streaming (`processAgentResponse` in `prompt_handler.go` lines 521-638)

When the provider doesn't emit ToolUseStart/Stop (Copilot, OpenAI, Gemini), the code
corrects by sending `StartToolCall` with complete data. This path works well
because it already has the complete input.

### Flow 3: Session history (`session_state.go` lines 238-283)

In history replay, the `message.ToolCall` instances already have `p.Input` with the
complete JSON stored in the database:

| Field | Value in history | Why it works |
|---|---|---|
| `rawInput` | `parseJSONInput(p.Input)` → parsed JSON | Complete input from DB |
| `title` | `toolDisplayTitle(p.Name, rawInput, workDir)` | Calculated with real data |
| `locations` | `toLocations(p.Name, p.Input)` | Extracted from complete JSON |
| `content` | `toolCallContent(p.Name, rawInput)` | Extracted from complete JSON |
| `status` | `ToolCallStatusInProgress` | Correct for replay |

This path **doesn't have the timing problem** because it reads persisted data where each
tool call already has the complete input.

## Concrete differences by field

| Field | Streaming start | Streaming in_progress | History replay |
|---|---|---|---|
| `rawInput` | `{}` (empty) | updated (but only on stop, not on intermediate in_progress) | parsed JSON |
| `title` | tool name | updated with `toolDisplayTitle` | calculated with `toolDisplayTitle` |
| `kind` | ✅ correct | ✅ correct | ✅ correct |
| `status` | `in_progress` | `in_progress` | `in_progress` |
| `locations` | ❌ not sent | ✅ sent | ✅ sent |
| `content` | ✅ terminal_ref or text | ✅ sent | ✅ sent |
| `_meta` | ✅ `pando.toolName` | ✅ updated | ✅ complete |

### Specific gap in `in_progress` streaming:

In `prompt_handler.go` line 214-231, the `UpdateToolCall` in_progress does send:
- `WithUpdateStatus`, `WithUpdateKind`, `WithUpdateTitle`, `WithUpdateRawInput`, `WithUpdateContent`, `WithUpdateLocations`

But this is only sent on the **Finished** event (`tc.Finished = true`), meaning
when the tool call is complete. During accumulation there are no intermediate updates.

## Implementation Plan

### Phase 1: Emit `AgentEventTypeToolCall` with accumulated input from `EventToolUseDelta`

**File:** `internal/llm/agent/agent.go` (~line 881)

**Change:** Publish an `AgentEventTypeToolCall` event on each `EventToolUseDelta` but
with a debounce (minimum 100ms between events) to avoid saturating the channel. This allows
`prompt_handler.go` to receive input updates while accumulating and send corrective
`UpdateToolCall` to the ACP client.

Alternative (simpler): Publish an event **only once** when the first chunk
of input arrives after ToolUseStart. This avoids saturation but still provides the first
enriched update to the client.

### Phase 2: Strengthen the corrective in `processAgentResponse` with `WithUpdateLocations`

**File:** `internal/mesnada/acp/prompt_handler.go` (~lines 582-592)

The corrective block (`hadEmptyInput && toolCall.Input != ""`, lines 571-602) currently
sends: `WithUpdateKind`, `WithUpdateTitle`, `WithUpdateRawInput`.

**Add:** `WithUpdateLocations(locations)` to the corrective update. Currently `locations`
is calculated on line 576 but is NOT included in the `correctiveOpts`.

```go
correctiveOpts := []acpsdk.ToolCallUpdateOpt{
    acpsdk.WithUpdateKind(kind),
    acpsdk.WithUpdateTitle(title),
    acpsdk.WithUpdateRawInput(rawInput),
    acpsdk.WithUpdateLocations(locations),  // ← ADD
}
if len(content) > 0 {
    correctiveOpts = append(correctiveOpts, acpsdk.WithUpdateContent(content))
}
```

### Phase 3: Send `StartToolCall` with `locations` even with empty input

**File:** `internal/mesnada/acp/prompt_handler.go` (~line 168-185)

The `sendStart` callback already tries to add locations, but `toLocations(tc.Name, tc.Input)`
returns nil when `tc.Input` is empty. For tools like `view`, `read`, `edit`
the location can't be extracted without input, but for `bash` we can send the
terminal_ref in content.

**Change:** For the bash tool, ensure that `StartToolCall` always carries the
`ToolTerminalRef` in content (already done on lines 164-166). Verify this works.

### Phase 4: Ensure `rawOutput` parity across paths

**File:** `internal/mesnada/acp/prompt_handler.go` (lines 193-232)

In the streaming path, when `tc.Finished = true` and `started = true` (the `StartToolCall`
was already sent), the `inProgressOpts` update is sent. This does NOT include `rawOutput` because
there's no result yet. This is correct.

But when `tc.Finished = true` and `!started` (the StartToolCall was never sent), a synthetic
`StartToolCall` is sent (lines 205-212). This already includes `rawInput`, `kind`,
`locations`, `content`. ✅ Correct.

### Phase 5: `_meta` parity between streaming and history replay

**File:** `internal/mesnada/acp/prompt_handler.go`

Verify that all paths include `_meta` consistently:
- Streaming start: ✅ (line 161-166, injected at line 182-184)
- Streaming in_progress: ✅ (line 226-228)
- Streaming synthetic start: ✅ (line 280-299)
- Streaming tool result: ✅ (line 341-409)
- Non-streaming start: ✅ (line 612-635)
- Non-streaming result: ✅ (line 690-747)

**History replay (`session_state.go`):**
- ToolCall start: ✅ (lines 270-282)
- ToolResult: ✅ (lines 349-406)

### Phase 6: Tests

**Files:** `internal/mesnada/acp/agent_pando_test.go`

Add tests that verify:
1. The corrective `UpdateToolCall` includes `locations` for tools with paths
2. The streaming `StartToolCall` for bash includes `terminal_ref` in content
3. The non-streaming `StartToolCall` always carries complete `rawInput`
4. `_meta` parity across all paths

Compare with existing `TestToolDisplayTitle` tests for reference.

### Phase 7: Verification

1. `go test ./internal/mesnada/acp ./internal/llm/agent`
2. `go vet ./internal/mesnada/acp ./internal/llm/agent`
3. Build: `go build ./cmd/pando`
4. Manual test connecting an ACP client (Zed) and observing the display

## Files to modify

| File | Change |
|---|---|
| `internal/llm/agent/agent.go` | Phase 1: Emit event on ToolUseDelta |
| `internal/mesnada/acp/prompt_handler.go` | Phase 2: Add locations to corrective |
| `internal/mesnada/acp/agent_pando_test.go` | Phase 6: Parity tests |

## Success criteria

- [ ] Streaming real-time tool calls show `rawInput` with actual command/parameters
- [ ] The `title` shows the same enriched info as in history (e.g., "ls -la /tmp" vs "bash")
- [ ] `locations` appear on real-time file tool calls (view, read, edit)
- [ ] `_meta` is consistent between streaming and history replay
- [ ] No regression in already-working tool call behavior
- [ ] Existing tests continue to pass
