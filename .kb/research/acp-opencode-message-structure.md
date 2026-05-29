# ACP (Agent Communication Protocol) — Message Structure in sst/opencode

> Source: DeepWiki analysis of https://github.com/sst/opencode  
> Date: 2026-05-29  
> Relevant paths: `packages/opencode/src/acp/`, `packages/opencode/src/session/message.ts`, `packages/opencode/src/session/message-v2.ts`, `@agentclientprotocol/sdk`

---

## 1. Overview

The `opencode acp` command starts an ACP server that communicates over **stdin/stdout using NDJSON**. The core implementation lives in `packages/opencode/src/acp/` and the `ACPAgent` class handles the conversion of internal message parts to ACP messages.

---

## 2. Protocol Lifecycle (Message Flow)

```
Client (e.g. Zed editor)          opencode acp server
        |                                 |
        |--- initialize request --------->|
        |<-- initialize response ---------|  (protocol version + capabilities)
        |                                 |
        |--- session/new (or load/resume)->|
        |<-- session response ------------|
        |                                 |
        |--- session/prompt (user text) ->|
        |                                 |
        |<-- sessionUpdate: plan ----------|  (todowrite output)
        |<-- sessionUpdate: tool_call_update (in_progress) --|
        |<-- sessionUpdate: tool_call_update (completed/failed) --|
        |<-- sessionUpdate: agent_thought_chunk --|
        |<-- sessionUpdate: agent_message_chunk --|
        |<-- sessionUpdate: user_message_chunk ---|
        |                                 |
        |       [agent finishes, session idle]
```

---

## 3. All sessionUpdate Message Types

| `sessionUpdate` value      | Description |
|---------------------------|-------------|
| `plan`                    | Plan entries update (from `todowrite` tool) |
| `tool_call_update`        | Tool execution status (pending/in_progress/completed/failed) |
| `tool_call`               | Initial notification of a tool call |
| `agent_message_chunk`     | Streaming text chunk from assistant message |
| `agent_thought_chunk`     | Streaming reasoning/thought chunk from agent |
| `user_message_chunk`      | Chunk from user message (text or file) |

---

## 4. Detailed Message Type JSON Examples

### 4.1 `plan` — Plan update (from TodoWrite tool)

Triggered when `todowrite` tool completes; contains an array of `PlanEntry` objects.

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "plan",
    "entries": [
      {
        "priority": "high",
        "status": "completed",
        "content": "Analyze the request and understand existing component structure."
      },
      {
        "priority": "medium",
        "status": "in_progress",
        "content": "Implement the new feature in small increments."
      },
      {
        "priority": "low",
        "status": "pending",
        "content": "Write tests and update documentation."
      }
    ]
  }
}
```

**`PlanEntry` fields:**
- `priority`: `"high"` | `"medium"` | `"low"`
- `status`: `"completed"` | `"in_progress"` | `"pending"`
- `content`: string description of the plan item

---

### 4.2 `tool_call_update` — Tool execution update

#### Common structure

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_xyz_001",
    "status": "in_progress",       // "pending" | "in_progress" | "completed" | "failed"
    "kind": "bash",                // tool type identifier
    "title": "Run session tests",  // human-readable title
    "locations": [                 // relevant file paths (empty for bash)
      { "path": "/path/to/file.ts" }
    ],
    "rawInput": { ... },           // tool-specific input (see per-tool examples below)
    "content": [                   // optional array of output chunks
      {
        "type": "content",
        "content": {
          "type": "text",
          "text": "Running..."
        }
      }
    ],
    "rawOutput": { ... }           // present when status = "completed" or "failed"
  }
}
```

**`locations` field:**
- Populated by `toLocations(tool, input)` function
- `read`, `edit`, `write` → `[{ "path": input.filePath }]`
- `glob`, `grep` → `[{ "path": input.path }]`
- `bash` (ShellID.ToolID) → `[]` (empty array)

---

### 4.3 `bash` tool — tool_call_update examples

**Input fields:** `command` (string), `description` (optional string)

**In progress:**
```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_bash_001",
    "status": "in_progress",
    "kind": "bash",
    "title": "Run session tests",
    "locations": [],
    "rawInput": {
      "command": "bun test --filter session",
      "description": "Run session tests"
    },
    "content": [
      {
        "type": "content",
        "content": {
          "type": "text",
          "text": "Running bun test --filter session..."
        }
      }
    ]
  }
}
```

