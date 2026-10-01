# ACP Support

Pando supports the [Agent Client Protocol](https://agentclientprotocol.com), allowing it to be used directly in compatible editors as an AI coding assistant.

## Quick Start

Run Pando as an ACP server (stdio mode, for editors):

```bash
pando acp
```

## Editor Configuration

### VS Code

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

### Zed

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

### JetBrains IDEs

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

## ACP Configuration

Configure ACP behavior in `.pando.toml`:

```toml
[acp]
enabled = true
max_sessions = 10
idle_timeout = "30m"
log_level = "info"
auto_permission = false  # set true for CI/batch environments
```

## Management Commands

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

## Decision model

The shared decision model (`[DecisionModel]`, see [decision-model.md](decision-model.md) and `pando_setup decision-model`) shows up in ACP in
three places:

- **Model option "Auto"**: its description ends with the router in use, e.g.
  `Route each prompt to the best configured model (router: ollama/tev1:0.8b)`.
- **`/decision-model [test]`**: answers locally, without a model turn, with the decision model's
  provider, effective URL, masked API key, model, timeout, enabled consumers and a live health
  verdict. It is announced in `available_commands_update` like the other commands.
- **Context filter notice**: when the relevance filter dropped retrieved context, the turn starts
  with a plain agent message such as
  `Context filter: kept 4/9 (38 ms) — code 1/3, kb 2/5, events 1/1`. It is live only and is not
  replayed on `session/load`.

## Client Examples

Examples are provided for:
- Go client: `examples/acp-client/go/`
- Python client: `examples/acp-client/python/`

## Features

- Stdio transport for editor subprocess mode
- HTTP+SSE transport for real-time updates
- Multiple concurrent sessions
- Security boundaries (path validation)
- Permission system for tool execution
- Auto-approval mode for trusted environments
