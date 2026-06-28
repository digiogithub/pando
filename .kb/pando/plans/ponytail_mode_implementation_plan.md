---
created_at: 2026-06-28T17:57:08.912032028Z
updated_at: 2026-06-28T17:57:08.912032028Z
tags:
    - plan
    - feature
    - ponytail
    - slash-command
    - prompt-injection
---
# Ponytail Mode — Implementation Plan

## Source analysis (DietrichGebert/ponytail, via DeepWiki)

Ponytail instills a "lazy senior developer" persona into AI coding agents to produce minimal, efficient code. It works by **injecting a ruleset into the LLM context before each turn**, with three intensity levels and an off switch.

- **Mechanism**: host-specific adapters. For plugin/hook hosts (Claude Code, Codex, OpenCode, Pi) a `SessionStart` hook emits hidden context with `getPonytailInstructions(mode)`; a per-turn tracker hook inspects user input for `/ponytail <mode>` to switch modes; `filterSkillBodyForMode` filters `skills/ponytail/SKILL.md` by the active mode. Static-rule hosts (Cursor, Windsurf, Cline, Copilot) read consolidated instruction files.
- **Modes / power levels**:
  - `lite` — build what's asked, but name a lazier alternative in one line.
  - `full` — default; enforce "The Ladder", shortest diff + shortest explanation.
  - `ultra` — YAGNI extremist; deletion before addition; challenge the requirement.
- **Activation**: default mode via `PONYTAIL_DEFAULT_MODE` env or `defaultMode` in config; manual via `/ponytail lite|full|ultra`.
- **Deactivation**: `/ponytail off`, or natural language ("stop ponytail" / "normal mode"); clears a `.ponytail-active` flag so nothing is injected next turn.
- **Injected content (always)**: intro persona, persistence note, **The Ladder** (6 rungs), general rules, output guidelines (code-first, ≤3 lines), exceptions (never simplify away validation/security/error-handling/accessibility/explicit requests), boundaries. The Intensity table row + worked example are filtered per active mode.
- Convention: mark deliberate simplifications with a `ponytail:` comment naming the ceiling + upgrade path.

## How Pando maps to this

Pando already has the exact primitives ponytail needs:
- **Per-turn prompt injection**: `agent.prepareProvider` (internal/llm/agent/agent.go:2190) builds `activeSkillInstructions []string` every request and passes them through `createAgentProvider → buildSystemMessage → prompt.WithSkills`. `prompt.InjectSkillInstructions(name, body)` (internal/llm/prompt/prompt.go:133) is the exact "inject a skill ruleset" helper. This runs for EVERY surface (TUI, WebUI, ACP) because they all go through the agent.
- **Per-session, ctx-threaded state**: `session_overrides.go` keeps a `sync.Map` keyed by sessionID and reads it via `sessionIDFromContext(ctx)` (prompt.SessionIDKey / tools.SessionIDContextKey). This is the model for a parallel ponytail registry — concurrency-safe across sessions, no global mutation.
- **Slash-command dispatch** already exists per surface:
  - ACP: `internal/mesnada/acp/slash_commands.go` (specs + parse) + `goal_commands.go` `handleSlashCommand` switch.
  - WebUI: `internal/api/handlers_chat.go` `handleSlashCommandStream` switch.
  - TUI: `internal/completions/slash_commands.go` provider (autocomplete) + dispatch.

## Design decisions

1. **New package `internal/ponytail`** owns the mode enum + the injected text (no scattering). `go:embed` the canonical SKILL body (MIT, attributed) OR keep it as Go consts; build `Instructions(mode)` = commonText + intensitySnippet[mode]. Programmatic assembly avoids markdown line-filtering — simplest correct path.
2. **New per-session registry `internal/llm/agent/ponytail_session.go`** mirroring `session_overrides.go`: `ponytailModes sync.Map`, `SetPonytailMode(sessionID, mode)`, `ponytailModeForContext(ctx)`. Dedicated rather than overloading `SessionLLMOverrides` (single concern; that struct feeds delegation overrides). Same ctx key plumbing already works.
3. **Injection** in `prepareProvider`: after the contextManager skill loop, read `ponytailModeForContext(ctx)`; if active, append `prompt.InjectSkillInstructions("ponytail", ponytail.Instructions(mode))` to `activeSkillInstructions`. One chokepoint → universal.
4. **Activation** wired in each surface switch (ACP/WebUI/TUI). All call `agent.SetPonytailMode(sessionID, mode)` and emit a short confirmation; `off` clears. No LLM turn needed (synchronous, like `/db-compact`).
5. **Default mode** config `[Ponytail] DefaultMode = "off"` + env `PANDO_PONYTAIL_DEFAULT_MODE`; applied at session start so a fresh session can auto-activate (parity with ponytail's `PONYTAIL_DEFAULT_MODE`). Off by default → byte-identical legacy behavior when unused.
6. **Natural-language off** ("stop ponytail"/"normal mode") is OPTIONAL (later phase); slash commands are the contract the user asked for.

## Phases

- **Phase 1 — `internal/ponytail` package**: `Mode` type (`off`/`lite`/`full`/`ultra`), `ParseMode(string)`, `Instructions(mode) string`, `Description(mode)`. Embed/const the SKILL text faithfully (attribution to DietrichGebert/ponytail, MIT). Unit test `Instructions` returns common sections + the right intensity snippet, and empty for off.
- **Phase 2 — per-session registry + injection**: `ponytail_session.go` (sync.Map + Set/get-by-ctx); wire into `prepareProvider`. Unit test that an active mode adds an injected block to the built system message and `off`/unset adds nothing.
- **Phase 3 — ACP surface**: `slashCommandPonytail` kind + spec (`InputHint: "lite|full|ultra|off"`, usage text) in slash_commands.go; `processPonytailCommand` in a new `ponytail_commands.go` that parses the arg, calls `SetPonytailMode(acpSession.PandoSessionID(), mode)`, sends confirmation, ends the turn. Add to `availableCommands()`. Tests mirroring the goal slash tests.
- **Phase 4 — WebUI surface**: `case "ponytail":` in `handleSlashCommandStream` → set mode + `content_delta` confirmation. (Optional REST GET for current mode badge — defer.)
- **Phase 5 — TUI surface**: register `/ponytail` in `completions/slash_commands.go` for autocomplete; verify/route dispatch so a typed `/ponytail <mode>` sets the registry and shows a status line. Optional sidebar/status badge (defer if heavy).
- **Phase 6 — config + default-mode bootstrap**: `PonytailConfig{DefaultMode string}` in config.go, env binding, applied on session creation (set registry to default if non-off). TUI/WebUI settings field + 7-locale i18n.
- **Phase 7 — docs + verification**: `go test ./internal/ponytail ./internal/llm/agent ./internal/mesnada/acp ./internal/api`; `go build ./...`; README note; `kb_add_document` change summary.

## Verified injection facts
- `prepareProvider` internal/llm/agent/agent.go:2190 — builds `activeSkillInstructions`.
- `createAgentProvider` :2217 / `buildSystemMessage` :2339 — thread skills via `prompt.WithSkills`.
- `prompt.InjectSkillInstructions` internal/llm/prompt/prompt.go:133.
- `session_overrides.go` — sync.Map + `sessionIDFromContext` pattern to mirror.
- ACP `handleSlashCommand` internal/mesnada/acp/goal_commands.go:12; specs in slash_commands.go.
- WebUI `handleSlashCommandStream` internal/api/handlers_chat.go:801.

## Status
Created 2026-06-28. Implementation starting from Phase 1.
