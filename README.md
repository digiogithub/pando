# 木 Pando

> **Fork of [OpenCode](https://github.com/digiogithub/pando)** by Kujtim Hoxha.
> Maintained by **José F. Rives**.


<img alt="pando mascot" title="pando mascot" src="https://github.com/digiogithub/pando/blob/main/assets/pando_mascot-fs8.png?raw=true" width="300" style="margin: 30px auto">


A powerful terminal-based AI assistant for developers, providing intelligent coding assistance directly in your terminal.

## Overview

Pando is a Go-based CLI application that brings AI assistance to your terminal. It provides a TUI (Terminal User Interface) for interacting with various AI models to help with coding tasks, debugging, and more.

## Features

- **Interactive TUI**: Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) for a smooth terminal experience
- **Multiple AI Providers**: Support for OpenAI, Anthropic Claude, Google Gemini, AWS Bedrock, Groq, Azure OpenAI, and OpenRouter
- **Session Management**: Save and manage multiple conversation sessions
- **Tool Integration**: AI can execute commands, search files, and modify code
- **Vim-like Editor**: Integrated editor with text input capabilities
- **Persistent Storage**: SQLite database for storing conversations and sessions
- **LSP Integration**: Language Server Protocol support for code intelligence
- **File Change Tracking**: Track and visualize file changes during sessions
- **External Editor Support**: Open your preferred editor for composing messages
- **Named Arguments for Custom Commands**: Create powerful custom commands with multiple named placeholders
- **Configuration**: Supports both JSON and TOML configuration files

## Installation

### Using Go

```bash
go install github.com/digiogithub/pando@latest
```

### Building from Source

```bash
git clone https://github.com/your-repo/pando.git
cd pando
cd web-ui && bun install && bun run build:embedded && cd ..
go build -o pando
./pando app
```

## Configuration

Pando looks for configuration in the following locations:

- `$HOME/.pando.json` or `$HOME/.pando.toml`
- `$XDG_CONFIG_HOME/pando/.pando.json` or `$XDG_CONFIG_HOME/pando/.pando.toml`
- `./.pando.json` or `./.pando.toml` (local directory)

Both JSON and TOML formats are supported. Pando auto-detects the format based on the file extension.

### Environment Variables

You can configure Pando using environment variables (prefixed with `PANDO_` for app-specific settings):

| Environment Variable       | Purpose                                                                          |
| -------------------------- | -------------------------------------------------------------------------------- |
| `ANTHROPIC_API_KEY`        | For Claude models                                                                |
| `OPENAI_API_KEY`           | For OpenAI models                                                                |
| `GEMINI_API_KEY`           | For Google Gemini models                                                         |
| `GITHUB_TOKEN`             | For Github Copilot models                                                        |
| `VERTEXAI_PROJECT`         | For Google Cloud VertexAI (Gemini)                                               |
| `VERTEXAI_LOCATION`        | For Google Cloud VertexAI (Gemini)                                               |
| `GROQ_API_KEY`             | For Groq models                                                                  |
| `AWS_ACCESS_KEY_ID`        | For AWS Bedrock (Claude)                                                         |
| `AWS_SECRET_ACCESS_KEY`    | For AWS Bedrock (Claude)                                                         |
| `AWS_REGION`               | For AWS Bedrock (Claude)                                                         |
| `AZURE_OPENAI_ENDPOINT`    | For Azure OpenAI models                                                          |
| `AZURE_OPENAI_API_KEY`     | For Azure OpenAI models (optional when using Entra ID)                           |
| `AZURE_OPENAI_API_VERSION` | For Azure OpenAI models                                                          |
| `LOCAL_ENDPOINT`           | For self-hosted models                                                           |
| `PANDO_DEV_DEBUG`          | Enable dev debug mode (`true`)                                                   |
| `SHELL`                    | Default shell to use (if not specified in config)                                |

### Configuration File Structure (JSON)

```json
{
  "data": {
    "directory": ".pando"
  },
  "providers": {
    "anthropic": {
      "apiKey": "your-api-key",
      "disabled": false
    }
  },
  "agents": {
    "coder": {
      "model": "claude-3.7-sonnet",
      "maxTokens": 5000
    }
  },
  "shell": {
    "path": "/bin/bash",
    "args": ["-l"]
  },
  "mcpServers": {},
  "lsp": {},
  "debug": false,
  "autoCompact": true
}
```

### Configuration File Structure (TOML)

```toml
[data]
directory = ".pando"

[providers.anthropic]
apiKey = "your-api-key"
disabled = false

[agents.coder]
model = "claude-3.7-sonnet"
maxTokens = 5000

[shell]
path = "/bin/bash"
args = ["-l"]

debug = false
autoCompact = true
```

## Usage

```bash
# Start Pando
pando

# Start Pando as an MCP server (stdio + HTTP /mcp)
pando mcp-server

# Start with debug logging
pando -d

# Start with a specific working directory
pando -c /path/to/project

# Run a single prompt in non-interactive mode
pando -p "Explain the use of context in Go"

# Get response in JSON format
pando -p "Explain the use of context in Go" -f json

# Disable one MCP transport if needed
pando mcp-server --no-stdio
pando mcp-server --no-http
```

