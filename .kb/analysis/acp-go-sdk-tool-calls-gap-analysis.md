# Analysis: Tool Calls in Agent Client Protocol — Go SDK Implementation vs Official Specification (Rust)

## Executive Summary

The Go SDK (`acp-go-sdk`) is a faithful but incomplete reimplementation of the **Agent Client Protocol (ACP)**, based on the auto-generated JSON schema `0.12.0` from the official specification. It supports the complete tool call lifecycle (creation, updates, content types, states) but has several significant gaps compared to the official Rust SDK (`agent-client-protocol` v0.11.1).

## ACP Protocol Architecture for Tool Calls

### Communication flow
```
Client                           Agent
  |                                |
  |--- session/prompt ------------>|
  |                                | (LLM decides to execute tool)
  |<--- session/update (tool_call)-|  ← notification: new tool call
  |                                |
  |<--- session/request_permission-|  ← request: ask user for permission
  |--- response (outcome) ------->|
  |                                | (tool execution)
  |<--- session/update (update) ---|  ← notification: progress/result
  |                                |
  |<--- session/prompt response ---|  ← final response with StopReason
```

### Tool call content types (ToolCallContent)

The protocol defines **3 content variants**:

1. **Content** (`"type": "content"`): Standard content blocks (text, images, resources) — compatible with MCP
2. **Diff** (`"type": "diff"`): File change representation (`path`, `oldText`, `newText`)
3. **Terminal** (`"type": "terminal"`): Live terminal embedding by `terminalId`

### Tool call states (ToolCallStatus)

| State | Description |
|--------|-------------|
| `pending` | Has not started (waiting for input or approval) |
| `in_progress` | Executing |
| `completed` | Successfully completed |
| `failed` | Failed with error |

### Tool kinds (ToolKind)

`read`, `edit`, `delete`, `move`, `search`, `execute`, `think`, `fetch`, `switch_mode`, `other`

These types help the client choose icons and optimize the display.

### Permission system

The agent can request permission from the user before executing a tool call via `session/request_permission`. Options include: `allow_once`, `allow_always`, `reject_once`, `reject_always`. The client can auto-approve/reject based on user configuration.

## Go SDK Analysis (acp-go-sdk)

### What IS correctly implemented

| Feature | Status |
|---------|--------|
| `SessionUpdateToolCall` and `SessionToolCallUpdate` types | ✅ Complete |
| Enums: `ToolKind`, `ToolCallStatus` | ✅ Complete (includes `switch_mode`) |
| `ToolCallContent` with 3 variants (content, diff, terminal) | ✅ Complete |
| `ToolCallLocation` with optional `path` and `line` | ✅ Complete |
| `RequestPermissionRequest/Response` with options and outcomes | ✅ Complete |
| Helpers: `StartToolCall()`, `UpdateToolCall()`, `ToolContent()`, etc. | ✅ Rich builder set |
| `StartToolCallStreaming()` helper | ✅ Specific for streaming |
| Extension system (`_` prefixed methods) | ✅ Complete |
| Custom JSON marshal/unmarshal for `SessionUpdate` (discriminator `sessionUpdate`) | ✅ Robust manual implementation |
| `ToolCall` as complete struct (not just update) | ✅ Complete |
| `Plan` and `PlanEntry` for execution plans | ✅ Complete |

### Detected gaps and deficiencies

#### 1. Missing MCP Proxy Protocol types
The Rust SDK includes a `proxy_protocol` module with types for MCP tunneling over ACP:

| Type | Go SDK | Rust SDK |
|------|--------|----------|
| `McpConnectRequest/Response` | ❌ Missing | ✅ |
| `McpDisconnectNotification` | ❌ Missing | ✅ |
| `McpOverAcpMessage` | ❌ Missing | ✅ |
| `SuccessorMessage` | ❌ Missing | ✅ |
| `InitializeProxyRequest` | ❌ Missing | ✅ |

**Impact**: The Go SDK cannot participate as an ACP proxy without implementing manual extension methods.

#### 2. Methods marked as "Unstable" that are already stable upstream

Several methods are in the Go SDK's `AgentExperimental` interface when in the upstream schema (`0.12.0`) they are already stable:

| Method | Go SDK | Upstream |
|--------|--------|----------|
| `session/close` | `UnstableCloseSession` | `CloseSession` (stable) |
| `session/resume` | `UnstableResumeSession` | `ResumeSession` (stable) |
| `session/fork` | `UnstableForkSession` | Not in stable spec |
| `session/set_model` | `UnstableSetSessionModel` | `SetSessionModel` (stable) |

