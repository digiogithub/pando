# Agent Self-Service (`pando_setup`)

`pando_setup` is an always-on internal tool that lets Pando inspect and steer its own instance
instead of asking you for details it can look up. It behaves like a CLI: the model picks a
command and passes CLI-style arguments, and every command answers `--help`, so the tool schema
stays small and the detail is discovered on demand.

| Command | What it does |
| --- | --- |
| `help [command]` | List the commands, or print one command's usage |
| `config [section] [--search TERM]` | Read the active configuration — the same surface as the settings panels, read-only |
| `providers [--all]` | List provider accounts: type, credential kind, base URL, model count |
| `models [--provider P] [--account ID] [--search T] [--detail] [--limit N]` | List selectable models by canonical id (`copilot.gpt-5.4`), with cost, context window and models.dev metadata under `--detail` |
| `session` | This session's token usage, cost and active modes |
| `commands [--all]` | List the slash commands, marking the ones the agent may activate |
| `run <command> [args]` | Activate a slash command for the session |
| `telemetry [status\|enable\|disable\|regenerate\|level L]` | Show or change remote diagnostics (see [Remote diagnostics (telemetry)](telemetry.md)); refuses `enable`/`regenerate` when the build carries no ingest token |

Two guarantees matter here:

- **Configuration is read-only, and secrets never leave.** API keys, OAuth tokens, request
  headers and `NAME=value` environment entries are masked to their last four characters, so the
  agent can tell whether a credential is configured without ever seeing it.
- **`run` cannot take over the session.** Mode commands (`/caveman`, `/ponytail`,
  `/superpowers`, `/learning`) apply from the next turn; instruction commands (`/vulnhunt`,
  `/improve-agents-md`, and your own `user:`/`project:` commands) return their prompt for the
  agent to follow. Commands that belong to you — `/goal`, `/compact`, `/db-compact` and the
  `-finish` closing turns — are listed with the reason they are refused.

The main use for `models` is autonomous delegation: the agent can look up which model ids exist
and what they cost before spawning a subagent, instead of you naming one.
