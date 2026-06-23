# 木 Pando

> **Fork of [OpenCode](https://github.com/digiogithub/pando)** by Kujtim Hoxha.
> Maintained by **José F. Rives**.


<img alt="pando mascot" title="pando mascot" src="https://github.com/digiogithub/pando/blob/main/assets/pando_mascot-fs8.png?raw=true" width="300" style="margin: 30px auto">


A powerful terminal-based AI assistant for developers, providing intelligent coding assistance directly in your terminal.

## Overview

Pando is a Go-based CLI application that brings AI assistance to your terminal. It provides a TUI (Terminal User Interface), PWA WebUI and desktop application for interacting with various AI models to help with coding tasks, debugging, and more.

## Features

- **Interactive TUI**: Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) for a smooth terminal experience
- **Multiple AI Providers**: Support for Github copilot, OpenAI, Anthropic Claude, Google Gemini, AWS Bedrock, Groq, Azure OpenAI, Ollama, Llama.cpp, and OpenRouter. Also any OpenAI compatible provider, including self-hosted models with the `local` provider.
- **Session Management**: Save and manage multiple conversation sessions
- **Tool Integration**: AI can execute commands, search files, and modify code
- **Vim-like Editor**: Integrated editor with text input capabilities
- **Persistent Storage**: SQLite database for storing conversations and sessions
- **LSP Integration**: Language Server Protocol support for code intelligence
- **File Change Tracking**: Track and visualize file changes during sessions
- **External Editor Support**: Open your preferred editor for composing messages
- **Named Arguments for Custom Commands**: Create powerful custom commands with multiple named placeholders
- **Configuration**: Supports both JSON and TOML configuration files
- **PWA WebUI**: Access Pando through a web interface for a more visual experience in React (embedded into the binary). Access remotely or locally via browser. Perfect team with Tailscale or Zerotier distributed vpn for remote access.
  - **Code Editor**: Using the same component to VisualStudio Code's editor, with syntax highlighting and LSP features.
  - **File Explorer**: Browse and search project files with an integrated file explorer.
  - **Session History**: View and manage past conversations in a dedicated session history panel.
  - **Real-time Updates**: See AI responses and tool outputs in real-time with WebSockets.
- **Desktop Application**: Run Pando as a native desktop app using Wails (embedded into the binary)
- **Agent Client Protocol (ACP)**: Use Pando as an AI coding assistant directly in compatible editors like VS Code, JetBrains IDEs, and Zed
- **LLM Proxy Support**: Configure Pando to be used as a proxy for LLM API requests, allowing you to route requests through Pando for additional processing or logging
- **MCP Server**: Start Pando as an MCP server to allow external clients to connect and interact with it using the Model Client Protocol
- **ICP and inter-process communication**: Pando autodiscover other Pando instances running on the same machine and can communicate with them using a custom inter-process communication protocol, allowing for distributed AI assistance across multiple terminals or projects.
- **Subagent Delegation (conclusions + agent-loop resurrection)**: When the agent spawns delegated subagent tasks (via the mesnada orchestrator), each subagent ends with a thin `<pando:conclusion>` block; Pando fills in the launch metadata (task id, engine, model, project, parent session) and feeds the result back to the parent loop — either injected into a still-running loop (Case A) or by resurrecting an idle one (Case B). Includes a non-blocking `mesnada_await` primitive and anti-fork-bomb caps. Default-off; toggle from the TUI/WebUI settings (`Mesnada → Delegation`).
- **Warm per-project instance reuse**: Optionally route a delegated task whose project is already open to its running ("warm") per-project ACP instance instead of cold-spawning a CLI subprocess — capturing the conclusion over the wire. A single warm instance serves several delegated agent loops in parallel (each in its own session, bounded by `Max Concurrent`); when at that cap a further delegated task normally cold-spawns, but with `Warm Queue Depth` set it instead waits in a bounded queue for a free slot (`0` keeps the cold-spawn-at-cap behaviour). The Projects panel shows live delegated-loop counts and an `auto` badge for router-started instances, and stopping an instance cancels its in-flight loops (which then fall back to the cold path). Router-started warm instances are tagged so a later user activation from the Projects panel promotes them to user-focused; with `Warm Instance Idle Timeout` set, ones that stay idle (no in-flight loops, not the active project) are automatically stopped so they don't leak. Default-off; enable `Reuse Warm Instances` (and optionally disable `Auto-Start Warm Instance` for reuse-only, or set `Warm Instance Idle Timeout` such as `10m` to auto-GC idle instances — `0` disables it) under `Mesnada → Delegation`, or set `PANDO_DELEGATION_REUSE_WARM_INSTANCES` / `PANDO_DELEGATION_AUTO_START_WARM_INSTANCE` / `PANDO_DELEGATION_WARM_INSTANCE_IDLE_TIMEOUT`. To aim a delegated task at a specific registered project, pass a `project` argument (its id, display name, or directory path) to the `mesnada_spawn_agent` / `spawn_agent` tool — the task is routed to that project's warm instance and its `work_dir` defaults to the project's directory; an unknown reference returns an error listing the known projects. The Orchestrator dashboard (TUI and WebUI) surfaces live delegation telemetry — warm-reuse hit rate, warm hits/failures, cold fallbacks, cap rejections, and resurrection / live-injection counts — also exposed at `GET /api/v1/orchestrator/delegation/metrics`.
- **Hot-peer IPC delegation (external instances as warm targets)**: Optionally let a delegated task whose project is served by an *external* instance — e.g. one launched by an editor's ACP integration, which has no stdio pipe to this process — run inside that peer over the localhost IPC bus instead of cold-spawning a CLI, capturing the conclusion synchronously. It is two-sided opt-in and default-off: the caller enables `Allow External Warm Targets` and the target instance enables `Accept Delegations` (env `PANDO_DELEGATION_ALLOW_EXTERNAL_WARM_TARGETS` / `PANDO_DELEGATION_ACCEPT_DELEGATIONS`), both under `Mesnada → Delegation`. Pando never stops or kills an editor's instance; the delegated run uses a fresh ephemeral session isolated from the user's active one, on cancellation it best-effort interrupts only that session, and a peer that hasn't opted in (or is unreachable / too old — capability is negotiated over `instance.ping` and fails closed) simply falls back to the cold path. Delegations served this way are counted as `external_hits` in the delegation telemetry.

