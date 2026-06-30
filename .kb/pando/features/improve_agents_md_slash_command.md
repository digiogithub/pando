---
created_at: 2026-06-30T07:22:39.759599186Z
updated_at: 2026-06-30T07:28:43.903885576Z
tags:
    - feature
    - slash-command
    - agents-md
    - documentation
---
# Feature: `/improve-agents-md` slash command + canonical AGENTS.md ruleset

## What changed
Added a new built-in slash command `/improve-agents-md` that launches a normal
agent turn (with the session's **currently selected model**) whose task is to
**create, restructure, or reinforce** the working project's `AGENTS.md` so it
carries a clearly-labelled set of **MANDATORY** operating rules for AI agents. The
command works uniformly across the three surfaces (TUI, Web UI, ACP), mirroring
the existing `/ponytail` and `/db-compact` precedent.

It also introduces the canonical ruleset itself (project-agnostic) covering:
context gathering BEFORE any task (`kb_search_documents` / `hybrid_search_remembrances`
+ code-index tools `code_find_symbol`/`code_get_symbols_overview`/`code_hybrid_search`/
`code_search_pattern`), external research (Context7 `c7_resolve_library_id` +
`c7_get_library_docs`; web search `google_search`/`brave_search`/`exa_search` +
`fetch`; browser tools for frontend work), MANDATORY planning before non-trivial
work, small verified increments, and MANDATORY documentation of every change via
`kb_add_document`.

## How it works — EVALUATIVE MERGE (not verbatim insertion)
The command is a **prompt-expansion** command: it expands into a full instruction
prompt and runs as an ordinary agent turn (so it streams, steers, and persists,
and uses the current model/overrides). **The canonical block is NOT pasted
verbatim.** Instead the prompt instructs the agent to:
1. Gather context first (search KB, READ the current AGENTS.md + CLAUDE.md).
2. **Evaluate the canonical clauses one by one** as an authoritative checklist of
   REQUIREMENTS, and for each: keep the project's existing wording if already
   covered (no duplication), strengthen it if weaker/vaguer than the canonical
   bar, or add it if missing — adapted to the project's actual tools/voice.
3. **Merge, don't replace**: integrate the MANDATORY clauses into the existing
   structure, preserving every project-specific section; refine a prior managed
   section in place rather than appending a second one.
4. Create a fresh AGENTS.md (project header + adapted clauses) if none exists.
5. Guarantee every MANDATORY clause is represented and labelled, with no
   project content lost; then verify and document the change.

This was changed per user feedback: the command must *evaluate and merge* the
canonical block against the current AGENTS.md respecting each MANDATORY clause,
using an AI process with the selected model — not inject a fixed block.

## Files / symbols touched
- **New** `internal/agentsmd/template.md` — the canonical MANDATORY clauses
  (delimited by the `<!-- pando:agents-md:begin/end -->` sentinels; the markers
  are stripped before the clauses are shown to the agent).
- **New** `internal/agentsmd/agentsmd.go` — `//go:embed template.md` →
  `CanonicalTemplate`; `BeginMarker`/`EndMarker` consts; `canonicalClauses()`
  strips the markers; `Prompt(extra string)` builds the **evaluative-merge** task
  prompt (appends per-run user guidance when non-empty).
- **New** `internal/agentsmd/agentsmd_test.go` — embed test, evaluative-merge
  prompt test, and marker-stripping test.
- `internal/commands/registry.go` — added `{Name:"improve-agents-md", AcceptsArgs:true}`
  to `BuiltinCommands()` (drives TUI/WebUI completion via `/api/v1/commands` and
  `Parse`). Web-UI picks it up automatically (no frontend change).
- `internal/mesnada/acp/session_state.go` — `improveAgentsMdCommandToken`.
- `internal/mesnada/acp/slash_commands.go` — `slashCommandImproveAgentsMd` kind +
  spec (input hint "optional extra guidance").
- `internal/mesnada/acp/goal_commands.go` — dispatch case +
  `processImproveAgentsMdCommand` (notice then
  `processPromptWithAgent(agentsmd.Prompt(extra))`).
- `internal/api/handlers_chat.go` — `case "improve-agents-md"` submits
  `agentsmd.Prompt(cmdArgs)` to `bgRunner` and streams.
- `internal/tui/page/chat.go` — `expandImproveAgentsMdCommand` expands the command
  into the prompt at the top of `sendMessage`, then falls through to Run/Steer.
- `internal/mesnada/acp/agent_pando_test.go` — available-commands count 8→9 +
  inclusion/hint assertions for the new command.

## Why
The user wanted a general, reusable, MANDATORY instruction set for any code
project's AGENTS.md (how to document, how to recover context with concrete tools:
KB, code-index, web search, Context7, browser), plus a command that uses an AI
process (current model) to evaluate and merge those clauses into an existing or
new AGENTS.md respecting each MANDATORY requirement.

## How it was verified
- `go build ./...` — clean.
- `go vet` on agentsmd/acp/api/tui-page — clean.
- `go test ./internal/agentsmd/` — pass (embed, evaluative-merge prompt,
  marker-stripping tests).
- `go test ./internal/mesnada/acp/` — pass (updated available-commands test).

## Notes
- No new config knob and no i18n strings (command output is agent-driven; the
  static notice text is English like goal/compact).
- The project's own `AGENTS.md` was left as-is; run `/improve-agents-md` to have
  the agent evaluate-and-merge the canonical clauses into it or any other project.
