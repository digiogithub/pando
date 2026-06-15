---
created_at: 2026-06-15T12:27:47.718581984Z
updated_at: 2026-06-15T12:27:47.718581984Z
tags:
    - feature
    - tokens
    - tui
    - webui
    - context-window
    - pando
---
# Real-time context-window token counter (TUI + WebUI)

Implemented 2026-06-15. Goal: update the "Context" token counter live DURING the agent
loop (while tools execute / files are produced), not only when an LLM response finishes.

## Problem
- `agent.TrackUsage()` only updated `session.PromptTokens/CompletionTokens` and persisted
  (pubsub UpdatedEvent) on provider `EventComplete` (end of each LLM round-trip). During tool
  execution there was no token update. WebUI had NO SSE event for token updates at all.

## Design
Real tokens are only known after the LLM returns usage. During the loop we EMIT PROVISIONAL
ESTIMATES (~4 bytes/token heuristic) per tool result, reconciled by a confirmed update on the
next EventComplete. Estimated values render with a "~" prefix and dimmed style.

## Backend — internal/llm/agent/agent.go
- New `AgentEventType` = `AgentEventTypeTokenUsage` ("token_usage").
- New struct `TokenUsageInfo{PromptTokens, CompletionTokens, ContextWindow int64; Estimated bool}`
  and field `AgentEvent.TokenUsage *TokenUsageInfo`.
- Helpers: `tokenEstimateBase(ctx, sessionID)` (last confirmed totals + effective context window),
  `estimateToolResultTokens(tr)` ((len(Content)+len(Metadata)+3)/4),
  `publishTokenUsage(sessionID, eventCh, info)` (publishes to pubsub broker AND per-run eventCh,
  non-blocking).
- In the tool-execution loop (streamAndHandleEvents): capture base before loop; after each tool
  result publish an Estimated=true update with `PromptTokens = base + accumulated tool estimate`.
- In `processEvent` EventComplete: after `TrackUsage`, publish a confirmed (Estimated=false)
  token_usage so WebUI SSE can reconcile.

## TUI — internal/tui/components/core/status.go + tui.go + page/chat.go
- status.go: new `TokenUsageMsg` type; fields `estimatedTokens/estimatedContextWindow/estimatedActive`.
  Handle TokenUsageMsg (estimated→store+active, confirmed→clear). Clear estimate on session
  pubsub UpdatedEvent and SessionClearedMsg. `formatTokens(tokens, contextWindow, estimated)` adds
  "~" prefix. View uses estimate when active & > real total; dimmed background (t.TextMuted()).
- tui.go: in `case pubsub.Event[agent.AgentEvent]`, forward AgentEventTypeTokenUsage to the status
  component as core.TokenUsageMsg and return (that case does not fall through to the global forward).
- page/chat.go: excluded AgentEventTypeTokenUsage from the goal-update reload condition to avoid a
  DB hit per tool result.

## WebUI — web-ui/src
- types/index.ts: added 'token_usage' to SSEEvent union, `SSETokenUsage` interface, and transient
  `Session.context_window?` / `Session.tokens_estimated?` fields.
- services/sse.ts: parse 'token_usage' (snake_case keys: session_id, prompt_tokens,
  completion_tokens, context_window, estimated).
- stores/sessionStore.ts: `updateSessionTokens(id, prompt, completion, contextWindow, estimated)`.
- hooks/useChat.ts: in handleEvent, on 'token_usage' call updateSessionTokens (added activeSessionId
  to deps).
- components/layout/StatusBar.tsx: when tokens_estimated, show "~" prefix + dimmed/italic style.
- Backend SSE emit: internal/api/handlers_chat.go dispatchSSEEvent → writeSSEEvent "token_usage".

## Verification
- `go build ./...` OK; `go test ./internal/llm/agent ./internal/api` OK; `go vet` OK.
- web-ui `tsc --noEmit` OK; eslint on changed files OK.

## Notes / future
- ACP transport intentionally ignores the new event (TUI/WebUI scope only).
- Estimate is directional (Anthropic prompt tokens already include full conversation); the "~"
  marker communicates it is provisional. Could later use a real tokenizer for tighter estimates.