**Completed:**
```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_bash_001",
    "status": "completed",
    "kind": "bash",
    "title": "Run session tests",
    "locations": [],
    "rawInput": {
      "command": "bun test --filter session",
      "description": "Run session tests"
    },
    "rawOutput": {
      "output": "bun test v1.3.14\n\n✓ session-turn.test.tsx (3 tests) 45ms\n✓ message-part.test.tsx (7 tests) 120ms\n\nTests: 10 passed\nTime: 0.89s",
      "metadata": {
        "command": "bun test --filter session"
      }
    }
  }
}
```

**Failed:**
```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_bash_002",
    "status": "failed",
    "kind": "bash",
    "title": "Run failing command",
    "locations": [],
    "rawInput": {
      "command": "nonexistent-command"
    },
    "content": [
      {
        "type": "content",
        "content": {
          "type": "text",
          "text": "Command 'nonexistent-command' not found."
        }
      }
    ],
    "rawOutput": {
      "error": "Command 'nonexistent-command' not found.",
      "metadata": {}
    }
  }
}
```

---

### 4.4 `read` tool — tool_call_update examples

**Input fields:** `filePath` (string), `offset` (optional int), `limit` (optional int)

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_read_001",
    "status": "completed",
    "kind": "read",
    "title": "Read src/components/session-turn.tsx",
    "locations": [
      { "path": "src/components/session-turn.tsx" }
    ],
    "rawInput": {
      "filePath": "src/components/session-turn.tsx",
      "offset": 1,
      "limit": 50
    },
    "rawOutput": {
      "output": "export function SessionTurn(props) {\n  // component implementation\n  return <div>...</div>\n}",
      "metadata": {}
    }
  }
}
```

**With image attachment (e.g. reading a PNG):**
```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_read_img",
    "status": "completed",
    "kind": "read",
    "title": "Read /tmp/image.png",
    "locations": [
      { "path": "/tmp/image.png" }
    ],
    "rawInput": {
      "filePath": "/tmp/image.png"
    },
    "content": [
      {
        "type": "content",
        "content": {
          "type": "text",
          "text": "Image read successfully"
        }
      },
      {
        "type": "content",
        "content": {
          "type": "image",
          "mimeType": "image/png",
          "data": "<base64encodeddata>"
        }
      }
    ],
    "rawOutput": {
      "output": "Image read successfully",
      "attachments": [
        {
          "id": "part_image",
          "sessionID": "sess_abc123",
          "messageID": "msg_image_001",
          "type": "file",
          "mime": "image/png",
          "filename": "image.png",
          "url": "data:image/png;base64,<base64encodeddata>"
        }
      ]
    }
  }
}
```

---

### 4.5 `write` tool — tool_call_update examples

**Input fields:** `filePath` (string), `content` (string — full file content)

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_write_001",
    "status": "completed",
    "kind": "write",
    "title": "Write src/utils/helpers.ts",
    "locations": [
      { "path": "src/utils/helpers.ts" }
    ],
    "rawInput": {
      "filePath": "src/utils/helpers.ts",
      "content": "export function clamp(value: number, min: number, max: number) {\n  return Math.min(Math.max(value, min), max)\n}\n"
    },
    "rawOutput": {
      "output": "File written successfully",
      "metadata": {}
    }
  }
}
```

**Note:** The write tool sends the **complete new file content** in the `content` field of `rawInput`. No diff is used here.

---

### 4.6 `edit` tool — tool_call_update examples

