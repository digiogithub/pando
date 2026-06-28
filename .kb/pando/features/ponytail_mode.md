---
created_at: 2026-06-28T18:07:43.663382911Z
updated_at: 2026-06-28T18:07:43.663382911Z
tags:
    - feature
    - ponytail
    - slash-command
    - prompt-injection
    - change
---
# Feature: Ponytail mode (lazy-senior-dev prompt injection)

**Date:** 2026-06-28. **Status:** Implemented (Phases 1–7). Settings-panel UI + i18n deferred.

## What
Port of the `ponytail` skill (github.com/DietrichGebert/ponytail, MIT) into Pando as a native, per-session mode toggled by a slash command at three intensities plus off:

- `/ponytail lite` — build what's asked, name a lazier alternative in one line.
- `/ponytail full` — enforce "The Ladder"; shortest diff + explanation (also the no-arg default).
- `/ponytail ultra` — YAGNI extremist; deletion before addition; challenge the requirement.
- `/ponytail off` — disable (also "stop"/"normal"/"none"/"disable").

When active, the lazy-senior-dev ruleset (The Ladder + rules + output guidelines + exceptions + the active intensity snippet) is injected into the system prompt **before each turn**, reproduced faithfully from ponytail's SKILL.md.

## How it works
Pando already injects per-turn "active skill instructions" in `agent.prepareProvider` → `createAgentProvider` → `buildSystemMessage` → `prompt.WithSkills`. Ponytail rides that exact path, so it works uniformly across TUI, Web UI and ACP (all go through the agent). Mode state is per-session and ctx-threaded, mirroring `session_overrides.go`.

## Files changed
- **New `internal/ponytail/ponytail.go`** (+`ponytail_test.go`): `Mode` (off/lite/full/ultra), `ParseMode` (incl. off synonyms), `Description`, `Instructions(mode)` = always-on core + per-mode intensity snippet (programmatic assembly, no markdown line-filtering).
- **New `internal/llm/agent/ponytail_session.go`** (+test): `ponytailModes sync.Map`, `SetPonytailMode`, `PonytailMode`, `ponytailModeForContext`. Tri-state presence: explicit choice (incl. explicit off overriding a non-off default) vs absent→configured default.
- **`internal/llm/agent/agent.go`** `prepareProvider`: inject `prompt.InjectSkillInstructions("ponytail (<mode>)", ponytail.Instructions(mode))` when active; the early `skillManager==nil && persona==""` short-circuit now also checks active ponytail so injection still fires.
- **ACP** `internal/mesnada/acp/`: `slashCommandPonytail` kind + spec (`slash_commands.go`), `ponytailCommandToken`/`Name` (`session_state.go`), new `ponytail_commands.go` `processPonytailCommand`, wired into `handleSlashCommand` (`goal_commands.go`); `AgentService.SetPonytailMode(sessionID, mode) (applied string, ok bool)` (`types_interfaces.go`) implemented by adapters in `internal/app/app.go` + `cmd/root.go` (delegating to `agent.SetPonytailMode` via `ponytail.ParseMode`).
- **WebUI** `internal/api/handlers_chat.go`: `case "ponytail"` in `handleSlashCommandStream` → set mode + `content_delta` confirmation.
- **TUI** `internal/tui/page/chat.go`: `handlePonytailCommand` wired into `sendMessage` (toast + `agentpkg.SetPonytailMode`).
- **Shared registry** `internal/commands/registry.go`: `/ponytail` builtin (AcceptsArgs) → autocomplete + `commands.Parse` recognition across all three surfaces.
- **Config** `internal/config/config.go` (+`ponytail_test.go`): `PonytailConfig{DefaultMode}` (`[Ponytail] DefaultMode` toml) + `Config.PonytailDefaultMode()` resolver honoring env `PANDO_PONYTAIL_DEFAULT_MODE` (non-empty) over field; empty/invalid/off → "" (disabled, default). Parity with ponytail's `PONYTAIL_DEFAULT_MODE`/`defaultMode`.

## Tests
- `internal/ponytail`: ParseMode, off→empty, common+intensity present, no cross-mode leakage.
- `internal/llm/agent`: set/get/clear mode, ctx resolution.
- `internal/mesnada/acp`: `TestPandoACPAgent_HandleSlashPonytailSetsAndClearsMode` (ultra/default-full/off/unknown); updated `TestAvailableCommands_ExposeGoalSlashCommands` (7→8, ponytail has input hint).
- `internal/config`: default-mode field parsing + env precedence.
- Verified: `go build ./...`, `go vet`, and `go test ./internal/ponytail ./internal/llm/agent ./internal/mesnada/acp ./internal/api ./internal/config` all green.

## Deferred (future)
Settings-panel toggle + 7-locale i18n for the default mode (currently set via toml/env only); optional status-bar/sidebar "PONYTAIL" badge; REST GET of current mode for the WebUI. See plan `pando/plans/ponytail_mode_implementation_plan.md`.

## Attribution
Injected ruleset reproduced from DietrichGebert/ponytail (MIT). Pando delivers it via the agent's per-turn skill injection instead of host hooks.
