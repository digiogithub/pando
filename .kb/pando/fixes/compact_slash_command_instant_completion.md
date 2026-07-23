---
created_at: 2026-07-23T14:14:15.667453595Z
updated_at: 2026-07-23T14:14:15.667453595Z
tags:
    - fix
    - acp
    - webui
    - compact
    - summarize
---
# Fix: /compact slash command reported instant completion (ACP + WebUI)

## Symptom
Running `/compact` (alias `/summarize`) in ACP mode (Zed etc.) and in the Web UI
returned "Session summary complete" / "Session compacted successfully" immediately,
even with a slow local summarizer model that takes ~1 minute. The summary actually
kept running in the background but the client never saw progress nor real completion.

## Root cause
`agent.Summarize(ctx, sessionID) error` is fire-and-forget: it launches a goroutine
that publishes `AgentEventTypeSummarize` progress events on the **pubsub broker** and
returns `nil` immediately. Its only completion signal is that pubsub broadcast.

- TUI works because it has a global `CoderAgent.Subscribe` subscription and detects
  `payload.Done && Type==Summarize` (`internal/tui/tui.go:694`). No bug there.
- ACP `startManualSummary` (`internal/mesnada/acp/goal_commands.go`) called `Summarize`
  then returned a channel that a goroutine **closed immediately** without ever
  subscribing to pubsub. `processAgentEventStream` saw a closed channel and ended the
  turn at once. A test (`TestStartManualSummaryDoesNotEmitSyntheticSummarizeMessages`)
  even asserted this buggy "emit nothing" behavior.
- WebUI `handleSlashCommandStream` (`internal/api/handlers_chat.go`) called `Summarize`
  and immediately wrote "Session compacted successfully".

## Fix
Added a streaming variant that keeps a channel open until the summary truly finishes.

- `internal/llm/agent/agent.go`: new `SummarizeStream(ctx, sessionID) (<-chan AgentEvent, error)`
  added to the `Service` interface. It emits every progress event both on pubsub
  (so the TUI keeps rendering) **and** on a dedicated channel, closing the channel when
  the summary is persisted or fails. `Summarize` now just calls `SummarizeStream` and
  drains the channel in a background goroutine (fire-and-forget preserved for TUI).
- ACP `AgentService.Summarize` (interface in `types_interfaces.go`) changed to return
  `(<-chan AgentEvent, error)`. Both adapters — `acpAgentAdapter` (`cmd/root.go`) and
  `appACPAgentAdapter` (`internal/app/app.go`) — now call `svc.SummarizeStream` and pipe
  it through their existing `forwardEvents`. Also added `acpEv.Progress = ev.Progress`
  to the summarize case in `cmd/root.go` forwardEvents (was dropping progress text).
  `startManualSummary` now simply returns `agentService.Summarize(...)` so
  `processAgentEventStream` blocks on real completion.
- WebUI `handleSlashCommandStream` now calls `SummarizeStream`, ranges over the events
  streaming each `Progress` as a `content_delta`, and only reports success/error after
  the channel closes.

## Files touched
- internal/llm/agent/agent.go (SummarizeStream + refactor of Summarize)
- internal/mesnada/acp/types_interfaces.go (interface signature)
- internal/mesnada/acp/goal_commands.go (startManualSummary)
- cmd/root.go (acpAgentAdapter.Summarize + forwardEvents Progress)
- internal/app/app.go (appACPAgentAdapter.Summarize)
- internal/api/handlers_chat.go (WebUI compact/summarize case)
- Tests: internal/mesnada/acp/agent_pando_test.go (mock + rewritten
  TestStartManualSummaryStreamsUntilDone), internal/api/handlers_steer_test.go,
  internal/llm/agent/goal_runner_test.go (mock SummarizeStream)

## Verification
- `go build ./...` clean.
- `go vet` clean on affected packages.
- `go test ./internal/llm/agent ./internal/api ./internal/mesnada/acp` all pass.
- The rewritten ACP test now asserts summarize progress streams until the channel
  closes (real completion), instead of the old assertion that encoded the bug.

## Notes
- TUI was never affected. Other slash commands are fine: the synchronous ones
  (`/db-compact`, `/ponytail`, `/caveman`, ...) do their work inline; the LLM-turn ones
  (`/goal`, `/superpowers`, ...) already stream via `processAgentEventStream`. Only the
  async `Summarize` had the mismatch.
