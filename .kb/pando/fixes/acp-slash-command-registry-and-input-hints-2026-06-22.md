---
created_at: 2026-06-22T09:31:01.449557873Z
updated_at: 2026-06-22T09:31:01.449557873Z
tags:
    - fix
    - acp
    - slash-commands
---
# ACP slash commands registry and input hint fix

## What changed

Implemented an ACP-specific slash command registry that is now the single source of truth for both command advertising and parsing.

### Files touched
- `internal/mesnada/acp/slash_commands.go`
- `internal/mesnada/acp/goal_commands.go`
- `internal/mesnada/acp/session_state.go`
- `internal/mesnada/acp/agent_pando_test.go`

### Details
- Added `slashCommandSpec` definitions for ACP slash commands and aliases.
- Moved `availableCommands()` generation to use the ACP registry instead of the shared TUI/WebUI command registry.
- Added ACP `input.hint` metadata for argument-taking commands (`goal`, `autopilot`).
- Kept ACP command names without leading slash for protocol compatibility.
- Updated slash command parsing to use the same registry-backed definitions.
- Reused registry usage text for `/goal` validation feedback.
- Updated tests to verify input hints for argument commands and no empty input payloads for others.
- Added parser coverage to ensure aliases and command kinds remain aligned with the registry.

## Why

ACP slash commands were being advertised through a shared generic command registry with no ACP-specific input metadata. That likely caused ACP clients to stop rendering or listing the commands reliably. Using a dedicated ACP registry restores explicit compatibility and prevents future drift between advertised commands and parsed commands.

## Verification

Ran:
- `go test ./internal/mesnada/acp -run 'TestAvailableCommands_ExposeGoalSlashCommands|TestParseSlashCommand_UsesRegistry|TestPandoACPAgent_SetConnection_BackfillsAvailableCommandsForExistingSessions'`
- `go test ./internal/llm/agent ./internal/api`
- `gofmt -w` on modified ACP files

## Notes

The repo already had unrelated modified files before this change; only the ACP slash-command files and tests above were modified for this fix.