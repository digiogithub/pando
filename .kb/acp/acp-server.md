

# Pando ACP Server

## Overview

Pando can act as an **ACP (Agent Client Protocol) server**, allowing other ACP-compatible clients to connect and execute conversations. This enables Pando to be used as a backend service for various AI agent applications.

The ACP implementation follows the [Agent Client Protocol specification](https://github.com/coder/acp-spec) and supports both stdio and HTTP+SSE transport modes.

## Architecture

```
┌─────────────────┐
│  ACP Client     │
│  (Go/Python/etc)│
└────────┬────────┘
         │
         │ HTTP/JSONRPC or stdio
         │
┌────────▼────────┐
│  HTTP Transport │
│  (SSE Events)   │
├─────────────────┤
│  ACP Agent      │
│  (Pando Core)   │
├─────────────────┤
│  Client Conn    │
│  (File/Terminal)│
└────────┬────────┘
         │
         │ Workspace Operations
         │
┌────────▼────────┐
│  File System    │
│  Terminals      │
└─────────────────┘
```

### Components

1. **HTTP Transport** (`transport_http.go`)
   - Handles HTTP+SSE connections
   - Manages multiple concurrent sessions
   - Provides session lifecycle management
   - Implements idle timeout and cleanup

2. **ACP Agent** (`agent_simple.go`)
   - Implements ACP protocol methods
   - Handles initialization and authentication
   - Manages session creation and prompts

3. **Client Connection** (`client_connection.go`)
   - Provides file access (read/write)
   - Manages terminal creation and I/O
   - Enforces security boundaries (path validation)
   - Implements capability-based permissions

4. **Permission System** (`permissions.go`)
   - Handles tool execution permissions
   - Supports auto-approval or manual approval
   - Queue-based permission management

## Quick Start

### Stdio Mode

Run Pando as an ACP server using stdio for communication:

```bash
pando --acp-server
```

This starts Pando in ACP mode, reading JSON-RPC requests from stdin and writing responses to stdout.

### HTTP Mode

Configure HTTP+SSE transport in `.pando.toml`:

```toml
[mesnada.acp_server]
enabled = true
transports = ["http"]
host = "0.0.0.0"
port = 8765
max_sessions = 100
idle_timeout = "30m"
```

Start the server:

```bash
pando server --acp
```

The server will be available at `http://localhost:8765/mesnada/acp`

## Configuration

### Configuration File (.pando.toml)

```toml
[mesnada]
# Enable Mesnada orchestrator
enabled = true

[mesnada.acp_server]
# Enable ACP server
enabled = true

# Transports: ["stdio", "http", "http+sse"]
transports = ["http"]

# HTTP configuration
host = "0.0.0.0"
port = 8765

# Session management
max_sessions = 100
idle_timeout = "30m"
event_buf_size = 1000

# Security
[mesnada.acp_server.capabilities]
file_access = true
terminals = true
permissions = true

# Auto-approve tool executions (use with caution)
auto_permission = false
```

### Environment Variables

- `PANDO_ACP_PORT`: Override ACP server port
- `PANDO_ACP_HOST`: Override ACP server host
- `PANDO_ACP_ENABLED`: Enable/disable ACP server

### Command-Line Flags

```bash
# Start ACP server with custom port
pando server --acp --acp-port 9000

# Start with stdio transport only
pando --acp-server --acp-transport stdio

# Enable debug logging
pando server --acp --log-level debug
```

## API Reference

### HTTP Endpoints

#### POST `/mesnada/acp`

Main JSON-RPC endpoint for ACP protocol methods.

**Headers:**
- `Content-Type`: `application/json`
- `ACP-Session-Id`: Session identifier (optional for initialize)

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": 1,
    "clientInfo": {
      "name": "my-client",
      "version": "1.0.0"
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": 1,
    "agentInfo": {
      "name": "pando",
      "version": "1.0.0"
    },
    "agentCapabilities": {
      "loadSession": false,
      "mcpCapabilities": {
        "http": true,
        "sse": true
      },
      "promptCapabilities": {
        "audio": false,
        "image": false,
        "embeddedContext": false
      }
    }
  }
}
```

#### GET `/mesnada/acp/events`

Server-Sent Events (SSE) stream for real-time updates.

**Headers:**
- `ACP-Session-Id`: Session identifier (required)

**Response:**
```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive

event: connected
data: {"session_id":"abc123"}

event: message
data: {"type":"status","content":"Processing..."}
```

#### GET `/mesnada/acp/health`

Health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "transport": "http+sse",
  "active_sessions": 5,
  "uptime_seconds": 3600
}
```

