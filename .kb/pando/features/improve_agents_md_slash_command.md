---
created_at: 2026-06-30T07:22:39.436146813Z
updated_at: 2026-06-30T07:22:39.436146813Z
tags:
    - feature
    - slash-command
    - agents-md
    - documentation
---
# Feature: `/improve-agents-md` slash command + canonical AGENTS.md ruleset

## What changed
Added a new built-in slash command `/improve-agents-md` that launches a normal
agent turn whose task is to **create, restructure, or reinforce** the working
project's `AGENTS.md` so it carries a canonical set of **MANDATORY** operating
rules for AI agents. The command works uniformly across the three surfaces (TUI,
Web UI, ACP), mirroring the existing `/ponytail` and `/db-compact` precedent.

It also introduces the canonical ruleset itself (project-agnostic) covering:
context gathering BEFORE any task (`kb_search_documents` / `hybrid_search_remembrances`
+ code-index tools `code_find_symbol`/`code_get_symbols_overview`/`code_hybrid_search`/
`code_search_pattern`), external research (Context7 `c7_resolve_library_id` +
`c7_get_library_docs`; web search `google_search`/`brave_search`/`exa_search` +
`fetch`; browser tools for frontend work), MANDATORY planning before non-trivial
work, small verified increments, and MANDATORY documentation of every change via
`kb_add_document`.

## How it works
The command is a **prompt-expansion** command: it expands into a full instruction
prompt (built from the embedded canonical template) and runs as an ordinary agent
turn, so it streams, steers, and persists like any normal message. The agent is
told to gather context first (search KB, read the existing AGENTS.md/CLAUDE.md),
decide create-vs-reinforce, insert/replace the managed block delimited by the
sentinels `<!-- pando:agents-md:begin -->` / `<!-- pando:agents-md:end -->`
(so re-runs replace in place without losing project-specific content), verify,
and finally document the change.

## Files / symbols touched
- **New** `internal/agentsmd/template.md` — the canonical MANDATORY ruleset
  (delimited by the begin/end sentinels).
- **New** `internal/agentsmd/agentsmd.go` — `//go:embed template.md` →
  `CanonicalTemplate`; `BeginMarker`/`EndMarker` consts; `Prompt(extra string)`
  builds the agent task prompt (appends per-run user guidance when non-empty).
- **New** `internal/agentsmd/agentsmd_test.go` — embed + Prompt content tests.
- `internal/commands/registry.go` — added `{Name:"improve-agents-md", AcceptsArgs:true}`
  to `BuiltinCommands()` (drives TUI/WebUI completion via `/api/v1/commands` and
  `Parse`). Web-UI picks it up automatically (no frontend change).
- `internal/mesnada/acp/session_state.go` — `improveAgentsMdCommandToken`.
- `internal/mesnada/acp/slash_commands.go` — `slashCommandImproveAgentsMd` kind +
  spec (with input hint "optional extra guidance").
- `internal/mesnada/acp/goal_commands.go` — dispatch case +
  `processImproveAgentsMdCommand` (sends a notice then
  `processPromptWithAgent(agentsmd.Prompt(extra))`).
- `internal/api/handlers_chat.go` — `case "improve-agents-md"` in
  `handleSlashCommandStream` submits `agentsmd.Prompt(cmdArgs)` to `bgRunner` and
  streams (mirrors the goal-start path).
- `internal/tui/page/chat.go` — `expandImproveAgentsMdCommand` expands the command
  into the full prompt at the top of `sendMessage`, then falls through to the
  normal Run/Steer flow.
- `internal/mesnada/acp/agent_pando_test.go` — updated the available-commands
  count (8→9) and inclusion/hint assertions for the new command.

## Why
The user wanted a general, reusable, MANDATORY instruction set for any code
project's AGENTS.md (how to document, how to recover context with concrete tools:
KB, code-index, web search, Context7, browser), plus a one-shot command to inject
or reinforce those rules into an existing or new AGENTS.md.

## How it was verified
- `go build ./...` — clean.
- `go vet` on agentsmd/acp/api/tui-page — clean.
- `go test ./internal/agentsmd/` — pass (embed + Prompt tests).
- `go test ./internal/mesnada/acp/` — pass (updated available-commands test).

## Notes / future work
- No new config knob and no i18n strings (command output is agent-driven; the
  static notice text is English like goal/compact). A settings toggle was not
  needed.
- The project's own `AGENTS.md` was left as-is (it already carries equivalent
  rules); run `/improve-agents-md` to fold the canonical managed block into it or
  any other project.
