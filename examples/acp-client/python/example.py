#!/usr/bin/env python3
"""
Pando ACP Python Client Example

This example demonstrates how to use Pando as an ACP server from a Python client.
"""

import subprocess
import json
import sys
import tempfile
import os
from pathlib import Path
from typing import Dict, Any, Optional


class PandoACPClient:
    """A simple ACP client for Pando"""

    def __init__(self, workspace: str):
        self.workspace = Path(workspace)
        self.proc = None
        self.request_id = 0
        self.session_id = None

    def start_server(self):
        """Start Pando ACP server"""
        print("🔧 Starting Pando ACP server...")
        self.proc = subprocess.Popen(
            ["pando", "--acp-server"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1
        )
        print("✓ Pando server started\n")

    def stop_server(self):
        """Stop Pando ACP server"""
        if self.proc:
            self.proc.terminate()
            self.proc.wait()

    def send_request(self, method: str, params: Dict[str, Any]) -> Dict[str, Any]:
        """Send JSON-RPC request to Pando"""
        self.request_id += 1

        request = {
            "jsonrpc": "2.0",
            "id": self.request_id,
            "method": method,
            "params": params
        }

        # Send request
        request_json = json.dumps(request)
        self.proc.stdin.write(request_json + "\n")
        self.proc.stdin.flush()

        # Read response
        response_line = self.proc.stdout.readline()
        if not response_line:
            raise Exception("No response from server")

        response = json.loads(response_line)

        # Check for errors
        if "error" in response:
            raise Exception(f"Server error: {response['error']}")

        return response.get("result", {})

    def initialize(self) -> Dict[str, Any]:
        """Initialize ACP connection"""
        print("🔌 Initializing connection...")
        result = self.send_request("initialize", {
            "protocolVersion": 1,
            "clientInfo": {
                "name": "pando-python-example",
                "version": "1.0.0"
            }
        })

        agent_info = result.get("agentInfo", {})
        print(f"✓ Connected to {agent_info.get('name')} v{agent_info.get('version')}")
        print(f"  Protocol version: {result.get('protocolVersion')}\n")

        return result

    def new_session(self, cwd: Optional[str] = None) -> Dict[str, Any]:
        """Create a new session"""
        print("📋 Creating session...")
        result = self.send_request("newSession", {
            "cwd": cwd or str(self.workspace)
        })

        self.session_id = result.get("sessionId")
        print(f"✓ Session created: {self.session_id}\n")

        return result

    def prompt(self, text: str) -> Dict[str, Any]:
        """Send a prompt to Pando"""
        print(f"💭 Prompt: {text}")

        result = self.send_request("prompt", {
            "sessionId": self.session_id,
            "prompt": [
                {
                    "type": "text",
                    "text": text
                }
            ]
        })

        # Display response
        print("📝 Response:")
        message = result.get("message", [])
        for block in message:
            if block.get("type") == "text":
                print(f"   {block.get('text')}")

        return result

    def read_file(self, path: str) -> str:
        """Read a file from the workspace"""
        full_path = self.workspace / path

        # Security: ensure path is within workspace
        try:
            full_path = full_path.resolve()
            full_path.relative_to(self.workspace.resolve())
        except ValueError:
            raise Exception(f"Path outside workspace: {path}")

        return full_path.read_text()

    def write_file(self, path: str, content: str):
        """Write content to a file in the workspace"""
        full_path = self.workspace / path

        # Security: ensure path is within workspace
        try:
            full_path = full_path.resolve()
            full_path.relative_to(self.workspace.resolve())
        except ValueError:
            raise Exception(f"Path outside workspace: {path}")

        # Create directory if needed
        full_path.parent.mkdir(parents=True, exist_ok=True)
        full_path.write_text(content)

    def list_files(self) -> list:
        """List files in workspace"""
        return [f.name for f in self.workspace.iterdir() if f.is_file()]


def main():
    """Run the example"""
    print("🚀 Pando ACP Client Example (Python)")
    print("=====================================\n")

    # Create temporary workspace
    with tempfile.TemporaryDirectory(prefix="pando-example-") as workspace:
        print(f"📁 Workspace: {workspace}\n")

        # Create client
        client = PandoACPClient(workspace)

        try:
            # Start server
            client.start_server()

            # Initialize
            client.initialize()

            # Create session
            client.new_session()

            # Example 1: Create a Python program
            print("Example 1: Create a Python hello world program")
            print("-----------------------------------------------")
            client.prompt("Create a simple hello world program in Python called hello.py")
            print()

            # Check if file was created
            import time
            time.sleep(1)
            if os.path.exists(os.path.join(workspace, "hello.py")):
                print("✓ File hello.py was created successfully\n")

                # Show file content
                content = client.read_file("hello.py")
                print("📄 File content:")
                print(content)
                print()

            # Example 2: Run the Python program
            print("Example 2: Run the Python program")
            print("----------------------------------")
            client.prompt("Run the hello.py program and show me the output")
            print()

            # Example 3: Create a more complex program
            print("Example 3: Create a web scraper")
            print("--------------------------------")
            client.prompt("Create a simple web scraper in Python that fetches the title from a URL")
            print()

            # Example 4: Analyze code
            print("Example 4: Code analysis")
            print("------------------------")
            client.prompt("List all the files in the current directory and describe what each one does")
            print()

            # List files in workspace
            print("📂 Files in workspace:")
            files = client.list_files()
            for file in files:
                print(f"  - {file}")
            print()

            print("✅ Examples completed successfully!")

        except KeyboardInterrupt:
            print("\n⚠️  Interrupted by user")
        except Exception as e:
            print(f"\n❌ Error: {e}")
            import traceback
            traceback.print_exc()
        finally:
            # Stop server
            client.stop_server()


if __name__ == "__main__":
    main()
