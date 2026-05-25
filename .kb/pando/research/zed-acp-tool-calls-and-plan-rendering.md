# Zed ACP tool call inputs and plan rendering

## Summary
Zed displays ACP tool activity and planning information based on `session/update` notifications sent by the agent. For tool call inputs, the agent must include the tool parameters in the `rawInput` field of `tool_call` and/or `tool_call_update` updates. For plans, the agent must send `session/update` notifications with `sessionUpdate: "plan"` and a full `entries` array on every update.

## Key protocol details

### Tool calls
ACP Tool Calls documentation defines the following fields on tool call updates:
- `toolCallId`: unique identifier for the tool call
- `title`: human-readable label
- `kind`: tool category (`read`, `edit`, `execute`, etc.)
- `status`: `pending`, `in_progress`, `completed`, `failed`
- `content`: streamed content blocks
- `locations`: related file locations
- `rawInput`: raw input parameters sent to the tool
- `rawOutput`: raw output returned by the tool

Practical implication: for Zed to be able to show the actual arguments used for a tool invocation, the ACP agent must populate `rawInput` with structured data.

Example:
```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "sess_abc123",
    "update": {
      "sessionUpdate": "tool_call",
      "toolCallId": "call_001",
      "title": "bash",
      "kind": "execute",
      "status": "pending",
      "rawInput": {
        "command": "uv run ty check",
        "description": "Typecheck with mypy"
      }
    }
  }
}
```

### Tool call updates
ACP permits `tool_call_update` notifications during execution. All fields except `toolCallId` are optional in updates, so agents may repeat `rawInput` and should include status/content/output/location changes as they become available.

### Agent plans
ACP Agent Plan documentation defines plan updates as `session/update` notifications with:
- `sessionUpdate: "plan"`
- `entries`: complete array of plan items

Each plan entry includes:
- `content`
- `priority`: `high`, `medium`, `low`
- `status`: `pending`, `in_progress`, `completed`

Important protocol rule: every plan update must send the full plan, not a partial patch; the client replaces the visible plan with the provided complete array.

Example:
```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "sess_abc123",
    "update": {
      "sessionUpdate": "plan",
      "entries": [
        {
          "content": "Analyze the existing codebase structure",
          "priority": "high",
          "status": "completed"
        },
        {
          "content": "Implement ACP tool call rawInput support",
          "priority": "high",
          "status": "in_progress"
        },
        {
          "content": "Verify rendering in Zed",
          "priority": "medium",
          "status": "pending"
        }
      ]
    }
  }
}
```

## Zed-specific observations
- Zed Agent Panel documentation says tool activity streams live with indicators showing which tools are used.
- External ACP agents may support only part of the first-party UI features, but tool-call and plan rendering are driven by ACP session updates.
- Public Zed discussion about showing actual terminal commands confirms `rawInput` appears in ACP logs. Example observed payload:
  - `sessionUpdate: "tool_call_update"`
  - `title: "bash"`
  - `kind: "execute"`
  - `rawInput.command: "uv run ty check"`
  - `rawInput.description: "Typecheck with mypy"`
- This suggests missing UI detail is often caused by the adapter/agent payloads or rendering decisions, not by an absence of protocol support.

## Practical compatibility requirements for Pando ACP
1. Emit `tool_call` as soon as a tool invocation is known.
2. Include `rawInput` with structured tool arguments.
3. Continue emitting `tool_call_update` updates with status/content/rawOutput and preserve or repeat `rawInput` when useful.
4. Emit `plan` updates in real time, not only at the end.
5. Send the complete plan entries list on every plan update.

## Sources consulted
- https://zed.dev/acp
- https://zed.dev/docs/ai/external-agents
- https://zed.dev/docs/ai/agent-panel
- https://zed.dev/docs/assistant/model-context-protocol
- https://agentclientprotocol.com/protocol/tool-calls
- https://agentclientprotocol.com/protocol/agent-plan
- https://github.com/zed-industries/zed/discussions/47259
