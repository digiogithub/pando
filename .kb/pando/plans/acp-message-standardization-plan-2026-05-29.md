# Plan: ACP Message Standardization in Pando (based on opencode)

> Date: 2026-05-29
> Reference source: `.kb/research/acp-opencode-message-structure.md`
> Status: PLANNED

---

## 1. Comparative analysis: opencode vs pando

### 1.1 What opencode does (reference standard)

| ACP Message | Key field | opencode behavior |
|-------------|-------------|------------------------|
| `agent_message_chunk` | `messageId` | Includes `messageId` (UUID) in each chunk to group fragments of the same message |
| `agent_thought_chunk` | `messageId` | Includes `messageId` shared with the message of the same turn |
| `user_message_chunk` | `messageId` | Emitted when receiving the user's prompt |
| `tool_call` | `rawInput` | Always structured as a JSON object (same as Pando) |
| `tool_call_update` (failed) | `rawOutput.error` | Uses `rawOutput.error` for failures, not `rawOutput.output` |
| `edit` tool | `rawOutput.metadata.filediff` | Includes `filediff` with `before`/`after`/`additions`/`deletions` |
| `bash` tool | title | Uses the actual command as title: `input.command ? input.command : "Terminal"` |
| `grep`/`glob` | `locations` | `[{ "path": input.path }]` when path is available |

**Important note:** Opencode does use `rawInput` structured as a JSON object, same as Pando. The issue in Zed mentioned ("does not show input data from a tool") suggests there are other factors affecting rendering in Zed (possibly related to lifecycle, timing, or how data is displayed in the UI).

### 1.2 Current state of Pando

**What Pando already does well:**
- ✅ `rawInput` structured as object (same as opencode)
- ✅ `rawOutput: { output, metadata }` on completions
- ✅ `tool_call` before any `tool_call_update`
- ✅ Lifecycle: `pending` → `in_progress` → `completed/failed`
- ✅ `ToolDiffContent` for `edit`/`write`
- ✅ Terminal `_meta` (terminal_info, terminal_output, terminal_exit) for bash
- ✅ `UpdatePlan` instead of `StartToolCall` for `TodoWrite`
- ✅ `sendCurrentModeUpdate("plan")` before the `UpdatePlan`
- ✅ History replay with `streamSessionHistory`
- ✅ Synthesis of `StartToolCall` when the start was lost

**Identified gaps vs opencode:**

| Gap | Severity | Affected file(s) |
|-----|-----------|------------------------|
| **G1**: `messageId` missing in `agent_message_chunk` / `agent_thought_chunk` | High | `prompt_handler.go`, `session_state.go` |
| **G2**: `user_message_chunk` not emitted when receiving prompt | High | `prompt_handler.go` |
| **G3**: `rawOutput.error` not used in bash/tool failures; always uses `output` | Medium | `prompt_handler.go`, `session_state.go` |
| **G4**: `rawOutput.metadata.filediff` missing in completed `edit` | Medium | `prompt_handler.go`, `session_state.go` |
| **G5**: Bash title uses generic tool name, not the command | Medium | `tool_render.go` |
| **G6**: Terminal metadata not capability-gated consistently in replay | Low | `session_state.go` |
| **G7**: `user_message_chunk` not emitted in history replay | Low | `session_state.go` |
| **G8**: `agent_message_chunk`/`agent_thought_chunk` lack `messageId` in replay | Low | `session_state.go` |

---

## 2. Detailed description of each gap

### G1: messageId missing in agent_message_chunk / agent_thought_chunk

**Opencode behavior:**
```json
{
  "sessionUpdate": "agent_message_chunk",
  "messageId": "msg_asst_001",
  "content": { "type": "text", "text": "..." }
}
```

**Current Pando:**
```go
acpsdk.UpdateAgentMessageText(event.Delta)  // without messageId
```

The ACP SDK Go (`SessionUpdateAgentMessageChunk`) has field `MessageId *string` (UNSTABLE but implemented). Without it, clients that group chunks by messageId (like Zed) may have problems joining fragments from different messages.

**Fix:** Use `msg.ID` (from `message.Message`) as `messageId` when emitting content chunks in streaming mode. Create helper `UpdateAgentMessageTextWithID(text, msgID string)`.

### G2: user_message_chunk not emitted when receiving prompt

**Opencode behavior:** When the user's prompt arrives, it emits `user_message_chunk` before processing.

