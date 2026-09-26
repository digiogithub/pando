---
created_at: 2026-09-28T20:50:23.624078894Z
updated_at: 2026-09-28T20:50:51.930986115Z
tags:
    - fix
    - acp
    - xcode
---
# Fix: Xcode 27 ACP regressions seen in v1.0.4 (repeating thinking, still no messageId)

Status: FIXED in code (2026-09-28, issues 1-3), not yet released. Follow-up of [[pando/fixes/acp_xcode_compat.md]] (EP-0012/EP-0013).

## Evidence
Colleague log `pando-acp.log` (v1.0.4, Xcode 27.0 27A266, Copilot /responses, IPC role=secondary). `pando-acp_1.log` was just a truncated prefix of the same log.
- 620 `agent_thought_chunk`, each flush re-sending the whole accumulated thought ("... `git log -2", "... `git log -2 --", "... `git log -2 --stat`").
- 0 of 1323 live `agent_message_chunk` carried `messageId`; only the final forced thought flush (after AgentEventTypeResponse set currentMessageID) did.
- Session resume: Xcode sends `session/load`, Pando replays history (spec-compliant) and Xcode appends it to the transcript it already shows -> the "50 old messages shown again" symptom. Xcode's session/prompt carries no client messageId.
- mcpbridge permission prompt gone (EP-0013 confirmed working).

## Root causes and changes
1. **messageId dropped** — `pando acp` uses `acpAgentAdapter` in `cmd/root.go`, whose `forwardEvents` was a copy of `appACPAgentAdapter.forwardEvents` (internal/app/app.go). EP-0012 patched only the app copy. Fix: new exported `app.ForwardACPAgentEvents` shared by both adapters. Side effect: the stdio adapter now also forwards `AgentEventTypeSystemMessage` (the app copy already did).
2. **Grouped thinking never reset** — `processAgentEventStream.flushThinking` (internal/mesnada/acp/prompt_handler.go) decided success with `sentThinkingDeltas == beforeSent`; that flag stays true after the first flush of a turn, so `markFlushed` never ran again and every delta re-sent the full buffer. Fix: `sendThinking` returns bool; reset on each successful send.
3. **session/load duplication (option C)** — Xcode keeps and re-renders its own transcript; the spec-mandated replay got appended to it. In `internal/mesnada/acp`:
   - `agent.go`: `PandoACPAgent.clientName` stored in `Initialize`; capabilities advertise `sessionCapabilities.resume` (`ResumeSession` existed but was never announced).
   - `LoadSession` skips `streamSessionHistory` when `clientKeepsOwnTranscript(clientName)` (case-insensitive "xcode"); still schedules `available_commands_update`; logs `acp: load session replay skipped`.
   - `ResumeSession` aligned with `LoadSession`: `reconcileACPThinkingSession`, `SetAskPermission(false)` for new registrations (was `defaultAskPermissionForMode`), `available_commands_update`, `Models` via new `buildUnstableSessionModelState` (`session_state.go`).

## Tests
- `internal/mesnada/acp/agent_pando_test.go`: `TestPandoACPAgent_ProcessAgentEventStream_GroupedThinkingSendsOnlyNewText`, `TestPandoACPAgent_Initialize_AdvertisesResume`, `TestPandoACPAgent_LoadSession_SkipsReplayForXcode` (Xcode skip; zed/empty replay), `TestPandoACPAgent_ResumeSession_IncludesModelsAndNoPermissionPrompt`.
- `internal/app/acp_forward_events_test.go`: `TestForwardACPAgentEventsKeepsMessageID`.
- `go build ./...`, go vet, `go test ./internal/mesnada/acp ./cmd ./internal/app ./internal/api ./internal/llm/agent` green. Not live-tested with Xcode; if Xcode ever shows an empty transcript after reopen, revisit the skip.

## Other log noise
`sql: database is closed` in remembrances reindex/kb watcher on a secondary after shutdown; firebase MCP fails under seatbelt ("Unable to obtain permissions for firebase-debug.log"); poeditor MCP missing token.