#### GET `/api/acp/sessions`

List all active sessions (admin endpoint).

**Response:**
```json
{
  "sessions": [
    {
      "session_id": "abc123",
      "created_at": "2026-03-11T10:00:00Z",
      "last_activity": "2026-03-11T10:05:00Z",
      "workspace": "/tmp/workspace"
    }
  ]
}
```

#### GET `/api/acp/stats`

Get server statistics.

**Response:**
```json
{
  "active_sessions": 5,
  "total_requests": 1250,
  "uptime_seconds": 7200,
  "version": "1.0.0"
}
```

### ACP Protocol Methods

#### `initialize`

Initialize the ACP connection.

**Params:**
```json
{
  "protocolVersion": 1,
  "clientInfo": {
    "name": "client-name",
    "version": "1.0.0"
  }
}
```

#### `newSession`

Create a new agent session.

**Params:**
```json
{
  "cwd": "/path/to/workspace",
  "sessionId": "optional-custom-id"
}
```

#### `prompt`

Send a prompt to the agent.

**Params:**
```json
{
  "sessionId": "session-123",
  "prompt": [
    {
      "type": "text",
      "text": "List files in the current directory"
    }
  ]
}
```

#### `setSessionMode`

Change the session mode (e.g., code, ask, architect).

**Params:**
```json
{
  "sessionId": "session-123",
  "mode": "code"
}
```

#### `cancel`

Cancel an ongoing operation.

**Params:**
```json
{
  "sessionId": "session-123",
  "requestId": "request-456"
}
```

### Client Callbacks

The ACP client must implement these callback methods:

#### `readTextFile`

Read a text file from the workspace.

**Request:**
```json
{
  "path": "relative/path/to/file.txt"
}
```

#### `writeTextFile`

Write content to a file.

**Request:**
```json
{
  "path": "relative/path/to/file.txt",
  "content": "file contents"
}
```

#### `createTerminal`

Create a new terminal session.

**Request:**
```json
{
  "command": "npm",
  "args": ["install"],
  "cwd": "optional/subdirectory"
}
```

#### `terminalOutput`

Get output from a terminal.

**Request:**
```json
{
  "terminalId": "terminal-123"
}
```

#### `requestPermission`

Request permission to execute a tool.

**Request:**
```json
{
  "sessionId": "session-123",
  "toolCall": {
    "title": "Run npm install",
    "description": "Install dependencies"
  },
  "options": [
    {"optionId": "approve", "name": "Approve"},
    {"optionId": "deny", "name": "Deny"}
  ]
}
```

## Client Examples

### Go Client

```go
package main

import (
    "context"
    "fmt"
    "os/exec"

    acpsdk "github.com/madeindigio/acp-go-sdk"
)

func main() {
    // Start Pando ACP server
    cmd := exec.Command("pando", "--acp-server")
    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    cmd.Start()
    defer cmd.Process.Kill()

    // Create client connection
    client := &SimpleACPClient{}
    conn := acpsdk.NewClientSideConnection(client, stdin, stdout)

    // Initialize
    initResp, err := conn.Initialize(context.Background(), acpsdk.InitializeRequest{
        ProtocolVersion: acpsdk.ProtocolVersionNumber,
        ClientInfo: &acpsdk.Implementation{
            Name: "example-client",
            Version: "1.0.0",
        },
    })
    if err != nil {
        panic(err)
    }
    fmt.Printf("Connected to %s v%s\n", initResp.AgentInfo.Name, initResp.AgentInfo.Version)

    // Create session
    sessionResp, err := conn.NewSession(context.Background(), acpsdk.NewSessionRequest{
        Cwd: "/tmp/workspace",
    })
    if err != nil {
        panic(err)
    }

    // Send prompt
    promptResp, err := conn.Prompt(context.Background(), acpsdk.PromptRequest{
        SessionId: sessionResp.SessionId,
        Prompt: []acpsdk.ContentBlock{
            acpsdk.TextBlock("Create a simple hello world program in Python"),
        },
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("Response: %v\n", promptResp.Message)
}

// SimpleACPClient implements ACP client callbacks
type SimpleACPClient struct{}

func (c *SimpleACPClient) ReadTextFile(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
    // Implement file reading
    return acpsdk.ReadTextFileResponse{}, nil
}

// ... implement other callback methods
```

### Python Client