## Installation

### Install from binaries

Installer script for windows (copy into powershell)

```
iex (irm https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install-windows.ps1)
```

Installer in linux

```
curl -fsSL https://raw.githubusercontent.com/digiogithub/pando/main/scripts/install-linux.sh | bash
```

In OSX download the [release](https://github.com/digiogithub/pando/releases) `.pkg` for your architecture — `pando-<version>-darwin-arm64.pkg` (Apple Silicon) or `pando-<version>-darwin-x64.pkg` (Intel). The installer places `Pando.app` (with the embedded desktop wrapper, icons and the `/usr/local/bin/pando` launcher) under `/Applications`.

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

### Language Servers (LSP)

Pando activates language servers **on demand**. It ships a built-in catalogue of
presets (gopls, pyright, typescript-language-server, rust-analyzer, clangd,
jdtls, and more). When you edit, view, or save a file, Pando looks at its
extension and starts the matching server from the catalogue **only if its binary
is found on `PATH`** — exactly like editors such as OpenCode do. This means a
non-Go project never spins up `gopls`, and each language server starts the first
time a file of its kind is touched, not at boot.

Activation is triggered from three places: the agent's file tools (edit / write
/ view / patch), the TUI editor and file-tree, and a lightweight workspace
watcher that catches edits made by external tools. Servers whose binary is
missing are remembered and not retried.

You only need `[LSP]` entries to **override, extend, or disable** a catalogue
server, or to add one Pando doesn't know about:

```toml
# Turn on-demand activation off entirely (servers then start only if Autostart).
LSPAutoActivate = true

# Override a preset's command / args, or add your own server.
[LSP.gopls]
Command = "gopls"
Args = []
Languages = ["go"]
Disabled = false   # set true to keep this language server from ever starting
Autostart = false  # set true to eagerly start it at boot instead of on demand
```

| Setting           | Scope        | Meaning                                                                 |
| ----------------- | ------------ | ----------------------------------------------------------------------- |
| `LSPAutoActivate` | global       | When `true` (default), start servers on demand by file type.            |
| `Disabled`        | per-server   | Never start this server, even on demand.                                |
| `Autostart`       | per-server   | Eagerly start this server at boot instead of waiting for a matching file. |

The **Settings → LSP** page shows each configured server's command, its
`installed` / `not installed` status, and an `Autostart` toggle, plus the
catalogue presets you can add (annotated with their install status).

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

# Check for a newer compatible GitHub release
pando update --check

# Update the current binary in place
pando update

# Disable one MCP transport if needed
pando mcp-server --no-stdio
pando mcp-server --no-http
```

When Pando starts from a released semantic-version build, it also performs a short background
update check and prints a notice if a newer compatible release is available.

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
VERSION=$(git describe --tags 2>/dev/null || echo "dev")
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

interactive:true

```bash
# Create dist folder
mkdir -p dist

