---
created_at: 2026-06-22T09:37:19.930494367Z
updated_at: 2026-06-22T09:37:19.930494367Z
tags:
    - investigation
    - acp
    - slash-commands
    - zed
---
# Investigation: Zed reports `/goal` as unrecognized in Pando

## What was investigated
Reviewed Pando's ACP slash-command implementation and compared it with current Zed and ACP documentation to understand why Zed shows `/goal is not a recognized command in pando` and `Available commands for pando: none`.

## Files and symbols reviewed
- `internal/mesnada/acp/slash_commands.go`
- `internal/mesnada/acp/goal_commands.go`
- `internal/mesnada/acp/session_state.go`
- `TestAvailableCommands_ExposeGoalSlashCommands`

## Findings
- Pando currently advertises ACP slash commands through `available_commands_update` using `availableCommands()`.
- The ACP implementation includes `/goal`, `/goal-status`, `/goal-cancel`, `/compact`, and `/summarize`.
- `go test ./internal/mesnada/acp` passes, indicating the local implementation is coherent.
- Current ACP spec still documents slash commands as `available_commands_update` session notifications carrying `availableCommands` entries with optional `input.hint`.
- Current Zed docs for External Agents indicate ACP agents can integrate through the Agent Panel, but public issue traffic shows Zed has had compatibility and UX issues around third-party ACP slash commands.
- Zed PR `zed-industries/zed#37393` specifically mentions improved error handling for unsupported ACP slash commands, including a case where zero available commands are treated as `all unsupported`.
- Zed issue `zed-industries/zed#41405` documents users seeing `Available commands: none` even when agents advertise commands, implying some versions/configurations of Zed have not consistently surfaced third-party ACP slash commands.

## Likely explanation
The observed error is more likely caused by a client-side incompatibility/version mismatch or by Zed not receiving/accepting Pando's advertised command list in that session, rather than by a changed ACP slash-command specification. The spec still supports ACP-advertised slash commands.

## Verification
- Ran `go test ./internal/mesnada/acp`
- Reviewed ACP slash command spec at `https://agentclientprotocol.com/protocol/v1/slash-commands.md`
- Reviewed Zed docs for External Agents and public Zed/OpenCode issue discussions