## Custom Commands

Custom commands are predefined prompts stored as Markdown files:

1. **User Commands** (prefixed with `user:`): `$XDG_CONFIG_HOME/pando/commands/` or `$HOME/.pando/commands/`
2. **Project Commands** (prefixed with `project:`): `<PROJECT DIR>/.pando/commands/`

## Architecture

- **cmd**: Command-line interface using Cobra
- **internal/app**: Core application services
- **internal/config**: Configuration management
- **internal/db**: Database operations and migrations
- **internal/llm**: LLM providers and tools integration
- **internal/tui**: Terminal UI components and layouts
- **internal/logging**: Logging infrastructure
- **internal/message**: Message handling
- **internal/session**: Session management
- **internal/lsp**: Language Server Protocol integration

## Acknowledgments

Pando is a fork of [OpenCode](https://github.com/digiogithub/pando), originally created by [Kujtim Hoxha](https://github.com/kujtimiihoxha).

Special thanks to:
- [@isaacphi](https://github.com/isaacphi) - For the [mcp-language-server](https://github.com/isaacphi/mcp-language-server) project
- [@adamdottv](https://github.com/adamdottv) - For the design direction and UI/UX architecture
- The broader open source community

## Tasks

### tag

Genera una nueva tag

interactive:true

```bash
git tag --sort=creatordate | tail -n 5
git tag $(gum input)
git push origin --tags
```

### build-webui

Compiles the webui

```bash
# Build embedded web-ui assets
cd web-ui && bun install && bun run build:embedded && cd ..
```

### build-desktop

Compiles the desktop wails wrapper

```bash
make desktop-build
make desktop-embed
```

### build

Compiles the binary

requires: build-webui, build-desktop

```bash
# Get version from last git tag
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "dev")
#go build -ldflags "-X github.com/digiogithub/pando/internal/version.Version=$VERSION" -o pando .
make build
rm -f *.log
```

### build-and-copy

Compile the working binary and copy to the binary path `~/bin/`.

requires: build

```bash
rm -f ~/bin/pando
upx -1 pando
cp pando ~/bin/pando
rm -f *.upx
```

### release

Compiles the binaries for the different platforms (Linux x64, Windows x64, macOS aarch64) and zip them into `dist/`.

```bash
# Create dist folder
mkdir -p dist

# Build embedded web-ui assets
cd web-ui && bun install && bun run build:embedded && cd ..

# Get version from last git tag
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "dev")

# Linux x64
make release-linux-amd64
# Linux arm64
make release-linux-arm64

# Windows x64
make release-windows-amd64

# macOS aarch64
make release-darwin-arm64
# macOS x64
make release-darwin-amd64

ssh mac-mini-de-digio "export PATH=\$PATH:/usr/local/bin:~/.bun/bin:/opt/homebrew/bin/:~/go/bin && cd ~/www/MCP/Pando/pando && git pull origin main && git fetch origin --tags && rm dist/*.zip && xc build && make release-darwin-arm64 && make release-darwin-amd64"
scp mac-mini-de-digio:~/www/MCP/Pando/pando/dist/*.zip dist/

echo "Release builds completed in dist/"
```

## ACP Support

Pando supports the [Agent Client Protocol](https://agentclientprotocol.com), allowing it to be used directly in compatible editors as an AI coding assistant.

### Quick Start

Run Pando as an ACP server (stdio mode, for editors):

```bash
pando acp
```

### Editor Configuration

#### VS Code

Add to your `settings.json`:

```json
{
  "agent_servers": {
    "Pando": {
      "command": "pando",
      "args": ["acp"]
    }
  }
}
```

#### Zed

Add to `~/.config/zed/settings.json`:

```json
{
  "agent_servers": {
    "Pando": {
      "command": "pando",
      "args": ["acp"]
    }
  }
}
```

#### JetBrains IDEs

Add to your `acp.json`:

```json
{
  "agent_servers": {
    "Pando": {
      "command": "/path/to/pando",
      "args": ["acp"]
    }
  }
}
```

### ACP Configuration

Configure ACP behavior in `.pando.toml`:

```toml
[acp]
enabled = true
max_sessions = 10
idle_timeout = "30m"
log_level = "info"
auto_permission = false  # set true for CI/batch environments
```

### Management Commands

```bash
# Start ACP server (stdio, for editors)
pando acp

# Start with explicit flags
pando acp start --debug --cwd /path/to/project

# Check server status (HTTP mode)
pando acp status

# List active sessions
pando acp sessions

# View server statistics
pando acp stats

# Stop server
pando acp stop
```

### Client Examples

Examples are provided for:
- Go client: `examples/acp-client/go/`
- Python client: `examples/acp-client/python/`

### Documentation

For comprehensive documentation, see [docs/acp-server.md](docs/acp-server.md)

Features:
- Stdio transport for editor subprocess mode
- HTTP+SSE transport for real-time updates
- Multiple concurrent sessions
- Security boundaries (path validation)
- Permission system for tool execution
- Auto-approval mode for trusted environments

## License

Pando is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