```python
#!/usr/bin/env python3
import subprocess
import json
import sys

# Start Pando ACP server
proc = subprocess.Popen(
    ["pando", "--acp-server"],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True
)

def send_request(method, params, req_id=1):
    """Send JSON-RPC request"""
    request = {
        "jsonrpc": "2.0",
        "id": req_id,
        "method": method,
        "params": params
    }
    proc.stdin.write(json.dumps(request) + "\n")
    proc.stdin.flush()

    response = proc.stdout.readline()
    return json.loads(response)

# Initialize
init_resp = send_request("initialize", {
    "protocolVersion": 1,
    "clientInfo": {"name": "python-client", "version": "1.0.0"}
})
print(f"Connected: {init_resp}")

# Create session
session_resp = send_request("newSession", {
    "cwd": "/tmp/workspace"
}, req_id=2)
print(f"Session: {session_resp}")

# Send prompt
prompt_resp = send_request("prompt", {
    "sessionId": session_resp["result"]["sessionId"],
    "prompt": [{"type": "text", "text": "List files"}]
}, req_id=3)
print(f"Response: {prompt_resp}")

proc.terminate()
```

### HTTP Client Example

```bash
# Initialize
curl -X POST http://localhost:8765/mesnada/acp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": 1,
      "clientInfo": {"name": "curl-client", "version": "1.0.0"}
    }
  }'

# Connect to SSE stream
curl -N -H "ACP-Session-Id: <session-id>" \
  http://localhost:8765/mesnada/acp/events
```

## Security

### Path Validation

All file operations enforce strict path validation:

- Absolute paths are denied
- Path traversal (`../`) is blocked
- All paths must resolve within the workspace directory

```go
// Example path validation
conn.ReadTextFile(ctx, "file.txt")           // ✓ Allowed
conn.ReadTextFile(ctx, "subdir/file.txt")    // ✓ Allowed
conn.ReadTextFile(ctx, "/etc/passwd")        // ✗ Denied
conn.ReadTextFile(ctx, "../../../etc/passwd") // ✗ Denied
```

### Capability System

Control what operations clients can perform:

```toml
[mesnada.acp_server.capabilities]
file_access = true    # Allow file read/write
terminals = true      # Allow terminal creation
permissions = true    # Enable permission system
```

Capabilities can be set per-session:

```go
client.SetCapabilities(ACPCapabilities{
    FileAccess:  true,
    Terminals:   false,  // Disable terminal access
    Permissions: true,
})
```

### Permission System

By default, all tool executions require permission:

```go
// Manual approval (default)
client.SetAutoPermission(false)

// Get pending permissions
pending := client.GetPermissionQueue().GetPending("task-id")

// Approve/deny permission
queue.ResolvePermission(requestID, acpsdk.NewRequestPermissionOutcomeSelected("approve"))
```

Enable auto-approval for trusted environments:

```go
client.SetAutoPermission(true)  // Automatically approve all permissions
```

### Resource Limits

Configure limits to prevent abuse:

```toml
[mesnada.acp_server]
max_sessions = 100           # Maximum concurrent sessions
idle_timeout = "30m"         # Session idle timeout
event_buf_size = 1000        # SSE event buffer size
```

### Authentication

Currently, the ACP server does not implement authentication. For production use:

1. Deploy behind a reverse proxy with authentication
2. Use network-level security (VPN, firewall)
3. Implement custom authentication in the transport layer

## Troubleshooting

### Common Issues

#### "Session not found"

**Cause:** Session expired due to idle timeout or was manually closed.

**Solution:** Create a new session with `newSession`.

#### "Max sessions exceeded"

**Cause:** Server has reached `max_sessions` limit.

**Solution:**
- Close unused sessions
- Increase `max_sessions` in configuration
- Implement session pooling in your client

#### "Path outside workspace"

**Cause:** Attempted to access file outside workspace directory.

**Solution:** Use relative paths only, ensure all files are within the workspace.

#### SSE connection drops

**Cause:** Network timeout or idle connection.

**Solution:**
- Implement reconnection logic in client
- Send periodic keep-alive pings
- Adjust server `idle_timeout`

#### "Permission denied"

**Cause:** Tool execution requires permission approval.

**Solution:**
- Implement permission handling in client
- Enable `auto_permission` for trusted environments
- Manually approve permissions via queue

### Debug Logging

Enable debug logging to troubleshoot issues:

```bash
# Environment variable
export PANDO_LOG_LEVEL=debug
pando server --acp

# Command-line flag
pando server --acp --log-level debug
```

View ACP-specific logs:

```bash
# Filter ACP logs
pando server --acp 2>&1 | grep -i "acp"
```

