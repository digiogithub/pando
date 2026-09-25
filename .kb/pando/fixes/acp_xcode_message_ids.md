---
created_at: 2026-09-25T15:03:07.094816702Z
updated_at: 2026-09-25T15:03:07.094816702Z
---
# Fix: ACP live message ids for Xcode 27 compatibility (PANDO-US-0064 / P1)

Status: DONE (2026-09-25). Part of [[pando/plans/acp_xcode_compat.md]] (P1).

## Problem
From a real Xcode 27 ACP client log (see the plan doc): Xcode groups streamed
`session/update` chunks by `messageId`. Pando had two bugs:

1. `processPromptWithAgent` in `internal/mesnada/acp/prompt_handler.go` echoed
   the whole live user prompt back as a `user_message_chunk` with a CONSTANT id
   (`acpSession.PandoSessionID() + "-user"`) on every turn. The ACP spec only
   expects `user_message_chunk` during `session/load` history replay — the
   client already renders the prompt it just sent.
2. In `processAgentEventStream`, `currentMessageID` was only set from
   `AgentEventTypeResponse` (which arrives at the END of an assistant message),
   so every live `agent_message_chunk` / `agent_thought_chunk` delta was sent
   WITHOUT a messageId (or with the previous message's stale id after a tool
   round-trip). Result: Xcode merged answers from several turns into one bubble.

## Changes

- `internal/llm/agent/agent.go`:
  - `AgentEvent` struct (~line 221): added `MessageID string` field, documented
    as populated on ThinkingDelta/ContentDelta/ToolCall events.
  - `processEvent` (~line 1863 onward): set `MessageID: assistantMsg.ID` on both
    the `publishEvent` and `eventCh` sends for `EventThinkingDelta`,
    `EventContentDelta`, `EventToolUseStart`, `EventToolUseDelta`, and
    `EventToolUseStop` (trivial, assistantMsg already in scope).
- `internal/mesnada/acp/types_interfaces.go`:
  - Mirror `AgentEvent` struct: added `MessageID string` field with the same
    semantics, used by the ACP layer (avoids importing internal/llm/agent).
- `internal/app/app.go` (`appACPAgentAdapter.forwardEvents`, ~line 2589-2598):
  copy `acpEv.MessageID = ev.MessageID` across for the `ContentDelta`,
  `ThinkingDelta`, and `ToolCall` cases when converting `agent.AgentEvent` to
  `mesnadaACP.AgentEvent`.
- `internal/mesnada/acp/prompt_handler.go`:
  - `processPromptWithAgent`: removed the live `user_message_chunk` echo block
    entirely (was calling `acpSession.SendUpdate(updateUserMessageTextWithID(...))`
    with the constant `-user` id). Replaced with an explanatory comment.
    `streamSessionHistory` in `session_state.go` (history replay) is untouched
    and still sends `user_message_chunk` with the real persisted `msg.ID`.
  - `processAgentEventStream`: in the `AgentEventTypeThinkingDelta` and
    `AgentEventTypeContentDelta` cases, adopt `event.MessageID` into the
    closure variable `currentMessageID` (when non-empty) BEFORE
    buffering/sending. This means:
    - The very first delta of a message already carries the final messageId
      (same id used by `session/load` replay, since it's `assistantMsg.ID`).
    - A new assistant message started after a tool round-trip gets a new id
      immediately from its first delta instead of inheriting the previous
      message's id.
    - Grouped thinking flush (`flushThinking`) reads `currentMessageID` via
      closure at flush time; since flushes are always forced
      (`flushThinking(true)`) on tool-call/tool-result/response/system-message/
      error/summarize events (which occur between messages), any buffered text
      is always drained under the correct id before a new message's id would
      be adopted — no extra id-tracking needed on `groupedThinkingState`.

## Tests added
`internal/mesnada/acp/agent_pando_test.go`:
- Renamed `TestProcessPromptWithAgentEmitsUserMessageChunk` →
  `TestProcessPromptWithAgentDoesNotEmitLiveUserMessageChunk`: now uses a real
  captured `AgentSideConnection` (via `newThoughtCaptureSession`) and asserts no
  `user_message_chunk` notification is sent during a live prompt.
- `TestPandoACPAgent_ProcessAgentEventStream_ContentDeltaUsesEventMessageIDFromFirstDelta`:
  two content deltas with `MessageID: "assistant-msg-1"` both produce
  `agent_message_chunk` notifications carrying that id, from the first delta.
- `TestPandoACPAgent_ProcessAgentEventStream_NewMessageAfterToolCallGetsNewID`:
  content delta (msg1) → response → tool_call → tool_result → content delta
  (msg2) → response; asserts the two `agent_message_chunk` notifications carry
  distinct ids (`assistant-msg-1`, `assistant-msg-2`).
- New helpers: `acpUpdateRecord` struct and `decodeSessionUpdateRecords(t, raw)`
  (decodes every `session/update` JSON-RPC notification captured on the fake
  connection's buffer into `{Kind, Text, MessageID}`; `Content` is decoded as
  `json.RawMessage` first because it's an object for message/thought chunks but
  an ARRAY for tool_call/tool_call_update, so eager struct decoding fails for
  the latter).

`internal/llm/agent/process_event_message_id_test.go` (new file):
- `TestProcessEventPopulatesMessageIDOnDeltas`: constructs a minimal `*agent`
  (`Broker` + a local `noopMessagesService` mock, same pattern as
  `steeringMockMessages` in `steering_test.go`) and calls `processEvent`
  directly for `EventThinkingDelta` and `EventContentDelta`, asserting the
  `AgentEvent` sent on `eventCh` carries `MessageID == assistantMsg.ID`.

## Verification
```
go build ./...
go vet ./internal/mesnada/acp ./internal/llm/agent ./internal/app
go test ./internal/mesnada/acp ./internal/llm/agent ./internal/api
```
All green:
```
ok  	github.com/digiogithub/pando/internal/mesnada/acp	1.877s
ok  	github.com/digiogithub/pando/internal/llm/agent	0.506s
ok  	github.com/digiogithub/pando/internal/api	4.303s
```

## Notes / concurrent work
This was implemented while other agents concurrently touched
`internal/llm/provider/copilot.go` (P2/P3 Copilot reasoning), and
`internal/mesnada/acp/agent.go` + new `session_mcp*.go` files (P4 per-session
MCP servers) in the same working tree (jj vcs, not git). `internal/llm/agent/agent.go`
and `internal/mesnada/acp/types_interfaces.go` were observed to change on disk
mid-task (unrelated additions from the P4 work); edits here were re-verified
against the live file content before applying and did not touch those other
regions.

See also [[pando/plans/acp_xcode_compat.md]] for the full multi-phase plan
(P2 Copilot reasoning summaries, P3 Responses history fidelity, P4 per-session
MCP servers, P5 final verification/docs).
