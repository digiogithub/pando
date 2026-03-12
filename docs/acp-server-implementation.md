# ACP Server Implementation - Phase 1

## Overview

This document describes the implementation of the ACP (Agent Client Protocol) server functionality in Pando, allowing other ACP clients to connect to Pando.

## Implementation Summary

### Files Created

1. **`internal/mesnada/acp/server.go`**
   - `PandoACPAgent` struct implementing the `acpsdk.Agent` interface
   - Handles initialization handshake with ACP clients
   - Implements required Agent interface methods:
     - `Initialize()` - Handshake and capability negotiation
     - `Authenticate()` - Authentication (not yet implemented)
     - `Cancel()` - Cancellation notifications
     - `NewSession()` - Session management (not yet implemented)
     - `Prompt()` - Prompt handling (not yet implemented)
     - `SetSessionMode()` - Session mode changes (not yet implemented)

2. **`internal/mesnada/acp/transport_stdio.go`**
   - `StdioTransport` struct wrapping SDK's `AgentSideConnection`
   - Handles stdio-based JSON-RPC communication
   - Uses the ACP SDK's built-in message handling

3. **`internal/mesnada/acp/server_test.go`**
   - Unit tests for agent initialization
   - Tests for capability negotiation
   - Tests for protocol version handling

### Files Modified

1. **`cmd/root.go`**
   - Added `--acp-server` flag
   - Added `runACPServer()` function to launch ACP server mode
   - Imports ACP agent package

## Usage

### Starting the ACP Server

```bash
pando --acp-server
```

This starts Pando in ACP server mode, listening on stdin/stdout for JSON-RPC messages.

### Testing the Server

You can test the server with a simple initialize request:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientInfo":{"name":"test-client","version":"1.0.0"}}}' | pando --acp-server
```

Expected response:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "agentCapabilities": {
      "mcpCapabilities": {},
      "promptCapabilities": {}
    },
    "agentInfo": {
      "name": "pando",
      "version": "v0.3.1-..."
    },
    "authMethods": [],
    "protocolVersion": 1
  }
}
```

## Architecture

### Agent Implementation

Pando acts as an **ACP Agent** (server-side), implementing the `acpsdk.Agent` interface. This allows external clients to:
- Connect via stdio
- Negotiate capabilities
- Send prompts and commands
- Receive responses

### Transport Layer

The transport uses the SDK's `AgentSideConnection` which:
- Handles JSON-RPC message parsing
- Dispatches methods to the agent implementation
- Manages connection lifecycle
- Writes responses to stdout

### Capabilities

Currently advertised capabilities:
- `LoadSession`: false (not yet implemented)
- `McpCapabilities`: empty (no MCP support yet)
- `PromptCapabilities`: empty (no special prompt features yet)

## Current Limitations (Phase 1)

This is a **foundational implementation** focusing on the handshake and basic infrastructure:

1. ✅ **Implemented:**
   - Initialize handshake
   - Protocol version negotiation
   - Capability advertisement
   - Stdio transport
   - Basic tests

2. ⚠️ **Not Yet Implemented:**
   - Session management (`NewSession`)
   - Prompt handling (`Prompt`)
   - Authentication (`Authenticate`)
   - Session mode changes (`SetSessionMode`)
   - File system operations (client callbacks)
   - Terminal operations (client callbacks)

## Next Steps (Future Phases)

### Phase 2: Session Management
- Implement `NewSession` to create agent sessions
- Session state tracking
- Session context management

### Phase 3: Prompt Handling
- Implement `Prompt` to handle user prompts
- Integration with Pando's existing LLM infrastructure
- Stream responses back to client

### Phase 4: Client Callbacks
- Implement client callback handlers for:
  - File operations (`ReadTextFile`, `WriteTextFile`)
  - Terminal operations (`CreateTerminal`, etc.)
  - Permission requests

### Phase 5: Advanced Features
- MCP capabilities
- Authentication methods
- Session persistence/loading

## Testing

Run the tests:
```bash
go test ./internal/mesnada/acp/... -v
```

All tests pass:
- `TestNewPandoACPAgent`
- `TestNewPandoACPAgent_NilLogger`
- `TestInitialize_Success`
- `TestGetVersion`
- `TestGetCapabilities`
- `TestCancel`

## Success Criteria ✅

All Phase 1 success criteria met:

- ✅ Server can start with `pando --acp-server`
- ✅ Handshake Initialize works correctly
- ✅ Client can connect and negotiate capabilities
- ✅ Tests pass correctly
- ✅ Code compiles without errors

## References

- ACP SDK: `github.com/coder/acp-go-sdk`
- Existing client implementation: `internal/mesnada/acp/client.go`
- Protocol documentation: https://agentclientprotocol.com/
