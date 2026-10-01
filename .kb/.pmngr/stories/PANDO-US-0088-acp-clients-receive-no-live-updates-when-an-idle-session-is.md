---
id: PANDO-US-0088
type: story
title: ACP clients receive no live updates when an idle session is resumed after delegated subagents finish
status: in_review
priority: high
parent: PANDO-EP-0016
author: mcp
labels: [bug, acp, delegation]
created: 2026-10-01T12:26:47Z
updated: 2026-10-01T13:02:21Z
started: 2026-10-01T13:02:21Z
---

## Description

As a user of an ACP client (Zed, Xcode, VS Code, JetBrains), I want the conversation to keep updating in real time when the parent agent is resumed after its delegated subagents finish, so that the resumed turn is not invisible until the session is reloaded.

Depends on PANDO-US-0087 (same epic), which introduces the channel-returning resume and the hook that lets a surface own the resumed run. Do this story after it.

### Root cause (code read 2026-10-01)

- ACP receives agent events only from the channel returned by `Run` / `RunGoal` for one `session/prompt`: `appACPAgentAdapter.Run` and `ForwardACPAgentEvents` (`internal/app/app.go:2521-2594`), shared with the `pando acp` stdio adapter (`cmd/root.go:752`).
- `internal/mesnada/acp` has no subscription to the agent or message brokers. Its only subscriptions are `notify` (`agent.go:1376`) and design events.
- A resumed run is started by `agent.Resume` (`internal/llm/agent/agent.go:708-743`), which drains its own channel. No `session/prompt` is in flight, so nothing forwards the events.
- `ForwardACPAgentEvents` drops unknown event types (`default: continue`, `app.go:2583`), including `Resurrected`, `ConclusionQueued` and `ConclusionInjected`.

Already correct: steering during a resumed run. `Prompt` checks `agentService.IsSessionBusy` (`internal/mesnada/acp/agent.go:369`), which is true for a resumed run, so a new prompt is queued as feedback.

### Proposed solution

1. Register an ACP resume handler through the hook from PANDO-US-0087: when the supervisor resumes a session that has a live `ACPServerSession`, the ACP layer receives the run channel and forwards it with the same translation and `SendUpdate` path a prompt uses (message ids, grouped thinking, tool calls, plan).
2. Map `Resurrected` (and the conclusion events, if useful) in `ForwardACPAgentEvents` to a visible agent message chunk so the client shows why the session woke.
3. When the resumed run ends, send the same post-run updates `finishPrompt` sends (usage, title, run status meta), without a `PromptResponse` since no request is pending.
4. Make `session/cancel` stop a resumed run (verify the cancel path reaches `agentService.Cancel` with no prompt in flight).
5. If the session has no connected ACP client, keep the current behaviour (run proceeds, persisted only).

### Open question to resolve first

ACP updates here are sent outside any `session/prompt` turn. Confirm how each target client treats unsolicited `session/update` notifications between turns (render, ignore, or misgroup them). Xcode groups chunks by `messageId` and has had turn-merging bugs before. If a client ignores them, fall back to a visible notice plus replay on the next prompt or `session/load`.

## Acceptance Criteria

- [ ] With `resurrectIdleLoop = true`, a resumed run streams text, thinking, tool calls and plan updates to the connected ACP client as it happens.
- [ ] The client shows a visible marker that the session resumed after a delegated task.
- [ ] Usage and title updates are sent when the resumed run ends.
- [ ] A prompt sent during a resumed run is queued as steering (regression test for the existing behaviour).
- [ ] `session/cancel` stops a resumed run.
- [ ] No duplicated or merged messages in Zed and Xcode (message ids distinct from the previous turn).
- [ ] Behaviour with no connected client is unchanged.
- [ ] Both ACP adapters (in-app and `pando acp` stdio) are covered by the same code path.
- [ ] Go tests cover the resume forwarding and the new event mapping; `go test ./internal/mesnada/acp ./internal/app ./internal/llm/agent` passes.
- [ ] Manual check in at least Zed; result for Xcode recorded.

## Notes

- Key files: `internal/mesnada/acp/agent.go`, `internal/mesnada/acp/types_interfaces.go`, `internal/app/app.go` (`ForwardACPAgentEvents`, `appACPAgentAdapter`), `cmd/root.go` (`acpAgentAdapter`), `internal/app/delegation_supervisor.go`.
- Analysis by code reading only; not reproduced at runtime.
