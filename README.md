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
- **Output Compression (token reduction)**: RTK-style filtering of verbose command output (test runners, builds, installers, linters) before it reaches the model — typically 60-90% fewer tokens. Declarative, hot-reloadable TOML filters plus native structured parsers (`go test -json`, `golangci-lint`, `tsc`). Fail-safe and on by default; never drops errors. Add project-local filters in `.pando/filters.toml` and validate them with `pando filter test`. See [docs/output-filters.md](docs/output-filters.md).
- **Caveman Output Brevity (opt-in output-token reduction)**: `/caveman [lite|full|ultra]` makes Pando answer with fewer words — no filler, no restatement, no redundant summaries — while code, commands, errors, test output, reasoning quality, tool use and verification stay intact. `/caveman-finish` ends it for the session, and `[Caveman] DefaultMode` sets a global default. Off by default; it reduces output tokens only. See [Caveman output brevity](#caveman-output-brevity-opt-in).
- **Superpowers Mode (opt-in workflow discipline)**: `/superpowers` routes long or risky work through explicit gates — understand, design and get approval, write a risk-ordered plan, implement test-first, verify with real command output — and `/superpowers-finish` closes it with a verified report. Per-session and off by default; it never performs a git operation on its own. See [Built-in Slash Commands](#built-in-slash-commands).
- **Learning Mode (opt-in knowledge capture)**: `/learning` makes Pando lean on its knowledge base and memory — recover prior context before acting, ask you the questions that matter, document what it discovers, and mark superseded docs outdated instead of leaving stale ones behind. `/learning-finish` consolidates what was learned back into the KB. Per-session, off by default, and never a git side effect. See [Built-in Slash Commands](#built-in-slash-commands).
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
  - **Interactive Terminal**: A real shell running in a PTY, streamed to xterm.js over a WebSocket — the same experience as the TUI terminal, with full-screen programs (`vim`, `htop`, `less`), colors, job control and Ctrl+C. Tabs are independent shells that keep running in the background, and a page reload reattaches to them instead of losing them. **Security:** like the TUI terminal, this is an unrestricted shell — anyone who can reach the WebUI *and* holds its auth token gets interactive shell access as the user running Pando. The server binds to `localhost` by default; think carefully before exposing it with `--host 0.0.0.0` (prefer a VPN such as Tailscale/ZeroTier). The older single-command endpoint (`POST /api/v1/terminal/exec`), which keeps a dangerous-command filter, remains as a fallback for clients that cannot use WebSockets.
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

### Configuration File Structure (TOML)

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

### Data Directory and Legacy Database Migration

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

#### Built-in preset catalogue

| Preset name                  | Command                       | Languages                                              |
| ----------------------------- | ------------------------------ | ------------------------------------------------------- |
| `gopls`                       | `gopls`                        | `.go`                                                   |
| `rust-analyzer`               | `rust-analyzer`                | `.rs`                                                   |
| `typescript-language-server`  | `typescript-language-server`   | `.ts` `.tsx` `.js` `.jsx` `.mjs` `.cjs`                  |
| `pyright`                     | `pyright-langserver`           | `.py` `.pyi`                                            |
| `pylsp`                       | `pylsp`                        | `.py` `.pyi`                                            |
| `clangd`                      | `clangd`                       | `.c` `.cc` `.cpp` `.cxx` `.c++` `.h` `.hh` `.hpp` `.m` `.mm` |
| `lua-language-server`         | `lua-language-server`          | `.lua`                                                  |
| `bash-language-server`        | `bash-language-server`         | `.sh` `.bash` `.zsh` `.ksh`                              |
| `yaml-language-server`        | `yaml-language-server`         | `.yaml` `.yml`                                          |
| `json-language-server`        | `vscode-json-language-server`  | `.json` `.jsonc`                                        |
| `html-language-server`        | `vscode-html-language-server`  | `.html` `.htm`                                          |
| `css-language-server`         | `vscode-css-language-server`   | `.css` `.scss` `.sass` `.less`                           |
| `marksman`                    | `marksman`                     | `.md` `.markdown`                                       |
| `jdtls`                       | `jdtls`                        | `.java`                                                 |
| `solargraph`                  | `solargraph`                   | `.rb` `.rake`                                           |
| `zls`                         | `zls`                          | `.zig`                                                  |
| `kotlin-language-server`      | `kotlin-language-server`       | `.kt` `.kts`                                            |
| `intelephense`                | `intelephense`                 | `.php`                                                  |
| `omnisharp`                   | `omnisharp`                    | `.cs`                                                   |
| `dartls`                      | `dart`                         | `.dart`                                                 |
| `elixir-ls`                   | `elixir-ls`                    | `.ex` `.exs`                                            |

`json-language-server`, `html-language-server`, and `css-language-server` come
from the npm package `vscode-langservers-extracted`. `elixir-ls` matches the
shim name installed by Mason/asdf; a manual upstream ElixirLS release instead
ships `language_server.sh`, so override `Command` under `[LSP.elixir-ls]` if
you installed it that way.

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

# Convert a document (docx, pdf, xlsx, pptx, html, csv, epub, …) to Markdown
pando convert report.docx              # prints Markdown to stdout
pando convert data.xlsx -o data.md     # writes to a file
pando convert https://example.com/page # converts a web page
pando convert --list-formats           # list supported input formats
```

When Pando starts from a released semantic-version build, it also performs a short background
update check and prints a notice if a newer compatible release is available.

### WebUI Access (protecting a remotely exposed server)

`pando serve` and `pando app` expose the agent over HTTP — including the bash tool, the
terminal and file writes — so a server bound to anything other than localhost must be
protected. **WebUI Access** adds HTTP Basic Auth in front of the API:

```bash
pando app --host 0.0.0.0   # reachable from the network: credentials are required
pando app                  # bound to localhost: credentials are never asked for
```

Manage it from **Settings → Services → WebUI Access** in the Web UI: switch it on and add
one or more username/password pairs. The rules are:

- Only the `/api/` surface is guarded; static assets stay public so the PWA service worker
  can precache them.
- Credentials are only demanded once the server is exposed, i.e. started on a non-loopback
  host. Bound to localhost the setting stays inert and local development is unaffected.
- Where the request comes from makes no difference: on a `0.0.0.0` bind the browser on your
  own machine is asked to sign in too, because the port is open to the network either way.
- Passwords are stored **age-encrypted** in your config file (`age1:` prefix), exactly like
  provider API keys, using the key set in `~/.config/pando/keys/`.
- Access control cannot be enabled without at least one user, and deleting the last user
  turns it off, so the server can never demand credentials that do not exist.
- The Web UI shows its own login dialog; CLI clients get a standard challenge:

```bash
curl -u admin:secret http://my-host:9999/api/v1/token
```

```toml
[Server]
Enabled     = true
Host        = '0.0.0.0'
Port        = 9999

[Server.BasicAuth]
Enabled = true

[[Server.BasicAuth.Users]]
Username = 'admin'
Password = 'age1:...'   # written by the panel, never by hand
```

### Model pricing and capabilities (models.dev)

Most providers do not report per-token pricing (and often not the real context window) in
their model-listing API, so the session **Cost** shown in the TUI and WebUI sidebars stayed
empty for those models. Pando completes that metadata from the community catalog
[models.dev](https://github.com/anomalyco/models.dev): input/output/cache prices, context and
output limits, reasoning and attachment support, description and training cutoff.

- The catalog is downloaded **once per instance**, lazily, and cached in
  `~/.pando_modelsdev.json` for 24 h; a stale cache is reused when the network is unavailable.
- It **only fills what is missing**. Anything the provider (or Pando's curated catalogue)
  already reported stays authoritative, and capability flags are only ever raised.
- Any failure — offline, HTTP error, unknown provider or model — is silent: models keep exactly
  the behaviour they had before, with no cost displayed.
- Local runtimes (Ollama, llama.cpp) are deliberately excluded: they cost nothing to run, and
  importing a hosted price for a same-named open-weights model would invent a cost.

The model selectors (TUI dialog and WebUI switcher/combobox) show the resulting
`200K ctx · $3/$15 per 1M · cutoff 2025-01` line, and the badges are derived from the real
price instead of guessing from the model name.

Toggle it in **Settings → General → Model Catalog (models.dev)** (TUI and WebUI), or in the
config file:

```toml
[ModelsDev]
Enabled = true   # default; set to false to never contact models.dev
```

The switch is live: turning it off stops the catalog from being consulted immediately, and
turning it back on lets the next model refresh load it without restarting Pando.

### Document conversion in the Knowledge Base

Pando converts rich documents to Markdown using the pure-Go
[`conductor-oss/markitdown`](https://github.com/conductor-oss/markitdown) library (no CGO; PDF
via PDFium compiled to WebAssembly). Beyond the `pando convert` command, any supported document
dropped inside the Knowledge Base directory (`[Remembrances] KBPath`) is **converted on the fly
and indexed** with its Markdown chunks, while the indexed document keeps referencing the
**original file** (its `source_path`, `source_format` and a `converted` flag are stored in
metadata). Supported KB document formats: `.pdf .docx .pptx .xlsx .xls .epub .ipynb .csv .html
.htm .rss .atom`. Plain `.md` files are still indexed verbatim.

Configure it under `[Remembrances]`:

```toml
[Remembrances]
KBConvertDocuments = true            # default; convert documents in the KB folder
# KBConvertExtensions = ["docx", "pdf", "xlsx"]   # optional: override the curated set
```

### Wiki links in the Knowledge Base

KB documents link each other with `[[wiki links]]`, turning the knowledge base into a
navigable graph instead of a pile of loose files. Write `[[concept]]` or
`[[concept|display label]]` anywhere in a document's body; the target may be a full path
(`[[pando/plans/foo.md]]`), a bare name (`[[foo]]`) or an alias declared in the document's
front matter (`aliases: [...]`). Occurrences inside code fences and inline code are ignored,
so documentation that merely *shows* the syntax does not pollute the graph.

Links are resolved when they are read, not when they are written, so a link to a document
that does not exist yet is **valid on purpose**: it records a concept worth documenting
later (a "wanted concept"), and it starts resolving by itself the day that document is
created. The KB tools expose the graph:

- `kb_get_document` returns the document's outgoing links and its **backlinks**.
- `kb_search_documents` reports how connected each hit is and lists the neighbours of the
  best match, so the agent can hop instead of searching again.
- `kb_related_documents` navigates the graph — with a `file_path` it returns links,
  backlinks and scored related documents; with no arguments it lists the **wanted
  concepts**, i.e. what the knowledge base refers to but never explains.
- `kb_add_document` reports how many links it indexed and which targets are still
  undocumented.

Documents stored before the graph existed are backfilled in the background at startup;
`pando kb relink [--force]` rebuilds it on demand (it costs no embeddings and never rewrites
your markdown). Toggle the feature from the TUI/WebUI settings (`Remembrances → Wiki Links`)
or in config:

```toml
[Remembrances]
KBWikiLinks = true                   # default; index [[wiki links]] as a document graph
```

Turning it off is safe and reversible: nothing new is indexed and the tools answer exactly as
they did before the graph existed, but the links already stored survive and light up again
when you turn it back on.

### Output compression filters (token reduction)

Pando compresses verbose command output before it reaches the model, cutting token
usage on noisy tools (test runners, builds, installers, linters) by roughly 60-90%.
It is **fail-safe** (any error returns the raw output), **exit-code preserving**, and
**on by default**.

Two complementary mechanisms run at the `bash` tool boundary:

- **Native structured parsers** (first tier) for `go test -json`, `golangci-lint` and
  `tsc`, with RTK-style 3-tier degradation (structured summary → regex grep → raw).
- **Declarative TOML filters** (second tier) — an 8-stage line pipeline matched to a
  command by regex. 15 built-ins ship embedded (git, docker, cargo, go, gradle/maven,
  npm/pnpm/yarn, bun, deno, swift, pip, pytest).

Disable it (always return raw output) from the TUI/WebUI settings (`Bash → Output
Filter`) or in config:

```toml
[Bash]
OutputFilterDisabled = false                 # default; set true to turn compression off
# OutputFilterPaths = ["~/.pando/filters.toml"]   # extra user-global filter files
```

Add project-local filters in `.pando/filters.toml` (highest precedence) and validate
their inline `[[tests]]` before relying on them:

```bash
pando filter test .pando/filters.toml   # validate your authoring file
pando filter test                       # validate the built-in defaults
```

The full filter schema and authoring guide is in [docs/output-filters.md](docs/output-filters.md).

## Built-in Slash Commands

Available in the TUI, the Web UI and over ACP (editors like Zed or VS Code):

| Command | What it does |
| --- | --- |
| `/goal <objective>` (alias `/autopilot`) | Start goal mode with a persistent objective; `/goal-status`, `/goal-cancel` |
| `/compact` (alias `/summarize`) | Summarize and compact the current session |
| `/db-compact` | VACUUM the database and reclaim free space |
| `/ponytail [lite\|full\|ultra\|off]` | Toggle "lazy senior developer" mode (build less, keep the diff short) |
| `/caveman [lite\|full\|ultra]` | Answer with fewer words to spend fewer output tokens (see below) |
| `/caveman-finish` | Return the session to normal output length |
| `/superpowers [objective]` | Enable the disciplined development workflow (see below) |
| `/superpowers-finish` | Verify, report, and return to normal mode |
| `/learning [focus]` | Enable learner mode: read the KB more, document discoveries, ask questions, keep docs current (see below) |
| `/learning-finish` | Consolidate what was learned into the KB/memory and return to normal mode |
| `/improve-agents-md` | Create or reinforce AGENTS.md with the mandatory AI-agent operating rules |

### Caveman output brevity (opt-in)

`/caveman` asks Pando to say the same thing in fewer words: no greetings, no restatement of
your question, no generic transitions, no summary of what you just read. The goal is to spend
fewer **output** tokens on prose you did not ask for.

It constrains *expression*, never *work*. Code, commands, file paths, URLs, JSON/YAML/TOML,
error text, API signatures, test output, security warnings and approval questions are always
reproduced exactly — the mode may not abbreviate or paraphrase them. It may not skip
root-cause evidence, test commands and their results, or safety caveats in order to be
shorter, and it does not reduce reasoning, tool use or verification. If you ask for a detailed
explanation, you get one: a direct request always beats the brevity preference.

Three levels, from mild to extreme:

| Level | What it does |
| --- | --- |
| `lite` | Normal sentences, fewer of them: filler and restatement removed |
| `full` | Fragments over sentences: the answer, then a few lines of what matters (a bare `/caveman` picks this) |
| `ultra` | Telegraphic: the answer and nothing around it |

Replies stay in your language.

Set a global default for sessions that have not chosen a level, from the TUI/Web UI settings
(`Token Optimization → Caveman Output Brevity`) or in config:

```toml
[Caveman]
DefaultMode = ''   # default (off); or 'lite', 'full', 'ultra'
```

Scope and precedence, from strongest to weakest: **your direct instructions and project rules**
→ **the session's explicit choice** (`/caveman <level>`, or `/caveman-finish` for explicit off)
→ **`Caveman.DefaultMode`** → **off**. A session that ran `/caveman-finish` therefore stays
verbose even if the global default is on, and changing the global default never overrides a
session that already made a choice. Like Ponytail and Superpowers, the session override lives in
memory only: it does not survive a restart, while the TOML default does.

What it does *not* do: it does not reduce **input** or reasoning tokens, and the policy it
injects into the prompt has a small input cost of its own — on work whose output is already
terse, total session savings can be small or even negative. How much you save depends on the
model and the task, so Pando ships no percentage claim; measure it on your own workload.

The style levels follow [caveman](https://github.com/juliusbrussee/caveman) by Julius Brussee
(MIT), reimplemented natively in Pando as a prompt policy — no hooks, no telemetry, no stats
collection, no MCP middleware.

### Superpowers mode (opt-in)

`/superpowers` turns on a workflow policy for the current session. Long or risky work is then
routed through explicit gates instead of jumping straight to code: understand the context, present
a design and get approval, write a prioritized plan for anything multi-file (phases ordered by
risk and dependency, each with its exit criteria and verification command), implement test-first in
small increments, reproduce bugs before fixing them, and verify with real command output rather
than claims.

`/superpowers-finish` runs a closing turn that verifies the work, summarizes what changed, states
what is *not* done, and then returns the session to normal mode.

Worth knowing:

- **Opt-in and inert by default.** A session that never runs `/superpowers` behaves exactly as
  before — nothing is injected into the prompt.
- **Your instructions win.** The policy explicitly yields to direct user instructions, AGENTS.md,
  and the permission system, and it does not apply its gates to trivial or read-only requests.
- **No automatic git side effects.** Neither the mode nor the finish command will ever commit,
  merge, push, open a pull request, touch branches or worktrees, or discard work. Git stays
  user-directed.
- **Ephemeral (v1).** The mode is per-session and in-memory: it is cleared by `/superpowers-finish`
  and does not survive a restart.

It is inspired by the workflow principles of the [Superpowers](https://github.com/obra/superpowers)
plugin by Jesse Vincent (MIT), reimplemented natively in Pando — no plugin runtime, no telemetry,
no forced subagents.

### Learning mode (opt-in)

`/learning` turns on a knowledge-capture policy for the current session. With it active, Pando
treats the knowledge base and memory as a first-class part of the work rather than an afterthought:

- **Recover context first.** Before building on prior work it searches the KB (`kb_search_documents`,
  `hybrid_search_remembrances`) and reads relevant memories, instead of re-deriving what was already
  decided.
- **Ask what matters.** When a decision is genuinely yours to make, it asks — through the
  `AskUserQuestion` tool — rather than guessing.
- **Document discoveries.** Non-obvious findings are written back with `kb_add_document` (plans,
  analyses, design notes) and short durable facts with `remember`, so the next session starts ahead.
- **Keep docs honest.** When a plan, feature note, or fix write-up has been superseded, it marks the
  stale document outdated with `kb_mark_outdated` (excluded from default searches, still retrievable)
  and adds the up-to-date one, instead of leaving contradictory docs behind.

`/learning [focus]` takes an optional focus to steer what to learn; `/learning-finish` runs a closing
turn that consolidates what was learned into the KB/memory and returns the session to normal mode.

Worth knowing:

- **Opt-in and inert by default.** A session that never runs `/learning` behaves exactly as before —
  nothing is injected into the prompt.
- **Depth, not verbosity.** Learning governs how much Pando *documents and asks*, which is
  independent from output brevity: it composes cleanly with `/caveman`, which only shortens chat
  prose. You can run both at once.
- **Your instructions win.** The policy yields to direct user instructions, AGENTS.md, and the
  permission system.
- **No automatic git side effects.** Neither the mode nor the finish command commits, pushes, or
  otherwise touches git.
- **Ephemeral.** The mode is per-session and in-memory: it is cleared by `/learning-finish` and does
  not survive a restart.

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
echo "    cd ~/www/MCP/Pando/pando && xc release-osx"
echo
bash -c 'read -n 1 -s -r -p "When the command finish, press any key to continue..."'
echo


scp mac-mini-de-digio:~/www/MCP/Pando/pando/dist/*.zip dist/

echo "Release builds completed in dist/"
```


### release-osx

Builds and **notarizes** the macOS artifacts: the embedded desktop wrapper (so
`pando desktop` from a standalone binary is not killed by Gatekeeper), the loose
CLI zips (`pando-darwin-<arch>.zip`, submit-only), the `Pando-<arch>.app` bundles
(notarized + stapled) and the `.pkg` installers (notarized + stapled).

Notarization submits several artifacts to Apple with `--wait`, so this task can
take 10–30 min. Requires network access and the `pando-notary` keychain profile.

interactive:true


```zsh
export PATH=$PATH:/usr/local/bin:~/.bun/bin:/opt/homebrew/bin/:~/go/bin
cd ~/www/MCP/Pando/pando

git pull origin main
git fetch origin --tags
rm -rf dist
mkdir -p dist

# Signing identities, keychain path/password and NOTARY_PROFILE.
eval "$(cat ~/DIGIO_Software_Signing_Keys/kvagerc)"
NOTARYTOOL_STORE_CREDENTIALS=1 bash scripts/setup-macos-signing-keychain

# Ensure notarization env is exported BEFORE `xc build` so `make desktop-embed`
# notarizes the embedded wails wrapper. Defaults match the signing scripts.
export NOTARY_PROFILE="${NOTARY_PROFILE:-pando-notary}"
export MACOS_SIGN_KEYCHAIN_PATH="${MACOS_SIGN_KEYCHAIN_PATH:-$HOME/Library/Keychains/pando-build-db}"

# xc build -> build-desktop -> make desktop-embed: signs AND notarizes the
# embedded desktop wrapper (skipped non-fatally if the notary env is missing).
xc build

# Signs the loose CLI binaries with the hardened runtime (codesign-macos).
make release-darwin-arm64
make release-darwin-amd64

# Notarizes the CLI zips (submit-only) and builds + notarizes + staples the
# .app bundles and .pkg installers.
bash scripts/build-macos-app

# Verify notarization stapling on the distributable bundles/installers.
echo "== Verifying notarization staples =="
for f in dist/Pando-arm64.app dist/Pando-x64.app dist/*.pkg; do
    [ -e "$f" ] && { echo "-- $f"; xcrun stapler validate "$f" || echo "  NOT stapled: $f"; }
done

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
