---
created_at: 2026-06-16T12:04:47.408890364Z
updated_at: 2026-06-16T12:04:47.408890364Z
tags:
    - feature
    - agent
    - steering
    - tui
    - webui
    - acp
---
# Feature: Interactive Agent Loop Steering

Implemented 2026-06-16. Lets the user submit new feedback/prompt while the agent is working WITHOUT cancelling the loop. The message is queued and injected at the next safe boundary of the agent loop. Works in TUI, WebUI and ACP.

## Core (internal/llm/agent/agent.go)
- `agent.steeringQueue map[string][]steeringMessage` guarded by `steeringMu`.
- `Service.Steer(sessionID, content, attachments...) error` — queues if the session is busy; returns `ErrSessionNotBusy` otherwise (caller falls back to Run).
- `Service.PendingSteering(sessionID) int` — for UI indicators.
- `drainSteeringInto(ctx, sessionID, msgHistory, eventCh)` injects queued messages as persisted `message.User` (via createUserMessage) at two safe points in `processGeneration`:
  1. after `FinishReasonToolUse` (tool_results already persisted, history valid), before continue.
  2. at end of turn: instead of returning Done, inject + `ensureHistoryFitsBeforeSend` + continue.
- Queue cleared in `Cancel(sessionID)` and in the `Run` goroutine defer.
- Events: `AgentEventTypeSteeringQueued`, `AgentEventTypeSteeringInjected`.
- Never injects between a tool_call and its tool_result.
- Tests: internal/llm/agent/steering_test.go.

## TUI (internal/tui)
- editor.go send(): no longer rejects while busy.
- page/chat.go sendMessage(): if IsSessionBusy -> Steer (fallback to Run on ErrSessionNotBusy race); info toast.
- list.go working(): shows "(N feedback queued)"; help() hint shows enter=queue feedback / esc=cancel while busy.

## WebUI/API (internal/api, web-ui)
- `POST /api/v1/sessions/{id}/steer` (handlers_chat.go handleSteer, routes.go): 202 on queue, 409 {error:not_busy} if idle, 400 invalid/empty.
- SSE events `steering_queued`/`steering_injected` serialized in streamSessionEvents switch.
- Frontend useChat.ts: `steer(text)` POSTs to /steer with optimistic user message; sendMessage routes to steer while streaming. types/index.ts + services/sse.ts updated.
- Tests: internal/api/handlers_steer_test.go.

## ACP (internal/mesnada/acp)
- AgentService interface gained Steer/PendingSteering/IsSessionBusy (types_interfaces.go); adapters updated in internal/app/app.go (appACPAgentAdapter) and cmd/root.go (acpAgentAdapter).
- agent.go Prompt(): if not a slash command and agentService.IsSessionBusy(pandoSessionID) -> Steer + send agent_message_chunk ack + finishPrompt(EndTurn). Falls back to normal run if Steer errors (race). Overrides not applied in steering path.
- Tests: internal/mesnada/acp/agent_pando_test.go (BusySessionQueuesSteering, IdleSessionRunsNormally).

## Notes / future
- No config flag added; steering is always-on and safe (boundary injection). Could add [Agent] EnableSteering later if needed.
- Verify: go test ./internal/llm/agent ./internal/api ./internal/mesnada/acp ; web-ui: npm run typecheck.
