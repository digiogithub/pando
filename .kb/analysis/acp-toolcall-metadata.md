# Analysis: How ACP Wraps Tool Calls and the Problem in Pando

## How it works in acp-go-sdk (the ACP standard)

### ToolCall Structure in types_gen.go (line 5633)
```go
type ToolCall struct {
    Meta      map[string]any    `json:"_meta,omitempty"`
    Content   []ToolCallContent `json:"content,omitempty"`
    Kind      ToolKind          `json:"kind,omitempty"`
    Locations []ToolCallLocation `json:"locations,omitempty"`
    RawInput  any               `json:"rawInput,omitempty"`   // ← raw input parameters
    RawOutput any               `json:"rawOutput,omitempty"`
    Status    ToolCallStatus    `json:"status,omitempty"`
    Title     string            `json:"title"`                // ← readable title
    ToolCallId ToolCallId       `json:"toolCallId"`
}
```

### SessionUpdateToolCall (start, line 4581)
Same as ToolCall but with additional `SessionUpdate string`.

### Helpers.go flow:

1. **`StartToolCall(id, title, opts...)`** — Creates `SessionUpdate{ToolCall: &tc}` with options:
   - `WithStartKind(kind)` → read/edit/execute/search...
   - `WithStartStatus(status)` → pending/in_progress/completed/failed
   - `WithStartRawInput(rawInput)` → JSON parameters parsed as object
   - `WithStartContent(content)` → diffs, text blocks, terminal refs
   - `WithStartLocations(locations)` → array of {path}

2. **`UpdateToolCall(id, opts...)`** — Creates `SessionUpdate{ToolCallUpdate: &tu}` with options:
   - `WithUpdateStatus(status)`
   - `WithUpdateTitle(title)`
   - `WithUpdateKind(kind)`
   - `WithUpdateRawInput(rawInput)`
   - `WithUpdateRawOutput(rawOutput)`
   - `WithUpdateContent(content)`

### `toolDisplayTitle()` in tool_render.go (line 78)
Derives a readable title from rawInput:
- **bash**: extracts `rawInput["command"]` → shows the actual command
- **view/read**: extracts `rawInput["file_path"]` → "Read <path>"
- **write**: extracts `rawInput["file_path"]` → "Write <path>"
- **edit**: extracts `rawInput["file_path"]` → "Edit <path>"
- **agent**: extracts first chars of `rawInput["prompt"]`
- **glob/grep**: extracts `rawInput["path"]` and `rawInput["pattern"]`

### `parseJSONInput()` (line 15)
Converts JSON string to `map[string]interface{}`. If it fails, returns the original string.
If the string is empty, returns `map[string]interface{}{}`.

## How Pando implements it (server-side ACP)

### Key files:
- `internal/mesnada/acp/prompt_handler.go` — streaming and non-streaming tool calls
- `internal/mesnada/acp/session_state.go` — history replay
- `internal/mesnada/acp/tool_render.go` — render utilities

### Streaming flow (prompt_handler.go ~line 132):
```
AgentEventTypeToolCall → tc.Name, tc.Input, tc.Finished
  ↓
kind = mapToolKind(tc.Name)
rawInput = parseJSONInput(tc.Input)    // ← if tc.Input is empty → map[]{}
title = toolDisplayTitle(tc.Name, rawInput, workDir)
  ↓
StartToolCall(id, title, WithStartRawInput(rawInput), ...)
```

### Non-streaming flow (prompt_handler.go ~line 533):
```
msg.ToolCalls() → toolCall.Name, toolCall.Input
  ↓
rawInput = parseJSONInput(toolCall.Input)  // here Input has the complete JSON
title = toolDisplayTitle(toolCall.Name, rawInput, workDir)
  ↓
StartToolCall(id, title, WithStartRawInput(rawInput), ...)
```

## The problem: streaming path with rawInput={} and title="bash"

### Root cause

In `agent.go` lines 844-869, the event flow is:

1. **EventToolUseStart** (line 844): `event.ToolCall` has `Input: ""` (empty, only the name is known)
   → Publishes `AgentEventTypeToolCall` with `ToolCall.Input = ""` and `ToolCall.Finished = false`
   → `prompt_handler.go` receives this, calls `parseJSONInput("")` → `map[string]interface{}{}`
   → `toolDisplayTitle("bash", map[]{}, workDir)`: doesn't find "command" in the map → returns "bash"
   → **`StartToolCall` is sent with `rawInput: {}` and `title: "bash"`**

2. **EventToolUseDelta** (line 853): `assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)`
   → Only accumulates input, doesn't publish event to frontend

3. **EventToolUseStop** (line 856): Publishes `AgentEventTypeToolCall` with the complete ToolCall (Finished=true, Input=complete JSON)
   → `prompt_handler.go` receives this with `tc.Finished = true`
   → Now `rawInput` has the real data
   → Sends an **UpdateToolCall** with correct `WithStartRawInput(rawInput)` and correct title

### The failure

When the ACP client receives the first `StartToolCall` with `title="bash"` and `rawInput={}`, it displays that. Then the corrective `UpdateToolCall` arrives, **but the client may or may not update the UI** depending on its implementation. Some clients (Zed, VS Code) show the initial `title` and don't replace it with the update's title.

### Possible solution

In `prompt_handler.go`, in the streaming path (`AgentEventTypeToolCall`), when `tc.Finished = true`:

1. Send `StartToolCall` with status `in_progress` (instead of `pending`)
2. **Ensure the corrective parameters include `WithUpdateRawInput(rawInput)`** with the real data
3. Always include `WithUpdateTitle(title)` in the update so the client updates

This is already partially implemented in prompt_handler.go lines 203-231:
```go
if !started {
    sendStart(acpsdk.ToolCallStatusInProgress)  // with rawInput={}
}
inProgressOpts := []acpsdk.ToolCallUpdateOpt{
    WithUpdateStatus(acpsdk.ToolCallStatusInProgress),
    WithUpdateKind(kind),
    WithUpdateTitle(title),       // ← real title with command
    WithUpdateRawInput(rawInput), // ← real rawInput with command
    WithUpdateContent(content),
}
```

But sometimes `ToolUseStop` is lost (buffer of 256 slots full), and then `processAgentResponse` sends the corrective. That works, but the first `StartToolCall` already showed "bash" and `rawInput: {}`.

### Alternative approach: `StartToolCallStreaming`

The SDK has `StartToolCallStreaming()` (helpers.go line 385) that sends:
```go
func StartToolCallStreaming(id, title, kind, opts...) SessionUpdate {
    // status=pending, rawInput={}
}
```

This allows the client to show the tool immediately but with a placeholder. Then when the complete input arrives, `UpdateToolCall` replaces the info.

### Concrete improvement

The main problem is that some ACP clients don't update the title when they receive a `tool_call_update`. The solution is:

1. **In the streaming start**: use the tool name as provisional title (already done)
2. **In the streaming stop**: if the command is bash, use `toolDisplayTitle()` to set the actual command, and send `WithUpdateTitle(command)`
3. **In `session_state.go` (history replay)**: same as prompt_handler, ensure rawInput carries the command
