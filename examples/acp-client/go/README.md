# Pando ACP Go Client Example

This example demonstrates how to use Pando as an ACP server from a Go client.

## Prerequisites

- Go 1.21 or later
- Pando installed and in PATH

## Installation

```bash
go mod download
```

## Running

```bash
go run main.go
```

## What it does

This example:

1. Starts Pando as an ACP server
2. Initializes a client connection
3. Creates a new session with a temporary workspace
4. Sends prompts to create and run programs
5. Demonstrates file and terminal operations

## Expected Output

```
🚀 Pando ACP Client Example
===========================

📁 Workspace: /tmp/pando-example-123456
🔧 Starting Pando ACP server...
✓ Pando server started

🔌 Initializing connection...
✓ Connected to pando v1.0.0
  Protocol version: 1

📋 Creating session...
✓ Session created: session-abc123

Example 1: Create a Python hello world program
-----------------------------------------------
💭 Prompt: Create a simple hello world program in Python called hello.py
📝 Response:
   I'll create hello.py for you...
✓ File hello.py was created successfully

Example 2: Run the Python program
----------------------------------
💭 Prompt: Run the hello.py program and show me the output
📝 Response:
   Running the program...
   Output: Hello, World!

Example 3: Create a simple web server
--------------------------------------
💭 Prompt: Create a simple HTTP web server in Go that responds with 'Hello, World!' on port 8080
📝 Response:
   I'll create a simple web server...

📂 Files in workspace:
  - hello.py
  - server.go

✅ Examples completed successfully!
```

## Implementation Notes

### Client Implementation

The `ExampleACPClient` implements the required ACP client interface:

- `ReadTextFile`: Reads files from the workspace
- `WriteTextFile`: Writes files to the workspace
- `CreateTerminal`: Creates terminal sessions for command execution
- `TerminalOutput`: Gets output from running terminals
- `WaitForTerminalExit`: Waits for terminal commands to complete
- `KillTerminalCommand`: Terminates running commands
- `ReleaseTerminal`: Cleans up terminal resources
- `RequestPermission`: Handles permission requests (auto-approves in this example)

### Security

The example implements path validation to ensure all file operations stay within the workspace:

```go
// Security: ensure path is within workspace
absPath, err := filepath.Abs(path)
if err != nil {
    return acpsdk.ReadTextFileResponse{}, fmt.Errorf("invalid path: %w", err)
}
absWorkspace, _ := filepath.Abs(c.workspace)
if !filepath.HasPrefix(absPath, absWorkspace) {
    return acpsdk.ReadTextFileResponse{}, fmt.Errorf("path outside workspace")
}
```

## Customization

You can modify the prompts in `main.go` to test different scenarios:

```go
// Create a React app
prompt := "Create a simple React app with a button that counts clicks"

// Analyze code
prompt := "Analyze the code in hello.py and suggest improvements"

// Debug
prompt := "Find and fix any bugs in server.go"
```

## Troubleshooting

### "pando: command not found"

Ensure Pando is installed and in your PATH:

```bash
which pando
```

### Connection timeout

Increase the context timeout:

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()
```

### Permission errors

The example auto-approves all permissions. For manual approval:

```go
func (c *ExampleACPClient) RequestPermission(ctx context.Context, req acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
    // Prompt user for approval
    fmt.Printf("Approve %s? (y/n): ", *req.ToolCall.Title)
    var response string
    fmt.Scanln(&response)

    if response == "y" {
        return acpsdk.RequestPermissionResponse{
            Outcome: acpsdk.NewRequestPermissionOutcomeSelected(req.Options[0].OptionId),
        }, nil
    }

    return acpsdk.RequestPermissionResponse{
        Outcome: acpsdk.NewRequestPermissionOutcomeCancelled(),
    }, nil
}
```

## Further Reading

- [Pando ACP Server Documentation](../../../docs/acp-server.md)
- [ACP Specification](https://github.com/coder/acp-spec)
- [ACP Go SDK](https://github.com/coder/acp-go-sdk)
