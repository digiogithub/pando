---
id: PANDO-EP-0016
type: epic
title: Live updates for runs resumed after delegated subagents finish (WebUI and ACP)
status: in_review
priority: high
author: mcp
labels: [bug, delegation, webui, acp]
created: 2026-10-01T12:26:30Z
updated: 2026-10-01T13:02:21Z
started: 2026-10-01T13:02:21Z
---

## Description

When a parent agent ends its turn to wait for delegated (mesnada) tasks and the delegation supervisor later resumes the idle session (Case B, `mesnada.delegation.resurrectIdleLoop = true`), the resumed run is persisted but client surfaces that only consume the per-prompt run channel never see it live.

Common root cause: `agent.Resume` (`internal/llm/agent/agent.go:708-743`) starts the run with `runInternal` and drains the returned channel itself. Events reach only the agent pubsub broker.

Per surface (code read 2026-10-01):

- **WebUI: broken.** Live stream and running flag come only from `BackgroundSessionManager`; `internal/api` has no broker subscription. See the WebUI story.
- **ACP: broken.** `internal/mesnada/acp` receives agent events only from the channel returned by `Run` / `RunGoal` for one `session/prompt` (`ForwardACPAgentEvents`, `internal/app/app.go:2545`); it has no agent broker subscription (only `notify` and design events). See the ACP story.
- **TUI: works.** It subscribes globally to `app.Messages` and `app.CoderAgent` (`cmd/root.go:517,520`); the message list appends and updates any message of the open session regardless of who started the run (`internal/tui/components/chat/list.go:280-328`); busy state comes from `IsSessionBusy` and from broker events (`internal/tui/tui.go:748-757`, `list.go:347`). Only gap: no dedicated rendering of the `Resurrected` / `Conclusion*` framing events (cosmetic, out of scope).

## Order of work

1. WebUI story first: it introduces the shared agent-side piece (a channel-returning resume plus a hook that lets a surface own the resumed run).
2. ACP story second: reuses that piece to forward the resumed run as `session/update` notifications.

## Acceptance Criteria

- [ ] Both child stories done and verified.
- [ ] A resumed run is visible live in the WebUI and in an ACP client without reloading the conversation.
- [ ] TUI behaviour unchanged (regression check).
- [ ] `docs/delegation.md` describes how each surface receives resumed-run events.

## Notes

Analysis by code reading only; not yet reproduced at runtime. KB: `pando/analysis/webui-resumed-run-no-live-updates-2026-10-01.md`.
