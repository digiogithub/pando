---
created_at: 2026-10-01T10:15:57.773299844Z
updated_at: 2026-10-01T12:26:59.158992114Z
tags:
    - analysis
    - webui
    - acp
    - tui
    - sse
    - delegation
    - api
    - pando
---
# Analysis: no live updates for a run resumed after delegated subagents — WebUI and ACP (2026-10-01)

Tracked in gintrack epic **PANDO-EP-0016** with stories **PANDO-US-0087** (WebUI, first) and **PANDO-US-0088** (ACP, second). NOT FIXED YET.

Related: [[plan_delegated_conclusion_resurrection]], [[webui_pending_askuserquestion_blocks_model_switch]], [[serve-app-mode-part2-endpoints]], [[fix_acp_xcode_compat]]

## Symptom
Parent agent ends its turn waiting for mesnada subagents. When a subagent concludes, the delegation supervisor resumes the idle parent (Case B, needs `mesnada.delegation.resurrectIdleLoop = true`). The client shows nothing live; messages only appear after reloading the conversation.

## Common root cause
`agent.Resume` (`internal/llm/agent/agent.go:708-743`) calls `runInternal` and drains the run channel itself; events reach only the agent pubsub broker.

## WebUI (broken) — PANDO-US-0087
- Live path depends only on `BackgroundSessionManager` (`bgRunner`); `internal/api` never subscribes to the agent broker.
- Every running flag is `bgRunner.IsBusy` (`handlers_sessions.go:68,132`, `handlers_questions.go:106`, `handlers_chat.go:272`), which stays false, so the reattach effect in `web-ui/src/components/chat/ChatView.tsx:114-126` never fires.
- `finishedSessionRef` in `ChatView.tsx` blocks reattach after a real `done` until the user sends a message.
- `dispatchSSEEvent` and the client `SSEEvent` type lack `Resurrected` / `ConclusionQueued` / `ConclusionInjected`.

## ACP (broken) — PANDO-US-0088
- Events come only from the channel returned by `Run`/`RunGoal` for one `session/prompt` (`ForwardACPAgentEvents`, `internal/app/app.go:2545`; stdio adapter `cmd/root.go:752`).
- `internal/mesnada/acp` has no agent/message broker subscription (only `notify` at `agent.go:1376` and design events).
- `ForwardACPAgentEvents` drops unknown types (`default: continue`).
- Steering during a resumed run already works: `Prompt` checks `IsSessionBusy` (`internal/mesnada/acp/agent.go:369`).
- Open question: how clients treat `session/update` sent outside a prompt turn (Xcode groups by messageId).

## TUI (works)
- Global subscriptions to `app.Messages` and `app.CoderAgent` (`cmd/root.go:517,520`).
- Message list appends/updates any message of the open session (`internal/tui/components/chat/list.go:280-328`).
- Busy state from `IsSessionBusy` and broker events (`internal/tui/tui.go:748-757`, `list.go:347`).
- Only gap: no dedicated rendering for `Resurrected`/`Conclusion*` (cosmetic).

## Proposed fix (summary)
Channel-returning `Resume` variant plus a hook letting a surface own the resumed run. WebUI: route through `bgRunner.Submit`, OR busy flag with `agent.IsSessionBusy`, map the three event types to SSE, fix `finishedSessionRef`. ACP: forward the resumed run through the same translation/`SendUpdate` path as a prompt, send post-run updates, verify cancel.

## Verification status
Code reading only; not reproduced at runtime.