**Current Pando:** The user's prompt is processed directly without emitting `user_message_chunk`. In `streamSessionHistory` user messages are replayed, but in live mode this update is not emitted when receiving the prompt.

**Fix:** In `HandlePrompt` / `processPromptWithAgent`, before calling the agent, emit `acpsdk.UpdateUserMessageText(promptText)` with the `messageId` of the current session.

### G3: rawOutput.error on failures

**Opencode behavior (failed):**
```json
{
  "rawOutput": {
    "error": "Command 'nonexistent' not found.",
    "metadata": {}
  }
}
```

**Current Pando:**
```go
rawOutput := map[string]interface{}{
    "output": tr.Content,  // always "output", even on errors
}
```

**Fix:** When `tr.IsError == true`, use `"error"` as key instead of `"output"` to align with the opencode standard.

### G4: rawOutput.metadata.filediff on completed edit

**Opencode behavior:**
```json
{
  "rawOutput": {
    "output": "File edited successfully",
    "metadata": {
      "filediff": {
        "file": "src/component.tsx",
        "before": "  gap: 12px",
        "after": "  gap: 18px",
        "additions": 1,
        "deletions": 1
      }
    }
  }
}
```

**Current Pando:** Only uses `ToolDiffContent` in the `content` field, but `rawOutput.metadata` does not contain the `filediff`. The visual diff exists but not in the standard rawOutput format.

**Fix:** Add `rawOutput.metadata.filediff` with `{ file, before, after, additions, deletions }` for `edit` tools. For `write`, can include `{ file, additions: linesCount }`.

### G5: Bash title uses generic name

**Opencode / claude-agent-acp behavior:**
```
title: "bun test --filter session"  // the actual command
```

**Current Pando (`tool_render.go:toolDisplayTitle`):**
```
title: "bash"  // or "Bash" — generic title
```

**Fix:** In `toolDisplayTitle` for bash tools, use the `command` field from rawInput as title when available and sufficiently short (e.g., max 80 chars).

### G6 & G7: Replay - user_message_chunk and terminal capability gating

**G6:** In `streamSessionHistory`, user messages (role=User) already emit `UpdateUserMessageText`, but the `messageId` field is not populated.

**G7:** Terminal metadata in replay has no consistent capability gating mechanism (the field `a.terminalOutputEnabled()` does exist, but must be verified that it also works in the replay context).

### G8: messageId in replay

In `streamSessionHistory`, `UpdateAgentMessageText` and `UpdateAgentThoughtText` do not include `messageId`. They must use `msg.ID` as `messageId`.

---

## 3. Implementation plan by phases

### Phase 1 — High priority: messageId + user_message_chunk (basic standard)

**Files:** `prompt_handler.go`, `session_state.go`, possibly new helper in `tool_render.go`

#### 1a. Add messageId to agent_message_chunk and agent_thought_chunk

In `processAgentEventStream`:
```go
// In AgentEventTypeContentDelta:
update := SessionUpdate{AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
    Content:   acpsdk.TextBlock(event.Delta),
    MessageId: &currentMessageID,  // new field
}}
```

Where `currentMessageID` is updated in `AgentEventTypeResponse` with `event.Message.ID`.

In `processAgentResponse`:
```go
// Pass msgID when sending complete content/reasoning
update := SessionUpdate{AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
    Content:   acpsdk.TextBlock(content.String()),
    MessageId: &msg.ID,
}}
```

#### 1b. Emit user_message_chunk when receiving prompt

In `HandlePrompt` or `processPromptWithAgent`, before calling the agent:
```go
userMsgID := generateMessageID()  // or retrieve from pando session
acpSession.SendUpdate(SessionUpdate{UserMessageChunk: &acpsdk.SessionUpdateUserMessageChunk{
    Content:   acpsdk.TextBlock(promptText),
    MessageId: &userMsgID,
}})
```

#### 1c. messageId in streamSessionHistory

For each user/assistant message in the replay, use `msg.ID` as `MessageId`.

---

### Phase 2 — Medium priority: rawOutput.error + filediff

**Files:** `prompt_handler.go`, `session_state.go`

#### 2a. rawOutput key error vs output