**Impact**: Consumers use APIs marked as unstable for stable protocol features.

#### 3. ToolCallUpdateFields doesn't exist as a separate type
In the Rust SDK, `ToolCallUpdateFields` is a struct that groups optional update fields and is used as a building block. In Go it doesn't exist; the fields are inline in `SessionToolCallUpdate`.

**Impact**: Minor. It's more of a design difference than a functional deficiency.

#### 4. Missing builders for complete SessionUpdate
Although there are builders for tool calls, equivalent builders are missing for other `SessionUpdate` types:
- No `StartPlan()`, `UpdatePlan()`
- No builders for `UserMessageChunk`, `AgentMessageChunk`, `AgentThoughtChunk`
- No builder for `AvailableCommandsUpdate`
- No builder for `SessionInfoUpdate`

**Impact**: Client code must construct these structs manually without typed builder assistance.

#### 5. No `_meta` support in helpers
The helpers (`StartToolCall`, `UpdateToolCall`, etc.) accept `_meta` via `WithStartMeta`/`WithUpdateMeta`, but the content types (`ToolContent`, `ToolDiffContent`, `ToolTerminalRef`) don't expose `_meta`.

**Impact**: Minor. `_meta` is optional for extensibility.

#### 6. SessionUpdate validation is manual and verbose
The marshaling/unmarshaling of `SessionUpdate` (lines ~4700-5300 in types_gen.go) is very extensive generated code (~600 lines) to handle the `sessionUpdate` discriminator. It would be more maintainable with a dispatch table.

**Impact**: Maintainability, not functionality.

## Side-by-side comparison

### Creating a tool call

**Rust SDK:**
```rust
conn.session_update(SessionUpdate::ToolCall {
    tool_call_id: ToolCallId::from("call_1"),
    title: "Reading file".to_string(),
    kind: Some(ToolKind::Read),
    status: ToolCallStatus::Pending,
    ..Default::default()
}).await?;
```

**Go SDK:**
```go
conn.SessionUpdate(ctx, SessionNotification{
    SessionId: "sess_1",
    Update: StartToolCall(
        "call_1",
        "Reading file",
        WithStartKind(ToolKindRead),
        WithStartStatus(ToolCallStatusPending),
    ),
})
```

Both SDKs are equivalent in expressiveness for this case.

### Updating a tool call

**Rust SDK:**
```rust
conn.session_update(SessionUpdate::ToolCallUpdate {
    tool_call_id: ToolCallId::from("call_1"),
    status: Some(ToolCallStatus::Completed),
    content: Some(vec![ToolCallContent::Content(ContentBlock::Text(...))]),
}).await?;
```

**Go SDK:**
```go
conn.SessionUpdate(ctx, SessionNotification{
    SessionId: "sess_1",
    Update: UpdateToolCall("call_1",
        WithUpdateStatus(ToolCallStatusCompleted),
        WithUpdateContent([]ToolCallContent{...}),
    ),
})
```

Equivalent.

## Recommendations

### High Priority
1. **Add MCP Proxy Protocol types**: `McpConnect`, `McpDisconnect`, `McpOverAcpMessage`, `SuccessorMessage`, `InitializeProxy`
2. **Promote methods to stable**: `session/close` and `session/resume` should exit `AgentExperimental`

### Medium Priority
3. **Add builders for other SessionUpdate types**: `StartPlan()`, `UpdatePlan()`, builders for message chunks
4. **Add `_meta` to content helpers**: `WithContentMeta()` on `ToolContent()`, `ToolDiffContent()`, `ToolTerminalRef()`

### Low Priority
5. **Refactor the SessionUpdate dispatcher**: Replace the giant switch with a dispatch table for maintainability
6. **Add `ToolCallUpdateFields` as a separate type**: For consistency with the Rust SDK

## Conclusion

The Go ACP SDK is functionally **correct and usable** for the complete tool call lifecycle. The main deficiencies are:
- Missing types for MCP Proxy Protocol (blocks use as ACP proxy)
- Several stable methods are marked as `Unstable`
- Missing builders for SessionUpdate types not related to tool calls

The type mapping, JSON serialization, and tool call model (creation, update, content, permissions, states) are correctly implemented and compatible with the official specification.
