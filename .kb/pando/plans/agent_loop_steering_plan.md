---
created_at: 2026-06-16T10:38:06.725488385Z
updated_at: 2026-06-16T10:38:06.725488385Z
tags:
    - plan
    - agent
    - steering
    - tui
    - webui
    - acp
    - architecture
---
# Plan: Interactive Agent Loop Steering (inject feedback without ending the loop)

Created: 2026-06-16. Status: Phase 1 + 2 in progress.

## Goal
Allow the user to enter a new prompt/feedback while the agent is working, WITHOUT cancelling the loop. The message is QUEUED and INJECTED at the next safe boundary (after the current iteration's tool_results, or at end of the current turn), without cutting the stream or corrupting tool_call/tool_result history. Claude Code model. Must work in TUI, WebUI and ACP.

## Confirmed architecture
- Central loop: `agent.processGeneration` (internal/llm/agent/agent.go:585), `for {}` calling `streamAndHandleEvents`; if `FinishReasonToolUse` it appends `(agentMessage, toolResults)` and `continue`; otherwise returns `Done`.
- Busy state: `activeRequests sync.Map[sessionID]CancelFunc`; `IsSessionBusy`/`Cancel`.
- Frontends: TUI (editor.go:178 rejects if busy), WebUI (handlers_chat.go + BackgroundSessionManager), ACP (agent.go Prompt/Cancel).

## Design decisions
- Safe-at-boundary injection (chosen over immediate interrupt). No new goroutines; queue drained synchronously inside existing loop, guarded by a mutex.
- Queued feedback is materialized as a real message.User via createUserMessage, so it persists in conversation and survives reloads/summaries.
- Injection happens only at iteration boundaries, never between a tool_call and its tool_result; sanitizeToolCallHistory covers edges.

## Phase 1 — Core (internal/llm/agent/agent.go + tests)
1. Queue in `agent` struct: `steeringMu sync.Mutex`, `steeringQueue map[string][]steeringMessage` (steeringMessage{content string; attachments []message.Attachment}).
2. Service interface additions: `Steer(sessionID, content string, attachments ...message.Attachment) error` (queues if busy; returns ErrSessionNotBusy sentinel if not busy so caller does normal Run); `PendingSteering(sessionID string) int`.
3. Drain helper `drainSteeringInto(ctx, sessionID, msgHistory) ([]message.Message, bool)` called: (a) after FinishReasonToolUse before continue; (b) in end-of-turn branch before return Done — if pending, inject and continue instead of returning; apply fitMessagesToProviderBudget/ensureHistoryFitsBeforeSend.
4. Cleanup: clear queue in Run defer and in Cancel(sessionID).
5. New events: AgentEventTypeSteeringQueued, AgentEventTypeSteeringInjected.
6. Tests in internal/llm/agent.

## Phase 2 — TUI (page/chat.go, components/chat/editor.go, components/chat/list.go)
1. editor.go send(): stop rejecting when busy; always allow sending.
2. page/chat.go sendMessage(): if IsSessionBusy -> CoderAgent.Steer(...) else Run. Feedback toast.
3. list.go working(): show pending steering count next to spinner; reflect SteeringInjected.
4. Escape stays Cancel.
5. TUI test (editor_test.go style).

## Phase 3 — WebUI/API (handlers_chat.go, routes, webui frontend)
1. POST /api/v1/sessions/{id}/steer -> if busy Steer, else 409 not_busy. Injection events flow over existing SSE (survives disconnects via BackgroundSessionManager).
2. Frontend: send while active -> /steer; show "queued" chip; reflect SteeringInjected.
3. Tests in internal/api.

## Phase 4 — ACP (internal/mesnada/acp/agent.go Prompt)
1. In Prompt: if agentService.IsSessionBusy -> Steer instead of ErrSessionBusy/new Run; emit session/update about queueing.
2. Keep Cancel intact. Tests in internal/mesnada/acp.

## Phase 5 — Closeout
1. Optional config [Agent] EnableSteering (default true).
2. KB docs pando/features/agent_loop_steering.md.
3. Verify: go test ./internal/llm/agent ./internal/api (+ ./internal/mesnada/acp, ./internal/tui/...).