**Input fields:** `filePath` (string), `oldString` (string), `newString` (string)  
**Metadata:** `filediff` object with `file`, `before`, `after`, `additions`, `deletions`

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_edit_001",
    "status": "completed",
    "kind": "edit",
    "title": "Edit src/components/session-turn.tsx",
    "locations": [
      { "path": "src/components/session-turn.tsx" }
    ],
    "rawInput": {
      "filePath": "src/components/session-turn.tsx",
      "oldString": "gap: 12px",
      "newString": "gap: 18px"
    },
    "rawOutput": {
      "output": "File edited successfully",
      "metadata": {
        "filediff": {
          "file": "src/components/session-turn.tsx",
          "before": "  gap: 12px;\n  display: flex;",
          "after": "  gap: 18px;\n  display: flex;",
          "additions": 1,
          "deletions": 1
        }
      }
    }
  }
}
```

**Key:** The edited lines are communicated via:
- `rawInput.oldString` — the exact text that was replaced
- `rawInput.newString` — the exact replacement text
- `rawOutput.metadata.filediff` — contextual diff with `before`/`after` snippets and line counts

The diff is also used in the `permission.asked` event's metadata with `filepath` and `diff` fields when the agent requests permission before writing.

---

### 4.7 `grep` tool — tool_call_update examples

**Input fields:** `pattern` (string, required), `path` (optional string), `include` (optional glob string)

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call_grep_001",
    "status": "completed",
    "kind": "grep",
    "title": "Found 2 matches",
    "locations": [
      { "path": "src" }
    ],
    "rawInput": {
      "pattern": "SessionTurn",
      "path": "src",
      "include": "*.tsx"
    },
    "rawOutput": {
      "output": "src/components/session-turn.tsx:141\nsrc/pages/session/timeline.tsx:987",
      "metadata": {}
    }
  }
}
```

---

### 4.8 `agent_message_chunk` — Streaming assistant text

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "agent_message_chunk",
    "messageId": "msg_asst_001",
    "content": {
      "type": "text",
      "text": "I'll start by analyzing the component structure...",
      "annotations": {
        "audience": ["assistant"]
      }
    }
  }
}
```

---

### 4.9 `agent_thought_chunk` — Streaming agent reasoning

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "agent_thought_chunk",
    "messageId": "msg_asst_001",
    "content": {
      "type": "text",
      "text": "The user wants to refactor the clamp function. I should look at the current implementation first, then check for usages before modifying..."
    }
  }
}
```

---

### 4.10 `user_message_chunk` — User message chunk

```json
{
  "sessionId": "sess_abc123",
  "update": {
    "sessionUpdate": "user_message_chunk",
    "messageId": "msg_user_001",
    "content": {
      "type": "text",
      "text": "Please refactor the helpers.ts file to add proper error handling.",
      "annotations": {
        "audience": ["user"]
      }
    }
  }
}
```

---

## 5. Content Sub-types: resource, resource_link, image

These sub-types appear inside `agent_message_chunk` or `user_message_chunk` `content` fields when sending file-related content.

### 5.1 `resource` — Inline file content (text or binary)

**Text resource:**
```json
{
  "sessionUpdate": "agent_message_chunk",
  "messageId": "msg_asst_001",
  "content": {
    "type": "resource",
    "resource": {
      "uri": "file:///path/to/file.ts",
      "mimeType": "text/plain",
      "text": "export const foo = 'bar'\n"
    }
  }
}
```

**Binary resource:**
```json
{
  "sessionUpdate": "agent_message_chunk",
  "messageId": "msg_asst_001",
  "content": {
    "type": "resource",
    "resource": {
      "uri": "file:///path/to/binary.bin",
      "mimeType": "application/octet-stream",
      "blob": "<base64encodedblobdata>"
    }
  }
}
```

### 5.2 `resource_link` — Reference to a local file

Used for local `file://` URL references (not inlining content):

```json
{
  "sessionUpdate": "agent_message_chunk",
  "messageId": "msg_asst_001",
  "content": {
    "type": "resource_link",
    "uri": "file:///path/to/file.txt",
    "name": "file.txt",
    "mimeType": "text/plain"
  }
}
```

### 5.3 `image` — Inline image data

Used for data URLs with `image/*` MIME types:

```json
{
  "sessionUpdate": "agent_message_chunk",
  "messageId": "msg_asst_001",
  "content": {
    "type": "image",
    "mimeType": "image/png",
    "data": "<base64encodedimagedata>",
    "uri": "file:///path/to/image.png"
  }
}
```

---

## 6. Internal Message Part Types (message.ts / message-v2.ts)

These are the internal TypeScript types used in `Session.Message` stored objects, distinct from the ACP wire format above.

### TextPart
```typescript
{
  type: "text",
  text: string,
  // v2 additions:
  synthetic?: boolean,
  ignored?: boolean,
  time?: { start: number, end: number },
  metadata?: Record<string, unknown>
}
```

### ReasoningPart
```typescript
{
  type: "reasoning",
  text: string,
  providerMetadata?: Record<string, unknown>,
  // v2 additions:
  time?: { start: number, end: number }
}
```

