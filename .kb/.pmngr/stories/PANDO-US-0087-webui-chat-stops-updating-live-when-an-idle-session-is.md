---
id: PANDO-US-0087
type: story
title: WebUI chat stops updating live when an idle session is resumed after delegated subagents finish
status: in_review
priority: high
parent: PANDO-EP-0016
author: mcp
labels: [bug, webui, sse, delegation, api]
created: 2026-10-01T09:48:40Z
updated: 2026-10-01T13:02:21Z
started: 2026-10-01T13:02:21Z
---

## Description

As a WebUI user who delegates work to subagents, I want the open conversation to keep updating in real time when the parent agent is resumed after the subagents finish, so that I do not have to switch screens and re-select the conversation to see the new messages.

### Symptom

The parent agent ends its turn to wait for delegated (mesnada) tasks. When a task concludes, the delegation supervisor resumes the idle parent session. The resumed run is persisted correctly, but the open chat view shows nothing: no deltas, no tool calls, no busy indicator. Leaving the chat and selecting the conversation again reloads the history and shows the messages, still without live updates.

Requires `mesnada.delegation.resurrectIdleLoop = true` (default is false in code; `pando init` and this repo's `.pando.toml` set it to true).

### Root cause (analysis 2026-10-01, key lines read)

The WebUI live path is built entirely on `BackgroundSessionManager` (`bgRunner`), and the resumed run never goes through it.

1. **Resumed run bypasses bgRunner.** `delegation_supervisor.go` (`flushWith`, ~:596) calls `agent.Resume`. `Resume` (`internal/llm/agent/agent.go:708-743`) calls `runInternal` and drains the returned run channel in a goroutine. Events only reach the agent pubsub broker. Nothing in `internal/api` subscribes to that broker (no `CoderAgent.Subscribe` / `Messages.Subscribe` there).
2. **Running flag stays false.** Every running signal the WebUI reads is `bgRunner.IsBusy`, not `CoderAgent.IsSessionBusy`: `handlers_sessions.go:68,132`, `handlers_questions.go:106` (`/pending`), `handlers_chat.go:272`. `IsBusy` is `!s.done` of the previous, finished run (`background_runner.go:61-71`). The only frontend reattach trigger (`ChatView.tsx:114-126`, driven by `is_running`) therefore never fires. `Subscribe` on the old session would only replay the stale buffer and close (`background_runner.go:164-165`).
3. **Frontend guard.** After a real `done`, `finishedSessionRef` (`ChatView.tsx:32-34`, `:121`) blocks reattach for that session until the user sends a new message (`:130-132`). It would still block reattach after 1 and 2 are fixed.
4. **Missing event mapping.** `dispatchSSEEvent` (`handlers_chat.go`) and the client `SSEEvent` type have no case for `AgentEventTypeResurrected`, `ConclusionQueued`, `ConclusionInjected`; they are dropped.

Not affected: the live-injection case (parent still busy, `InjectConclusion`) flows through the existing bgRunner stream. The TUI works because it subscribes globally to the message and agent brokers and uses `IsSessionBusy`.

### Related side effects (inferred from code, not reproduced)

- A prompt sent during a resumed run passes `bgRunner.Submit` (old run is done) and then `Run` fails with `ErrSessionBusy`; the UI does not steer because `streaming` is false.
- Cancel from the UI calls `bgRunner.Cancel` with the stale cancel func, which does not cover the resumed run.
- ACP appears to have the same gap (events only from the per-prompt `Run` channel). Out of scope here; track separately if confirmed.

### Proposed solution

Make resumed runs first-class bgRunner runs, so the existing stream, replay buffer, busy flag, steer and cancel all work unchanged.

1. **Backend, run ownership.** Let the API layer own the resumed run: give the agent/supervisor an optional "resume runner" hook (set by `internal/api` at server start) so that Case B calls `bgRunner.Submit(sessionID, ...)` with a function that starts the system-initiated run and returns its event channel, instead of `Resume` draining it. Split `Resume` into a channel-returning variant (e.g. `ResumeRun`) plus the current draining wrapper for surfaces with no runner (TUI, CLI). Emit `Resurrected` on the run channel as its first event so it lands in the replay buffer.
2. **Backend, busy signal.** Report `is_running` / `running` as `bgRunner.IsBusy(id) || agent.IsSessionBusy(id)` as a safety net for any run started outside bgRunner.
3. **Backend, events.** Map `Resurrected`, `ConclusionQueued`, `ConclusionInjected` in `dispatchSSEEvent` to SSE events.
4. **Frontend.** Add the new event types to `SSEEvent` and render the "Resuming" framing. Fix the `finishedSessionRef` guard so a run that starts after a completed one can reattach: clear the guard when the server reports `is_running` false after the done, so a later false-to-true transition reattaches. Reattach must not duplicate messages already on screen (stream replay starts at the new run, since `Submit` replaces the `bgSession`).
5. **Detection latency.** Reattach currently depends on the 4 s `/pending` poll. Acceptable for a first version; optional follow-up is a push signal (session-running event on an existing global SSE stream).

## Acceptance Criteria

- [ ] With `resurrectIdleLoop = true`, a parent session that ended its turn and is resumed by a delegated-task conclusion shows the resumed run live in the open WebUI chat (text deltas, tool calls, busy indicator) without navigating away, within one poll interval.
- [ ] The chat shows a visible "resuming after delegated task" marker for the resumed turn.
- [ ] `GET /sessions`, `GET /sessions/{id}` and `/pending` report the session as running during a resumed run.
- [ ] Sending a message during a resumed run steers it instead of failing with a busy error.
- [ ] Cancel from the WebUI stops a resumed run.
- [ ] `GET /sessions/{id}/stream` during a resumed run replays that run's events and then goes live; after it ends, `done` is emitted once.
- [ ] No duplicated messages in the transcript after the automatic reattach.
- [ ] TUI and non-API surfaces keep their current behaviour (resumed run still works with no runner hook set).
- [ ] Go tests cover: resume submitted through bgRunner, busy flag during resumed run, SSE mapping of the three event types. `go test ./internal/llm/agent ./internal/api ./internal/app` passes; `npx tsc --noEmit` clean in `web-ui`.
- [ ] Manual or Playwright check of the full flow in `pando app`.

## Notes

- Key files: `internal/llm/agent/agent.go` (`Resume`, `runInternal`), `internal/app/delegation_supervisor.go`, `internal/api/background_runner.go`, `internal/api/handlers_chat.go`, `internal/api/handlers_sessions.go`, `internal/api/handlers_questions.go`, `web-ui/src/components/chat/ChatView.tsx`, `web-ui/packages/pando-client/src/hooks/useChat.ts`, `web-ui/packages/pando-client/src/services/sse.ts`.
- Rejected alternative: a session-wide SSE channel fed from the agent/message brokers (what the TUI does). Cleaner long term but a much larger change to the WebUI state model; steer and cancel would still need the bgRunner fix.
- Analysis was by code reading only; the bug has not been reproduced under instrumentation yet.
