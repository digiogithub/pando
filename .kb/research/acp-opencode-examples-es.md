# ACP Messages Examples (Agent Client Protocol) in sst/opencode

## 1. tool_call_update (in_progress, bash)
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call-456",
    "status": "in_progress",
    "kind": "bash",
    "title": "bash",
    "locations": [],
    "rawInput": {
      "command": "ls -l",
      "description": "List files"
    },
    "content": [
      {
        "type": "content",
        "content": {"type": "text", "text": "total 0\n-rw-r--r--  1 user  group  0 May 29 10:00 file.txt\n"}
      }
    ]
  }
}
```

## 2. tool_call_update (completed, read)
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call-789",
    "status": "completed",
    "kind": "read",
    "title": "read",
    "rawInput": {"filePath": "src/main.ts"},
    "content": [
      {"type": "content", "content": {"type": "text", "text": "console.log('Hello, World!');"}}
    ],
    "rawOutput": {"content": "console.log('Hello, World!');"}
  }
}
```

## 3. tool_call_update (completed, write)
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call-101",
    "status": "completed",
    "kind": "write",
    "title": "write",
    "rawInput": {"filePath": "src/new_file.ts", "content": "console.log('New file content');"},
    "content": [
      {"type": "content", "content": {"type": "text", "text": "File 'src/new_file.ts' written successfully."}}
    ],
    "rawOutput": {"message": "File 'src/new_file.ts' written successfully."}
  }
}
```

## 4. tool_call_update (completed, edit)
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call-102",
    "status": "completed",
    "kind": "edit",
    "title": "edit",
    "rawInput": {"filePath": "src/main.ts", "startLine": 1, "endLine": 1, "replacement": "console.log('Updated Hello, World!');"},
    "content": [
      {"type": "content", "content": {"type": "text", "text": "File 'src/main.ts' edited successfully."}}
    ],
    "rawOutput": {"message": "File 'src/main.ts' edited successfully."}
  }
}
```

## 5. tool_call_update (completed, grep)
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call-103",
    "status": "completed",
    "kind": "grep",
    "title": "grep",
    "rawInput": {"pattern": "function", "filePath": "src/utils.ts"},
    "content": [
      {"type": "content", "content": {"type": "text", "text": "src/utils.ts:10:function myFunction() {}"}}
    ],
    "rawOutput": {"results": [{"filePath": "src/utils.ts", "lineNumber": 10, "lineContent": "function myFunction() {}"}]}
  }
}
```

## 6. tool_call_update (completed, bash)
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call-104",
    "status": "completed",
    "kind": "bash",
    "title": "bash",
    "rawInput": {"command": "echo 'Hello from bash'", "description": "Echo a message"},
    "content": [
      {"type": "content", "content": {"type": "text", "text": "Hello from bash\n"}}
    ],
    "rawOutput": {"stdout": "Hello from bash\n", "stderr": "", "exitCode": 0}
  }
}
```

## 7. tool_call_update (failed, read)
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "tool_call_update",
    "toolCallId": "call-105",
    "status": "failed",
    "kind": "read",
    "title": "read",
    "rawInput": {"filePath": "/nonexistent/file.txt"},
    "content": [
      {"type": "content", "content": {"type": "text", "text": "Error: File not found"}}
    ],
    "rawOutput": {"error": "File not found", "metadata": {}}
  }
}
```

## 8. plan (plan update)
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "plan",
    "entries": [
      {"priority": "medium", "status": "todo", "content": "Implement feature X"},
      {"priority": "medium", "status": "completed", "content": "Fix bug Y"}
    ]
  }
}
```

## 9. resource_link
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "agent_message_chunk",
    "messageId": "msg-123",
    "content": {
      "type": "resource_link",
      "uri": "file:///path/to/document.pdf",
      "name": "document.pdf",
      "mimeType": "application/pdf"
    }
  }
}
```

## 10. image
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "agent_message_chunk",
    "messageId": "msg-123",
    "content": {
      "type": "image",
      "mimeType": "image/png",
      "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=",
      "uri": "file:///path/to/image.png"
    }
  }
}
```

## 11. resource
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "agent_message_chunk",
    "messageId": "msg-123",
    "content": {
      "type": "resource",
      "resource": {
        "uri": "file:///path/to/data.json",
        "mimeType": "application/json",
        "text": "{\"key\": \"value\"}"
      }
    }
  }
}
```

## 12. agent_message_chunk
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "agent_message_chunk",
    "messageId": "msg-456",
    "content": {
      "type": "text",
      "text": "The agent is processing your request..."
    }
  }
}
```

## 13. agent_thought_chunk
```json
{
  "sessionId": "session-123",
  "update": {
    "sessionUpdate": "agent_thought_chunk",
    "messageId": "msg-789",
    "content": {
      "type": "text",
      "text": "Thinking: I need to identify the relevant files first."
    }
  }
}
```

### Reference
This document summarizes the main ACP message types used in sst/opencode for tool usage, status, plan, and resources. Each example corresponds to the formats used and covers the key fields: sessionId, sessionUpdate, tool kind, rawInput, content, rawOutput, and type-specific details.