# Build embedded web-ui assets
cd web-ui && bun install && bun run build:embedded && cd ..

# Get version from last git tag
VERSION=$(git describe --tags 2>/dev/null || echo "dev")

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

echo "Run in osx terminal the command:"
echo "    export KEYSTORE_PASS=<temp_keychain_password> SIGNING_CERT_PASSWORD=<p12_password> NOTARY_APPLE_ID=<apple_id> NOTARY_TEAM_ID=<team_id> NOTARY_APP_PASSWORD=<app_specific_password> && xc release-osx"
echo
bash -c 'read -n 1 -s -r -p "When the command finish, press any key to continue..."'
echo


scp mac-mini-de-digio:~/www/MCP/Pando/pando/dist/*.zip dist/

echo "Release builds completed in dist/"
```

For macOS signing the flow is split in two:

- **Embedded wrapper (standalone CLI builds).** `make desktop-embed` now signs the `pando-desktop` wrapper with the Developer ID Application identity (hardened runtime, **no notarization**) *before* it is baked into the CLI via `go:embed`. Signing the wrapper before embedding is what stops macOS from killing it when `pando desktop` extracts and runs it from a temp dir. The Mach-O signature travels inside the embedded bytes and is preserved on extraction.
- **Packaged `.app`/`.pkg` (per architecture).** `scripts/build-macos-app` builds one bundle per architecture and ships `pando-desktop` as a **real file inside `Pando.app/Contents/MacOS/`** next to `pando-bin` — it is no longer extracted from the embed at runtime. The runtime (`internal/desktop/launcher.go`) prefers that on-disk sibling, so the wrapper runs from its signed & notarized location. Each bundle is signed inside-out (`pando-bin`, then `pando-desktop`, then `Pando.app` with `codesign --deep -o runtime`) and packaged into `pando-<version>-darwin-arm64.pkg` and `pando-<version>-darwin-x64.pkg`.

The installer package must be signed separately with `productsign` and a `Developer ID Installer` identity via `PKG_SIGN_IDENTITY`; reusing the application certificate for the `.pkg` makes Gatekeeper report the package as unsigned or invalid. `scripts/build-macos-app` verifies every bundle and installer after signing with `codesign`, `spctl`, and `pkgutil --check-signature`, and notarizes/staples each `.pkg` via `notarytool` (profile `NOTARY_PROFILE`). For unattended SSH / CI runs, create a temporary signing keychain from `~/DIGIO_Software_Signing_Keys` with `scripts/setup-macos-signing-keychain` and export `MACOS_SIGN_KEYCHAIN_PATH`, `MACOS_SIGN_KEYCHAIN_PASSWORD`, `SIGNING_IDENTITY`, and `PKG_SIGN_IDENTITY` before running the release steps.


### release-osx

Compiles the binaries for the different platforms (Linux x64, Windows x64, macOS aarch64) and zip them into `dist/`.

interactive:true
Inputs: KEYSTORE_PASS, SIGNING_CERT_PASSWORD, NOTARY_APPLE_ID, NOTARY_TEAM_ID, NOTARY_APP_PASSWORD

```bash
export PATH=$PATH:/usr/local/bin:~/.bun/bin:/opt/homebrew/bin/:~/go/bin && cd ~/www/MCP/Pando/pando && git pull origin main && git fetch origin --tags && rm -rf dist && mkdir -p dist && export KEYSTORE_PASS='$KEYSTORE_PASS' && export SIGNING_CERT_PASSWORD='$SIGNING_CERT_PASSWORD' && export NOTARY_APPLE_ID='$NOTARY_APPLE_ID' && export NOTARY_TEAM_ID='$NOTARY_TEAM_ID' && export NOTARY_APP_PASSWORD='$NOTARY_APP_PASSWORD' && NOTARYTOOL_STORE_CREDENTIALS=1 bash scripts/setup-macos-signing-keychain && export MACOS_SIGN_KEYCHAIN_PATH="$HOME/Library/Keychains/pando-build.keychain-db" && export MACOS_SIGN_KEYCHAIN_PASSWORD="$KEYSTORE_PASS" && xc build && make release-darwin-arm64 && make release-darwin-amd64 && bash scripts/build-macos-app


echo "Release builds completed in dist/"
```

>
> Quick remote smoke test before the full release:
>
> ```bash
> export KEYSTORE_PASS='$KEYSTORE_PASS'
> export SIGNING_CERT_PASSWORD='$SIGNING_CERT_PASSWORD'
> bash scripts/setup-macos-signing-keychain
> security find-identity -v -p codesigning "$HOME/Library/Keychains/pando-build.keychain-db"
> ```

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
