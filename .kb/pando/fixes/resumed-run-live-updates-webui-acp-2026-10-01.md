---
created_at: 2026-10-01T13:02:10.294315096Z
updated_at: 2026-10-01T13:02:10.294315096Z
tags:
    - fix
    - webui
    - acp
    - delegation
    - sse
    - api
    - agent
    - pando
---
# Fix: runs resumed after delegated subagents now stream live in WebUI and ACP (2026-10-01)

Gintrack: epic **PANDO-EP-0016**, stories **PANDO-US-0087** (WebUI) and **PANDO-US-0088** (ACP). Analysis: [[webui-resumed-run-no-live-updates-2026-10-01]]. Related: [[fix_acp_xcode_compat]], [[webui_pending_askuserquestion_blocks_model_switch]].

## Problem
With `mesnada.delegation.resurrectIdleLoop = true`, the delegation supervisor resumes an idle parent session when a delegated task concludes (Case B). `agent.Resume` drained the run channel itself, so only broker subscribers (TUI) saw the run. WebUI (built on `BackgroundSessionManager`) and ACP (per-prompt run channel) showed nothing until the conversation was reloaded.

## Shared mechanism (agent side)
- `agent.Service.ResumeRun(ctx, sessionID, content) (<-chan AgentEvent, error)` in `internal/llm/agent/agent.go`: like `Resume` but returns the run channel, with `AgentEventTypeResurrected` as first event. Caller must read until closed; cancelling ctx cancels the run (the forwarder then drains so nothing leaks). `Resume` = `ResumeRun` + drain.
- `internal/llm/agent/resume_registry.go`: `ResumeRegistry`, `ResumeHandler func(sessionID, start ResumeStart) (taken bool, err error)`, priorities `ResumePriorityOwner` (100) and `ResumePriorityFallback` (0). `taken=false` = not mine, `start` not called. `ErrSessionBusy` from a taker makes the supervisor fall back to `InjectConclusion`.
- `App.ResumeHandlers` (always non-nil) in `internal/app/app.go`; `delegationSupervisor.resume` offers the run to the registry and falls back to `agent.Resume` (TUI/CLI path, unchanged).
- Any new surface that streams runs must register a handler here.

## WebUI (PANDO-US-0087)
- `internal/api/resume_run.go`: API registers at Fallback priority and submits the resumed run to `bgRunner.Submit` (retries up to 2s when the previous bgRunner entry is not yet marked done). `sessionRunning = bgRunner.IsBusy || agent.IsSessionBusy` now feeds `is_running` (`handlers_sessions.go`), `/pending` (`handlers_questions.go`) and the session stream. `brokerRunEvents` streams a run bgRunner does not own, live from the agent broker (no replay).
- `handlers_chat.go`: `dispatchSSEEvent` maps `resurrected`, `conclusion_queued`, `conclusion_injected`; `handleChatStream` steers instead of erroring when the session is busy.
- New `POST /api/v1/sessions/{id}/cancel` (`bgRunner.Cancel` + `agent.Cancel`); the WebUI cancel now calls it. Before, cancel only aborted the SSE client and hit `/goal/cancel`, which does nothing without an active goal.
- Frontend: `SSEEvent` types + `sse.ts` parsing, notice rows with i18n `noticeKey` (`routingNotice.ts`, `MessageBubble.tsx`, 7 locales), `ChatView.tsx` `finishedSessionRef` is now `{id, at}` and blocks reattach only for 3s after a real `done`.

## ACP (PANDO-US-0088)
- `internal/mesnada/acp/resume_run.go`: `PandoACPAgent.TakeResumedRun` takes the run only when a live `ACPServerSession` with a client connection is bound to that Pando session id; run context derives from the session context so `session/cancel` stops it; events go through the shared `processAgentEventStream`; post-run updates via `sendPostRunUpdates` (no `PromptResponse`).
- Helpers split out so prompt and resume paths share setup: `prepareRun`, `applyRunMode`, `reconcileModelAfterRun`, `sendPostRunUpdates`.
- `app.RegisterACPResumeHandler` (Owner priority) is the single registration for both adapters: in-app server (`internal/app/app.go`) and `pando acp` stdio (`cmd/root.go`).
- `ForwardACPAgentEvents` maps `Resurrected` and `ConclusionInjected` to a system message with its own `MessageID` (`<session>-notice-<nanos>`) so Xcode does not merge it into a neighbouring message.

## Verification
- `go build ./...`, `go vet` on touched packages: clean.
- `go test -count=1 ./internal/mesnada/acp ./internal/app ./internal/llm/agent ./internal/api ./internal/agui ./cmd/...`: pass; resume tests also pass with `-race`.
- `web-ui`: `npx tsc --noEmit` clean, `npx vitest run` 21 passed.
- NOT verified: live flow in `pando app`, and any real ACP client (Zed, Xcode). Out-of-turn `session/update` handling by clients is unconfirmed; the story's fallback (notice + replay on next prompt / `session/load`) is not built.

## Docs
`docs/delegation.md` gained a paragraph on how each surface shows a resurrected run.