### Testing Connection

Test basic connectivity:

```bash
# Health check
curl http://localhost:8765/mesnada/acp/health

# Initialize test
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientInfo":{"name":"test","version":"1.0"}}}' | \
  pando --acp-server
```

## Performance Tuning

### Session Management

```toml
[mesnada.acp_server]
max_sessions = 100       # Adjust based on expected concurrent users
idle_timeout = "30m"     # Balance between resource usage and user experience
event_buf_size = 1000    # Increase for high-throughput applications
```

### HTTP Transport

Use reverse proxy for production:

```nginx
# nginx configuration
upstream acp_backend {
    server localhost:8765;
    keepalive 32;
}

server {
    listen 80;
    server_name acp.example.com;

    location /mesnada/acp {
        proxy_pass http://acp_backend;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 24h;
    }
}
```

### Benchmarking

Run performance benchmarks:

```bash
# All benchmarks
go test -bench=. ./internal/mesnada/acp/...

# Specific benchmark
go test -bench=BenchmarkHTTPTransport ./internal/mesnada/acp/...

# With memory profiling
go test -bench=. -benchmem ./internal/mesnada/acp/...

# CPU profiling
go test -bench=. -cpuprofile=cpu.prof ./internal/mesnada/acp/...
```

### Optimization Tips

1. **Connection pooling**: Reuse HTTP connections
2. **Session reuse**: Keep sessions alive for multiple requests
3. **Batch operations**: Group file operations when possible
4. **Event buffering**: Tune `event_buf_size` for your workload
5. **Idle cleanup**: Adjust `idle_timeout` to free resources

## Monitoring

### Metrics

Access server metrics via API:

```bash
curl http://localhost:8765/api/acp/stats
```

```json
{
  "active_sessions": 15,
  "total_requests": 5432,
  "uptime_seconds": 86400,
  "version": "1.0.0"
}
```

### Session Monitoring

List active sessions:

```bash
curl http://localhost:8765/api/acp/sessions
```

### Health Checks

Implement health checks in your infrastructure:

```bash
# Kubernetes liveness probe
livenessProbe:
  httpGet:
    path: /mesnada/acp/health
    port: 8765
  initialDelaySeconds: 3
  periodSeconds: 10
```

## Development

### Running Tests

```bash
# Unit tests
go test ./internal/mesnada/acp/...

# Integration tests
go test -tags=integration ./test/e2e/...

# Coverage
go test -cover ./internal/mesnada/acp/...

# Detailed coverage
go test -coverprofile=coverage.out ./internal/mesnada/acp/...
go tool cover -html=coverage.out
```

### Building

```bash
# Build pando with ACP support
go build -o pando ./cmd/pando

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o pando-linux ./cmd/pando
```

### Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for contribution guidelines.

## FAQ

**Q: Can I run multiple ACP servers on the same machine?**

A: Yes, use different ports for each instance:

```bash
pando server --acp --acp-port 8765
pando server --acp --acp-port 8766
```

**Q: How do I persist sessions across server restarts?**

A: Currently, sessions are in-memory only. Implement session persistence by:
- Saving session state to database
- Implementing `loadSession` capability
- Restoring sessions on startup

**Q: Is WebSocket transport supported?**

A: Not currently. Use HTTP+SSE for real-time bidirectional communication.

**Q: Can I use Pando ACP with non-Go clients?**

A: Yes! Any language that supports HTTP and JSON-RPC can connect. See Python example above.

**Q: How do I secure the ACP server?**

A: Deploy behind a reverse proxy with:
- TLS/HTTPS encryption
- Authentication (Basic Auth, OAuth, JWT)
- Rate limiting
- IP whitelisting

**Q: What's the maximum message size?**

A: Default is limited by HTTP server config. Adjust with:

```go
server.MaxRequestBodySize = 10 * 1024 * 1024 // 10MB
```

**Q: How do I handle long-running operations?**

A: Use the SSE stream for progress updates:
1. Client subscribes to `/mesnada/acp/events`
2. Server sends progress events
3. Client updates UI in real-time

## References

- [Agent Client Protocol Specification](https://github.com/coder/acp-spec)
- [ACP Go SDK](https://github.com/madeindigio/acp-go-sdk)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
- [Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events)

## Changelog

### v1.0.0 (2026-03-11)

- Initial ACP server implementation
- HTTP+SSE transport support
- Session management
- Security boundaries (path validation)
- Permission system
- Auto-approval mode

---

For issues and support, visit: https://github.com/anthropics/pando/issues