Create helper:
```go
func buildRawOutput(content string, metadata string, isError bool) map[string]interface{} {
    key := "output"
    if isError {
        key = "error"
    }
    out := map[string]interface{}{key: content}
    if metadata != "" {
        var m interface{}
        if json.Unmarshal([]byte(metadata), &m) == nil {
            out["metadata"] = m
        } else {
            out["metadata"] = metadata
        }
    }
    return out
}
```

Replace the two duplicated `rawOutput` blocks in `processAgentEventStream` and `processAgentResponse`.

#### 2b. rawOutput.metadata.filediff for edit

In the `ToolResult` section for edit tools:
```go
if isEditTool(tr.Name) && !tr.IsError && storedInput != "" {
    var ep editToolInput
    if json.Unmarshal([]byte(storedInput), &ep) == nil && ep.FilePath != "" {
        if tr.Name == "edit" {
            // Count lines for additions/deletions
            rawOutput["metadata"] = map[string]interface{}{
                "filediff": map[string]interface{}{
                    "file":      ep.FilePath,
                    "before":    ep.OldString,
                    "after":     ep.NewString,
                    "additions": countLines(ep.NewString),
                    "deletions": countLines(ep.OldString),
                },
            }
        }
    }
}
```

---

### Phase 3 — Medium priority: bash title + title correction

**Files:** `tool_render.go`

#### 3a. Bash title as actual command

In `toolDisplayTitle`:
```go
case "bash", "execute_command":
    if m, ok := rawInput.(map[string]interface{}); ok {
        if cmd, ok := m["command"].(string); ok && cmd != "" {
            if len(cmd) > 80 {
                cmd = cmd[:77] + "..."
            }
            return cmd
        }
    }
    return "Terminal"
```

---

### Phase 4 — Low priority: Live/replay unification + tests

**Files:** `prompt_handler.go`, `session_state.go`, `agent_pando_test.go`

#### 4a. Extract shared helper for rawOutput

Factor `buildRawOutput` into `tool_render.go` so that both `prompt_handler.go` and `session_state.go` use it.

#### 4b. ACP lifecycle tests with messageId

Add tests in `agent_pando_test.go`:
- `TestAgentMessageChunkHasMessageId`: verifies that each chunk has `messageId`
- `TestUserMessageChunkEmittedOnPrompt`: verifies that `user_message_chunk` is emitted
- `TestRawOutputErrorKeyOnFailure`: verifies `rawOutput.error` vs `rawOutput.output`
- `TestEditToolRawOutputFilediff`: verifies `rawOutput.metadata.filediff`
- `TestBashTitleUsesCommand`: verifies that bash title uses the command

---

## 4. Summary of changes by file

| File | Changes |
|---------|---------|
| `internal/mesnada/acp/prompt_handler.go` | G1 (messageId in streaming), G2 (user_message_chunk), G3 (rawOutput error key) |
| `internal/mesnada/acp/session_state.go` | G1 (messageId in replay), G7 (terminal gating replay), G8 (messageId replay) |
| `internal/mesnada/acp/tool_render.go` | G3 (buildRawOutput helper), G4 (filediff), G5 (bash title) |
| `internal/mesnada/acp/agent_pando_test.go` | Tests for G1, G2, G3, G4, G5 |

---

## 5. Recommended execution order

```
Phase 1 → Phase 2 → Phase 3 → Phase 4
```

Each phase is independent and deployable separately. Phase 1 has the greatest compatibility impact with standard ACP clients (Zed, etc.).

---

## 6. ACP compliance checklist post-implementation

- [ ] `agent_message_chunk` includes `messageId` (message UUID)
- [ ] `agent_thought_chunk` includes shared `messageId`
- [ ] `user_message_chunk` emitted when receiving prompt (live)
- [ ] `user_message_chunk` emitted in history replay
- [ ] `rawOutput.error` used when `isError == true`
- [ ] `rawOutput.metadata.filediff` present for `edit` tool
- [ ] Bash title uses actual command when available
- [ ] All changes have tests in `agent_pando_test.go`
- [ ] Live and replay produce the same payload shape (parity)

---

## 7. References

- Source document: `.kb/research/acp-opencode-message-structure.md`
- Previous analysis: `.kb/pando/analysis/acp-tool-call-compatibility-improvements-from-opencode-and-claude-agent-acp-2026-05-25.md`
- ACP SDK Go: `/www/MCP/Pando/acp-go-sdk/helpers.go`, `types_gen.go`
- Current implementation: `internal/mesnada/acp/prompt_handler.go`, `session_state.go`, `tool_render.go`