### ToolInvocationPart (union of ToolCall | ToolPartialCall | ToolResult)
```typescript
{
  type: "tool-invocation",
  toolInvocation: ToolCall | ToolPartialCall | ToolResult
}

// ToolCall:
{
  state: "call",
  step?: number,
  toolCallId: string,
  toolName: string,    // e.g. "bash", "read", "edit", "write", "grep"
  args: unknown        // tool-specific input object
}

// ToolResult:
{
  state: "result",
  step?: number,
  toolCallId: string,
  toolName: string,
  args: unknown,
  result: string       // serialized output
}
```

### FilePart
```typescript
{
  type: "file",
  mediaType: string,   // "mime" in v2
  filename?: string,
  url: string,
  source?: FilePartSource  // v2: FileSource | SymbolSource | ResourceSource
}
```

### SourceUrlPart
```typescript
{
  type: "source-url",
  sourceId: string,
  url: string,
  title?: string,
  providerMetadata?: Record<string, unknown>
}
```

### StepStartPart
```typescript
{
  type: "step-start",
  snapshot?: string  // v2 addition
}
```

---

## 7. Complete Message (Info) Type

```typescript
{
  id: string,
  role: "user" | "assistant",
  parts: MessagePart[],   // union of all part types above
  metadata: {
    time: {
      created: number,
      completed?: number
    },
    error?: MessageError,
    sessionID: string,
    tool: Record<string, {
      title: string,
      snapshot?: string,
      time: { start: number, end: number },
      [key: string]: unknown
    }>,
    assistant?: {
      system: string[],
      modelID: string,
      providerID: string,
      path: { cwd: string, root: string },
      cost: number,
      summary?: boolean,
      tokens: {
        input: number,
        output: number,
        reasoning: number,
        cache: { read: number, write: number }
      }
    },
    snapshot?: string
  }
}
```

---

## 8. Tool Input Parameter Summary

| Tool   | Required fields in `rawInput` | Optional fields |
|--------|-------------------------------|-----------------|
| `bash` | `command: string` | `description: string` |
| `read` | `filePath: string` | `offset: number`, `limit: number` |
| `write`| `filePath: string`, `content: string` (full file) | — |
| `edit` | `filePath: string`, `oldString: string`, `newString: string` | — |
| `grep` | `pattern: string` | `path: string`, `include: string` (glob) |
| `glob` | `pattern: string` | `path: string` |

---

## 9. How the Edit Tool Sends Edited Lines

The `edit` tool communicates changes at three levels:

1. **`rawInput`** — The LLM-level instruction: what string to find (`oldString`) and what to replace it with (`newString`).
2. **`rawOutput.metadata.filediff`** — A rendered diff for UI display:
   - `file`: path of the edited file
   - `before`: snippet of context around removed lines
   - `after`: snippet of context around added lines
   - `additions`: count of added lines
   - `deletions`: count of deleted lines
3. **`permission.asked` event** — When the agent requests permission to apply the edit, the metadata includes `filepath` and `diff` (unified diff string).

---

## 10. How the Plan is Updated

1. The `TodoWrite` tool is called by the agent with a list of plan entries.
2. When `TodoWrite` completes, the ACPAgent parses its output into `PlanEntry[]`.
3. A `sessionUpdate: "plan"` message is sent with the full updated `entries` array.
4. The client replaces its current plan display with the new entries.
5. Plan entries have `priority` (`high`/`medium`/`low`), `status` (`pending`/`in_progress`/`completed`), and `content` (description).

---

## 11. References

- `packages/opencode/src/acp/agent.ts` — ACPAgent, sessionUpdate dispatch, toLocations()
- `packages/opencode/src/session/message.ts` — V1 message schema
- `packages/opencode/src/session/message-v2.ts` — V2 message schema (current)
- `packages/opencode/test/acp/event-subscription.test.ts` — ACP integration tests with image examples
- `packages/ui/src/components/timeline-playground.stories.tsx` — TOOL_SAMPLES with concrete examples
- `packages/opencode/test/tool/__snapshots__/parameters.test.ts.snap` — Tool parameter schemas
- `@agentclientprotocol/sdk` — ACP SDK types (ToolCallUpdate, PlanEntry, etc.)
