---
created_at: 2026-07-14T14:42:06.042683141Z
updated_at: 2026-07-14T14:42:06.042683141Z
tags:
    - change
    - caveman
    - slash-commands
    - acp
    - tui
    - webui
    - phase4
---
# Caveman output-brevity mode — Phase 4 (slash commands) COMPLETE

Continues [[caveman_phase2_3_config_and_injection]]. Implements Phase 4 of
[[caveman-persistent-output-brevity-mode]]: `/caveman [lite|full|ultra|wenyan]`
and `/caveman-finish` on every interactive surface. Both are **synchronous
control commands** — they set session state and reply, without running an LLM
turn.

## Shared message helpers — `internal/caveman/caveman.go`
Added `Usage`, `FinishUsage`, `DisabledMessage` and `ActivationMessage(Mode)` so
the four surfaces (TUI, Web UI/API, ACP, and the built-in registry) do not each
reword the confirmation. `ActivationMessage` states what changes (fewer words,
less filler, to cut output tokens) *and* what does not: reasoning, tool use,
testing and verification are unchanged; code/commands/errors stay exact; asking
for detail still gets it in full.

## Surfaces
- **`internal/commands/registry.go`** — `caveman` (AcceptsArgs) and
  `caveman-finish` (no args) added to `BuiltinCommands`, which feeds native
  TUI/Web UI completion.
- **ACP**: `slashCommandCaveman` / `slashCommandCavemanFinish` kinds +
  specs (`slash_commands.go`), tokens (`session_state.go`), dispatch
  (`goal_commands.go`), and the new `caveman_commands.go` modeled on
  `ponytail_commands.go`.
- **`internal/mesnada/acp/types_interfaces.go`** — `AgentService` gains
  `SetCavemanMode(sessionID, mode string) (applied string, ok bool)`, implemented
  by both concrete adapters (`cmd/root.go` `acpAgentAdapter`, `internal/app/app.go`
  `appACPAgentAdapter`) and the test mock.
- **`internal/api/handlers_chat.go`** — `caveman` / `caveman-finish` cases in
  `handleSlashCommandStream` (Web UI + TUI chat over SSE).
- **`internal/tui/page/chat.go`** — `handleCavemanCommand`, wired next to
  `handlePonytailCommand`; reports via toast.

## Notable design points
- **`slashCommandSpec.parse` now always carries the typed argument**, including
  for argument-free commands (previously it was dropped when `InputHint` was
  empty). Without this the ACP layer could not see a stray argument, and the plan
  requires `/caveman-finish ultra` to return usage rather than silently disabling
  the mode — which would look to the user like a level switch. Other no-arg
  commands ignore `Objective`, so nothing else changes.
- `/caveman` with no argument defaults to `full` (matching `/ponytail`).
- An unknown level is rejected and leaves session state untouched on every
  surface.

## Verification
- `go build ./...` — clean.
- `go test ./internal/commands ./internal/mesnada/acp ./internal/caveman ./internal/api ./internal/llm/agent ./internal/config ./internal/tui/...` — all ok.
- `go test -race ./internal/mesnada/acp -run Caveman` — ok.
- New tests: registry parity + parsing (`internal/commands/registry_test.go`),
  ACP parsing incl. the stray-argument case, advertisement (level hint on
  `/caveman`, none on `/caveman-finish`), set/clear across all four levels,
  unknown level rejected, `/caveman-finish <arg>` rejected without changing the
  level (`internal/mesnada/acp/caveman_commands_test.go`). Updated the ACP
  advertised-command count test (11 → 13).

## Next
Phase 5 (TUI settings section), Phase 6 (Web UI settings + `caveman_default_mode`
in the settings API), Phase 7 (docs + honest measurement wording).
