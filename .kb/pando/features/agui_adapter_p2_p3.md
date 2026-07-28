---
created_at: 2026-07-28T15:14:42.735244448Z
updated_at: 2026-07-28T15:14:42.735244448Z
tags:
    - feature
    - agui
    - copilotkit
    - state
    - frontend-tools
    - implementation
---
# Feature: AG-UI adapter — P2 (shared state) + P3 (frontend tools)

**Date:** 2026-07-28
**Status:** P2 and P3 DONE. Builds on [[pando/features/agui_adapter_p0_p1.md]].
**Plan:** [[pando/analysis/copilotkit_agui_integration_analysis.md]] (rev. 2, §3.5 and §3.6)

## What was changed

### P2 — shared state document (`internal/agui/state.go`, new)

AG-UI carries a JSON document beside the message stream: `STATE_SNAPSHOT` publishes it whole,
`STATE_DELTA` patches it with RFC-6902 operations. CopilotKit exposes it to the page through
`useCoAgent` / `useCoAgentStateRender`, which is how a Generative-UI frontend renders a todo list
or a token gauge without parsing chat text.

- `StateDoc` = `{thread, session, agent, model, todos, tokenUsage, files, client}`.
  The JSON tags are the contract: they are the pointer paths the deltas patch.
- `client` echoes the inbound `RunAgentInput.state` — read-only page context, never written back
  into Pando's config.
- `stateTracker.Observe(agent.AgentEvent) []Event`:
  - `todos_updated` → `replace /todos`
  - `token_usage` → `replace /tokenUsage`
  - `tool_result` of a file tool (`view`/`write`/`edit`/`multiedit`/`patch`/`image_crop`) →
    `add /files/-`, or `replace /files/<i>` when a file already listed changes action.
- Deltas are suppressed when the value did not change (`sameJSON`). This matters for token usage,
  which is re-emitted as an estimate on every tool call — without it the state channel would carry
  more traffic than the message channel.
- Failed tool results and non-file tools never enter the file list.
- The empty document serializes `todos`/`files` as `[]`, never `null`, so the first `add` patch has
  a valid target.

`translate.go`: the translator gained `withState(...)`. `Translate` now = `protocol(ev)` plus
`state.Observe(ev)`, interleaved on purpose (a `STATE_DELTA` landing after the tool result it
describes would render a stale document for one frame). With a tracker attached, todos and token
usage travel as state; without one the translator still works standalone and falls back to the
`pando.todos` / `pando.tokenUsage` CUSTOM events, which keeps it unit-testable in isolation.

### P3 — frontend tools (`internal/agui/frontend_tool.go` + `run.go`, new)

A CopilotKit page declares browser-side tools (`useCopilotAction`, `useFrontendTool`,
`useHumanInTheLoop`). AG-UI models the handoff as: agent emits `TOOL_CALL_START/ARGS/END`, run ends
with `RUN_FINISHED{outcome:"interrupt"}`, browser executes, the next POST on the thread carries the
result as a tool message.

- `frontendTool` is an ordinary `tools.BaseTool` whose `Run` blocks on a channel — the same shape
  `permission.Request` and `userinput.Ask` already use. The agent loop just sees a slow tool.
  **Zero changes in `internal/llm/agent`.**
- `pendingRegistry` (per session): `register` arms a slot and announces the suspension on a
  buffered notify channel (dropped if nobody streams — registering must never block the agent
  goroutine); `resolve` returns false for unknown or duplicate ids, so a stale client retry cannot
  be mistaken for a resumption.
- `agentpool.buildLocked` appends the proxies. A declared tool whose name collides with a Pando tool
  is **dropped** — letting a page shadow `bash`/`edit` would turn a rendering hint into a way of
  silently disabling a real capability. `toolSchema` unwraps `properties`/`required` from the client's
  JSON Schema (`ToolInfo.Parameters` is the properties map, not the whole schema); a bare properties
  map is also accepted.
- Client failures come back as **tool errors**, never as run errors: the model must be able to reason
  about them. Same for the 10-minute client timeout.

**Detached runs** (`run.go`) — the load-bearing change. A run used to live exactly as long as its HTTP
request. The handoff breaks that: the result arrives on the *next* request. So runs are now parented
to `Runtime.baseCtx`, and the request context is used only to notice the browser went away.

- `activeRun` per thread in `runStore`; `remove` only deletes if the entry still points at that run,
  so a superseded run cannot evict its replacement (two-tab case).
- `suspendRun` drains the suspending call's still-queued events before writing `RUN_FINISHED` —
  the agent queues `TOOL_CALL_ARGS` *before* invoking the tool, and a client that gets the interrupt
  first has no arguments to execute with. Bounded by `suspendDrainTimeout` (2s).
- A parked run is reaped after `suspendGrace` (tool timeout + 1 min) so a closed tab cannot leak a
  blocked goroutine holding the session busy.
- Every non-suspension exit (`response`, `error`, closed channel, client disconnect, write failure)
  calls `finishRun`.
- A new user turn on a thread with a live run calls `abandonRun`, which cancels and drains until the
  agent goroutine closes the channel — without that wait the following `Run` races it and loses to
  `ErrSessionBusy`.
- `resumeRun` does **not** call `svc.Run`: the same agent goroutine is still blocked inside the tool.
  It mints a new translator (new AG-UI run id ⇒ new message id) but reuses the thread's state tracker,
  and marks the resolved calls via `translator.suppressToolCall` so the resumed run does not echo back
  a result the browser already rendered.

## Files

New: `state.go`, `frontend_tool.go`, `run.go`, `state_test.go`, `frontend_tool_test.go`,
`interrupt_test.go`.
Modified (all inside `internal/agui`): `translate.go` (state hook, `toolSuppressed`),
`server.go` (`handleRun` resume branch, `resumeRun`, `deliverToolResults`, `stream` on `*activeRun`,
`suspendRun`, `drainUntilToolEnded`), `runtime.go` (`baseCtx`, `pending`, `runs`, `Close` cancels
detached runs), `agentpool.go` (proxies + registry), `deps.go` (`suspendDrainTimeout`), `doc.go`
(status), `server_test.go` (test runtime fields).

**Zero existing files outside `internal/agui` were touched** — the P1 gating in `config.go`,
`api/server.go` and `api/routes.go` already covered everything these phases needed.

## Verification

- `go build ./...`, `go vet ./internal/agui ./internal/api ./internal/config`, `gofmt -l` — clean.
- `go test -race ./internal/agui ./internal/api ./internal/config ./internal/llm/agent` — all ok.
- Tests added cover: snapshot shape/pointer paths, todo + token-usage deltas and their suppression,
  file add/replace/ignore, translator routing with and without a tracker; the full proxy handoff,
  timeout-as-tool-error, cancellation, shadowing rejection, schema shapes; and the streaming loop —
  `TOOL_CALL_END` before `RUN_FINISHED{interrupt}`, run kept registered on suspend, unregistered on
  completion/disconnect, `deliverToolResults` selectivity, `abandonRun` drain.
- **Not run:** live E2E against CopilotKit / the AG-UI Dojo — needs provider credentials and a Node
  frontend. That is the P6 deliverable.

## Still deferred

P4 (permissions + `AskUserQuestion` as AG-UI tool calls — `hitl.go` still denies unless
`AutoApprove`), P5 (durable thread↔session table, dedicated listener), P6 (SDK subpath + example),
P7 (embedded CopilotKit route).
