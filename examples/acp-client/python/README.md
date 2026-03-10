

# Pando ACP Python Client Example

This example demonstrates how to use Pando as an ACP server from a Python client.

## Prerequisites

- Python 3.7 or later
- Pando installed and in PATH

## Installation

No additional dependencies required! This example uses only Python standard library.

## Running

```bash
# Make executable
chmod +x example.py

# Run
./example.py

# Or
python3 example.py
```

## What it does

This example:

1. Starts Pando as an ACP server
2. Initializes a client connection using stdio transport
3. Creates a new session with a temporary workspace
4. Sends prompts to create and analyze programs
5. Demonstrates file operations and workspace management

## Expected Output

```
🚀 Pando ACP Client Example (Python)
=====================================

📁 Workspace: /tmp/pando-example-abc123

🔧 Starting Pando ACP server...
✓ Pando server started

🔌 Initializing connection...
✓ Connected to pando v1.0.0
  Protocol version: 1

📋 Creating session...
✓ Session created: session-xyz789

Example 1: Create a Python hello world program
-----------------------------------------------
💭 Prompt: Create a simple hello world program in Python called hello.py
📝 Response:
   I'll create hello.py for you...
✓ File hello.py was created successfully

📄 File content:
print("Hello, World!")

Example 2: Run the Python program
----------------------------------
💭 Prompt: Run the hello.py program and show me the output
📝 Response:
   Running the program...
   Output: Hello, World!

Example 3: Create a web scraper
--------------------------------
💭 Prompt: Create a simple web scraper in Python that fetches the title from a URL
📝 Response:
   I'll create a web scraper...

Example 4: Code analysis
------------------------
💭 Prompt: List all the files in the current directory and describe what each one does
📝 Response:
   Files in directory:
   - hello.py: Prints "Hello, World!"
   - scraper.py: Fetches webpage titles using requests

📂 Files in workspace:
  - hello.py
  - scraper.py

✅ Examples completed successfully!
```

## Implementation Details

### JSON-RPC Communication

The client communicates with Pando using JSON-RPC 2.0 over stdio:

```python
def send_request(self, method: str, params: Dict[str, Any]) -> Dict[str, Any]:
    request = {
        "jsonrpc": "2.0",
        "id": self.request_id,
        "method": method,
        "params": params
    }

    # Send to stdin
    self.proc.stdin.write(json.dumps(request) + "\n")
    self.proc.stdin.flush()

    # Read from stdout
    response = json.loads(self.proc.stdout.readline())
    return response.get("result", {})
```

### Security

The example implements path validation to prevent directory traversal:

```python
def read_file(self, path: str) -> str:
    full_path = self.workspace / path

    # Security: ensure path is within workspace
    try:
        full_path = full_path.resolve()
        full_path.relative_to(self.workspace.resolve())
    except ValueError:
        raise Exception(f"Path outside workspace: {path}")

    return full_path.read_text()
```

### Error Handling

JSON-RPC errors are handled gracefully:

```python
if "error" in response:
    raise Exception(f"Server error: {response['error']}")
```

## Advanced Usage

### HTTP Transport

To use HTTP+SSE transport instead of stdio:

```python
import requests
import sseclient

class PandoHTTPClient:
    def __init__(self, base_url="http://localhost:8765"):
        self.base_url = base_url
        self.session_id = None
        self.request_id = 0

    def send_request(self, method: str, params: Dict[str, Any]) -> Dict[str, Any]:
        self.request_id += 1

        request = {
            "jsonrpc": "2.0",
            "id": self.request_id,
            "method": method,
            "params": params
        }

        headers = {"Content-Type": "application/json"}
        if self.session_id:
            headers["ACP-Session-Id"] = self.session_id

        response = requests.post(
            f"{self.base_url}/mesnada/acp",
            json=request,
            headers=headers
        )

        # Store session ID from response header
        if "ACP-Session-Id" in response.headers:
            self.session_id = response.headers["ACP-Session-Id"]

        return response.json().get("result", {})

    def subscribe_events(self):
        """Subscribe to SSE events"""
        headers = {"ACP-Session-Id": self.session_id}
        response = requests.get(
            f"{self.base_url}/mesnada/acp/events",
            headers=headers,
            stream=True
        )

        client = sseclient.SSEClient(response)
        for event in client.events():
            print(f"Event: {event.event}, Data: {event.data}")
```

### Async Support

For async operations using `asyncio`:

```python
import asyncio
import aiohttp

class AsyncPandoClient:
    async def send_request(self, method: str, params: Dict[str, Any]) -> Dict[str, Any]:
        async with aiohttp.ClientSession() as session:
            async with session.post(
                f"{self.base_url}/mesnada/acp",
                json={"jsonrpc": "2.0", "id": 1, "method": method, "params": params},
                headers={"Content-Type": "application/json"}
            ) as response:
                data = await response.json()
                return data.get("result", {})

    async def prompt_async(self, text: str):
        return await self.send_request("prompt", {
            "sessionId": self.session_id,
            "prompt": [{"type": "text", "text": text}]
        })

# Usage
async def main():
    client = AsyncPandoClient()
    await client.initialize()
    result = await client.prompt_async("Create a Python script")

asyncio.run(main())
```

### Custom Client Callbacks

To implement a full ACP client with callbacks:

```python
class FullACPClient(PandoACPClient):
    def handle_callback(self, callback_request: Dict[str, Any]) -> Dict[str, Any]:
        """Handle callbacks from Pando"""
        method = callback_request.get("method")
        params = callback_request.get("params", {})

        if method == "readTextFile":
            path = params.get("path")
            content = self.read_file(path)
            return {"content": content}

        elif method == "writeTextFile":
            path = params.get("path")
            content = params.get("content")
            self.write_file(path, content)
            return {}

        elif method == "createTerminal":
            command = params.get("command")
            args = params.get("args", [])
            return {"terminalId": self.create_terminal(command, args)}

        elif method == "requestPermission":
            # Auto-approve or prompt user
            return {"outcome": {"selected": {"optionId": "approve"}}}

        else:
            raise Exception(f"Unknown callback method: {method}")
```

## Customization

Modify the prompts to test different scenarios:

```python
# Create a Flask app
client.prompt("Create a simple Flask web app with a hello endpoint")

# Data analysis
client.prompt("Create a script that analyzes a CSV file and generates a report")

# Testing
client.prompt("Create unit tests for the hello.py file")

# Debugging
client.prompt("Find and fix any bugs in the code")
```

## Troubleshooting

### "pando: command not found"

Ensure Pando is installed and in PATH:

```bash
which pando
export PATH="$PATH:/path/to/pando"
```

### Connection timeout

The server might take a moment to start. Add a delay:

```python
client.start_server()
time.sleep(1)  # Wait for server to be ready
client.initialize()
```

### JSON decode errors

Ensure server output is properly formatted. Debug with:

```python
response_line = self.proc.stdout.readline()
print(f"Raw response: {response_line}")
response = json.loads(response_line)
```

### Server not responding

Check if the process is running:

```python
if self.proc.poll() is not None:
    stderr = self.proc.stderr.read()
    raise Exception(f"Server died: {stderr}")
```

## Dependencies (Optional)

For HTTP transport support, install:

```bash
pip install requests sseclient-py
```

For async support:

```bash
pip install aiohttp
```

## Further Reading

- [Pando ACP Server Documentation](../../../docs/acp-server.md)
- [ACP Specification](https://github.com/coder/acp-spec)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)

## License

See main Pando LICENSE file.
