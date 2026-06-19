---
created_at: 2026-06-19T09:37:07.773699514Z
updated_at: 2026-06-19T09:37:07.773699514Z
tags:
    - fix
    - config
    - agents
    - webui
    - tui
    - cli-assist
    - models
---
# Fix: duplicate "CLI Assist" agent in web-UI + missing agents in TUI model settings

Date: 2026-06-19

## Symptoms
1. The web-UI Settings → Agents section showed **two** CLI-assist model selectors:
   the canonical "CLI Assist" card plus a second card further down labelled
   `cliassist`.
2. The TUI equivalent settings screen offered **no** model selector for the
   cli-assist agent — nor for `context-enricher` — even though both appear in the
   web-UI.

## Root causes
- `.pando.toml` `[Agents]` contained a stray/legacy `[Agents.cliassist]` key (no
  hyphen) alongside the canonical `[Agents.cli-assist]` (`AgentCLIAssist =
  "cli-assist"`). `handleGetConfigAgents` returned every key in `cfg.Agents`, and
  the web-UI `AgentsSettings.tsx` appended any name not in its known list as an
  extra card at the bottom → phantom duplicate.
- The TUI `buildAgentsSection` (internal/tui/page/settings.go) used a hardcoded
  list of only 5 agents (coder, summarizer, task, title, persona-selector),
  omitting `cli-assist` and `context-enricher`.
- Bonus inconsistency: `cliassist.FetchCommand` read `cfg.CLIAssist.Model` (the
  separate `[cliAssist]` section, empty by default) and fell back to the coder
  model, **ignoring** `Agents[cli-assist].Model`. So the UI/`--model` selector for
  cli-assist had no effect (the `--model` override in cmd/cliassist.go writes to
  `Agents[AgentCLIAssist]`, which FetchCommand never read).

## Changes
- **internal/config/config.go**: added `KnownAgentNames` (ordered canonical set)
  + `IsKnownAgent()` as the single source of truth. In `Validate()`, prune any
  agent entry whose name is not known (removes the stray `cliassist` in-memory on
  load; persisted out on next save).
- **internal/api/handlers_config.go**: removed local `webUIAgentOrder`; GET now
  iterates `config.KnownAgentNames` only (drops the "extra map keys" loop and the
  `seen` set), so unknown agents are never surfaced.
- **internal/tui/page/settings.go**: `buildAgentsSection` now uses
  `config.KnownAgentNames` → cli-assist and context-enricher gain full TUI model /
  tokens / reasoning / thinking / auto-compact fields. The save path
  (`saveAgent`, Split on ".") already handles hyphenated names (3 parts).
- **internal/cliassist/llm.go**: `FetchCommand` model precedence is now
  `Agents[cli-assist].Model` → legacy `cfg.CLIAssist.Model` → coder model, so the
  UI selector and `--model` override actually drive the cli-assist command.
- **web-ui/src/components/settings/AgentsSettings.tsx**: removed the
  `agents.forEach` append of unknown agents (defense in depth).

## Which model is "really used" by cli-assist (answer to the user's question)
Before: `[cliAssist] Model` (empty) → coder model; the agent cards were cosmetic.
After: the **cli-assist agent** model (the canonical "CLI Assist" card, editable
in both TUI and web-UI) is used, falling back to `[cliAssist] Model` then coder.

## Verification
- `go build ./internal/config/... ./internal/api/... ./internal/tui/... ./internal/cliassist/...` — clean.
- `go test ./internal/llm/agent ./internal/api ./internal/config` — pass.
- `npx tsc --noEmit` in web-ui — clean.
- Note: requires rebuilding the web-ui assets and the Go binary to take effect.
