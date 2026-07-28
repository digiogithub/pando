---
created_at: 2026-07-29T09:02:31.594012827Z
updated_at: 2026-07-29T09:02:31.594012827Z
tags:
    - fix
    - agui
    - copilotkit
    - frontend-tools
    - hitl
---
# AG-UI: tool calls never reached the event stream (browser E2E fix)

Date: 2026-07-29. Found while running the [[agui_adapter_p6_p7]] CopilotKit example
end-to-end in a real browser for the first time.

## Symptom

Two failures, both only visible against a live provider:

1. A frontend tool (`useCopilotAction` with a handler) produced
   `RUN_FINISHED{outcome:"interrupt"}` with **no preceding `TOOL_CALL_START`**.
   The client had no call id and no arguments, so it could not execute anything and
   the run stranded. In CopilotKit the turn simply rendered nothing.
2. Any Pando tool that produced a result made CopilotKit abort the run with
   `Cannot send 'TOOL_CALL_END' event: No active tool call found with ID ...`.

## Root cause (not in the adapter)

`agent.processEvent` publishes `AgentEventTypeToolCall` only from the streaming
branches `provider.EventToolUseStart / Delta / Stop` (internal/llm/agent/agent.go).
A provider that reports its tool calls in one final `EventComplete` (the Copilot
`gpt-5-mini` path used here) sets them on the assistant message — `agent.go:1711`,
`resolveToolCallsOnComplete` — and publishes **nothing**.

TUI, Web-UI and ACP render from the database, so they never noticed. The AG-UI
adapter has only the event stream, so its tool traffic disappeared entirely.

## Fix (adapter-side, invariants intact)

No change to `agent.NewAgent` / `agent.Run` and no new `agent.AgentEvent` type.
The adapter reconstructs what the stream omits, from sources it already holds:

- `internal/agui/frontend_tool.go` — `suspension` now carries `call *toolCallInfo`
  (name + input). `pendingRegistry.register(sessionID, suspension)` replaced the
  positional `(sessionID, callID, synthetic)` form. The frontend-tool proxy and
  `hitlQuestionTool` fill it from their own `tools.ToolCall`, which is authoritative:
  it is the exact call the model made.
- `internal/agui/translate.go` — new `completeToolCall(callID, name, input)` emits
  only the missing part of START / ARGS / END (uses `toolArgsSent`/`toolEnded`, so a
  streamed call is not duplicated). The `AgentEventTypeToolResult` branch now opens a
  call that was never seen before closing it: a bare `TOOL_CALL_END` is a protocol
  error for AG-UI clients and kills the run.
- `internal/agui/server.go` — `suspendRun` replaced `drainUntilToolEnded` (which waited
  up to 2s for events that were never coming) with `drainQueued` + `completeToolCall`.
  A streaming provider queues the call's events *before* its tool runs, so flushing the
  queue is enough and costs nothing when nothing is queued. `drainUntilToolEnded` and
  `suspendDrainTimeout` (deps.go) deleted.

## Tests

- `interrupt_test.go`: `TestStreamDescribesToolCallTheAgentNeverStreamed` (interrupt must
  carry START/ARGS/END built from the suspension) and
  `TestStreamDoesNotDuplicateAStreamedToolCall`.
- `frontend_tool_test.go`: `TestSuppressedToolCallIsNotEchoed` updated — an unseen call's
  result now yields START + END + RESULT.
- `go test -race ./internal/agui ./internal/api` green; SDK suite 90 tests green.

## Browser verification (examples/copilotkit)

Next.js 15.5 + CopilotKit **1.64.1** (the example was written against 1.10; only the
peer-module structural types in `sdk/typescript/src/agui/copilotkit.ts` needed loosening,
plus `exports` reordered so `types` precedes `import`/`require`).

Verified live against `pando agui-serve --port 8098 --no-tls --allow-origin http://localhost:3000`:

- chat streaming + `useCoAgent` dashboard (model, token budget, files, sub-agents)
- frontend tool: agent called `highlight_in_page`, the page highlighted the heading, the
  run resumed and answered `DONE`
- HITL: `write` raised `pando_permission_request`, the in-page card was approved, the file
  was created and `Files touched: write: hello.txt` arrived as a `STATE_DELTA`

## Operational notes

- `agui-serve` blocks silently at startup when another Pando instance holds the project's
  `.pando/ipc.lock`; serve a different `--cwd`. Added to the example's troubleshooting table.
- `examples/copilotkit/.gitignore` widened (node_modules, .next, out, build, dist,
  tsbuildinfo, next-env.d.ts, .env*).
- Pre-existing and **not** changed: the repo `.gitignore` line `/sdk/` (line 39) makes the
  whole TypeScript SDK untracked.

See also [[agui_adapter_p6_p7]], [[agui_adapter_p2_p3]], [[agui_adapter_p4_hitl]],
[[copilotkit_agui_integration_analysis]].
