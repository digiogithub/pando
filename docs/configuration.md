# Configuration

Pando looks for configuration in the following locations:

- `$HOME/.pando.json` or `$HOME/.pando.toml`
- `$XDG_CONFIG_HOME/pando/.pando.json` or `$XDG_CONFIG_HOME/pando/.pando.toml`
- `./.pando.json` or `./.pando.toml` (local directory)

Both JSON and TOML formats are supported. Pando auto-detects the format based on the file extension.

## Environment Variables

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
| `PANDO_SANDBOX`            | Host command sandbox override: `off`, `workspace-write`, `read-only`, `strict` (see [sandbox.md](sandbox.md)) |
| `PANDO_TELEMETRY_TOKEN`    | Ingest token for a self-hosted telemetry sink (see [telemetry.md](telemetry.md)) |
| `PANDO_TELEMETRY_ENDPOINT` | Custom telemetry endpoint; requires `PANDO_TELEMETRY_TOKEN` |
| `PANDO_BETTERSTACK_TOKEN`  | Build-time Better Stack ingest token (maintainer builds only) |
| `PANDO_DELEGATION_REUSE_WARM_INSTANCES` | Route delegated tasks to warm per-project instances (see [delegation.md](delegation.md)) |
| `PANDO_DELEGATION_AUTO_START_WARM_INSTANCE` | Auto-start a warm instance when none is running |
| `PANDO_DELEGATION_WARM_INSTANCE_IDLE_TIMEOUT` | Stop idle router-started warm instances after this duration (`0` disables) |
| `PANDO_DELEGATION_ALLOW_EXTERNAL_WARM_TARGETS` | Caller side of hot-peer IPC delegation |
| `PANDO_DELEGATION_ACCEPT_DELEGATIONS` | Target side of hot-peer IPC delegation |

## Configuration File Structure (JSON)

```json
{
  "data": {
    "directory": ".pando/data"
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

## Configuration File Structure (TOML)

```toml
[data]
directory = ".pando/data"

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

## Decision model

Features that need a small, fast classifier (model auto mode, persona auto-select and the context
relevance filter) share one provider configured in the top-level `[DecisionModel]` block:

```toml
[DecisionModel]
TimeoutMs = 0             # 0 = 1500 ms (ollama) / 3000 ms (remote)

[DecisionModel.Router]
Provider  = 'ollama'      # ollama | typesafe | custom
Model     = 'tev1:0.8b'   # ollama pull tev1:0.8b (Ollama >= 0.35)
KeepAlive = '30m'
```

Older files that still carry `[ModelAutoMode.Router]` are migrated into this block on load. See
[decision-model.md](decision-model.md) for the providers, REST endpoints, privacy notes and the
relevance filter options under `[Remembrances]`.

## Data Directory and Legacy Database Migration

Pando keeps its SQLite database at `<Data.Directory>/pando.db`, which for a project
initialized by Pando is `.pando/data/pando.db`. Older versions stored it directly at
`.pando/pando.db`; that path is obsolete.

On every startup — before any database connection is opened — Pando reconciles the two
paths:

- If only the obsolete `.pando/pando.db` exists, it is **moved** to the configured data
  directory together with its SQLite sidecars (`-wal`, `-shm`, `-journal`), so no
  committed WAL transaction is lost.
- If the current database already exists, it is **authoritative**: it is never modified,
  and the obsolete files are deleted.
- If migration or cleanup fails, startup fails instead of silently creating a fresh,
  empty database. Nothing is overwritten in any case.

The migration is idempotent and a no-op for configurations that still set
`Data.Directory` to `.pando` (there the obsolete and current paths are the same file) and
for projects that never used the old path. Don't start a second Pando instance while the
first migration is running.

## Related configuration guides

- [Language servers (LSP)](lsp.md)
- [MCP server authentication](mcp-authentication.md)
- [Host command sandbox](sandbox.md)
- [Output compression filters](output-filters.md)
- [Remote diagnostics (telemetry)](telemetry.md)
- [Web UI access control](webui.md)
- [Model catalog (models.dev)](model-catalog.md)
- [Decision model](decision-model.md) and [Model auto mode](model-auto-mode.md)
- [Model auto mode](model-auto-mode.md)
- [Knowledge Base](knowledge-base.md)
