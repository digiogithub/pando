---
created_at: 2026-07-28T15:32:40.045276065Z
updated_at: 2026-07-28T15:32:40.045276065Z
tags:
    - feature
    - agui
    - copilotkit
    - hitl
    - permissions
    - implementation
---
# Feature: AG-UI adapter — P4 (human in the loop)

**Date:** 2026-07-28
**Status:** P4 DONE. Builds on [[pando/features/agui_adapter_p2_p3.md]] and [[pando/features/agui_adapter_p0_p1.md]].
**Plan:** [[pando/analysis/copilotkit_agui_integration_analysis.md]] (rev. 2, §3.7)

## Problem

Invariant I3 gives the adapter its own `permission.Service` and `userinput.Service`, so a prompt raised
by a browser run can never surface in a TUI/Web-UI dialog. Until P4 that left the prompts with nowhere
to go: `hitl.go` denied every permission (unless `AutoApprove`) and cancelled every question, purely so
a run could not hang on a dialog nobody can see.

P4 gives them a destination: the AG-UI client renders them as tool calls, which is exactly what
CopilotKit's `useHumanInTheLoop` expects.

## Design

Both prompts reuse the P3 suspend/resume machinery — a blocked call, `RUN_FINISHED{outcome:"interrupt"}`,
a tool message on the next request — but reach it differently, because only one of them is a real tool call:

| Prompt | Tool call | How |
|---|---|---|
| `AskUserQuestion` | real (`tools.AskUserQuestionToolName`) — the agent's stream already carries START/ARGS/END | the pool **substitutes** the tool with `hitlQuestionTool`: same `Info()` (delegated to the wrapped tool), different waiting room |
| permission request | none — raised *inside* another tool's `Run` | the adapter **synthesizes** START/ARGS/END under `pando_permission_request` |

`permission.Request` calls the per-session handler **synchronously in the tool's goroutine**
(`internal/permission/permission.go:137`), which is what makes blocking there a legitimate way to suspend
the run. No change to `internal/permission` or `internal/userinput`.

### Registry generalisation

`pendingRegistry` now carries the raw client `Message` instead of a `tools.ToolResponse` — a permission
verdict and a tool result are decoded differently by their waiters — and the notification became a
`suspension{callID, events}`. `events` is empty for real tool calls and holds the synthesized prompt for
permissions.

### Handler side (`suspendRun`)

- synthetic suspension → `drainQueued` (flush what the agent already produced, non-blocking: the tool call
  that raised the prompt is guaranteed to be in the buffered channel), then `adoptToolCall(callID)` so the
  translator does not close it a second time, then emit the three events, then the interrupt.
- real tool call → `drainUntilToolEnded` as in P3.

### Carrying closed calls across a resumption

New: `translator.endedSnapshot()` / `inheritEnded()` + `activeRun.rememberEnded()`. The tool that was
blocked on the approval (`bash`) reports its **result in the next run**, whose translator never saw the
call open — without this the client received a second `TOOL_CALL_END` for a call it already watched close.

### Fail-closed decoding

- `approvalFromMessage`: accepts `{"approved":true}`, `{"allow":true}` and the literals
  `true/yes/approve/approved/allow/accept`. **Everything else denies** — empty result, client error,
  unparseable content. An approval must be explicit.
- `answerFromMessage`: renders `{"answers":[{header,selected,otherText}]}` as text for the model;
  `{"cancelled":true}` / empty → the same "user did not answer, use your best judgement" text the local
  tool reports; prose is passed straight through.
- Timeout (`defaultFrontendToolTimeout`, 10 min) or adapter shutdown → deny / cancel.

## Config

New knob `[AGUI] HumanInTheLoop` (default **true**):

- `AutoApprove = true` → approve without asking (unchanged).
- `AutoApprove = false`, `HumanInTheLoop = true` → suspend and ask the client.
- both false → deny and cancel (exact pre-P4 behaviour, still reachable).

`watchQuestions` is kept as a safety net: nothing in the adapter uses the userinput service once the tool
is substituted, but an unattended prompt would block a run forever, so anything reaching it is cancelled.

## Files

- `internal/agui/hitl.go` — rewritten: `askClientForApproval`, `awaitClient`, `permissionArgs`,
  `approvalFromMessage`, `hitlQuestionTool`, `answerFromMessage`, `watchQuestions`.
- `internal/agui/frontend_tool.go` — `suspension` struct; registry keyed on `Message`; `register` takes
  synthetic events.
- `internal/agui/server.go` — `suspendRun` handles synthetic suspensions; new `drainQueued`; `resumeRun`
  inherits closed calls.
- `internal/agui/translate.go` — `adoptToolCall`, `endedSnapshot`, `inheritEnded`.
- `internal/agui/run.go` — `activeRun.ended` + `rememberEnded`/`endedCalls`; `suspend` channel retyped.
- `internal/agui/agentpool.go` — substitutes `AskUserQuestion` when `HumanInTheLoop`.
- `internal/agui/deps.go`, `doc.go`, `runtime.go` — config field, status, log line.
- **`internal/config/config.go`** — the only existing file touched: `HumanInTheLoop` field + viper default
  (~10 LOC, additive, still under the disabled-by-default `[AGUI] Enabled` gate).
- New `internal/agui/hitl_test.go`; existing tests updated for the registry signature change.

## Verification

- `go build ./...`, `go vet ./internal/agui ./internal/api ./internal/config`, `gofmt -l` — clean.
- `go test -race ./internal/agui ./internal/api ./internal/config ./internal/llm/agent ./internal/permission ./internal/userinput` — all ok.
- Tests cover: the full permission round trip through the real `permission.Service` (suspension shape,
  synthetic START/ARGS/END, JSON args, client approval reaching the tool), auto-approve skipping the
  client, denial with HITL off, fail-closed on shutdown, the approval/answer decoding tables, the
  question tool keeping the wrapped schema and name, its client round trip, the handler emitting the
  queued tool call *before* the prompt with exactly one END per call, and the carried-call rule on resume.
- **Not run:** live E2E against CopilotKit's `useHumanInTheLoop` — needs provider credentials and a Node
  frontend (P6).

## Still deferred

P5 (durable thread↔session table, `/info` hardening, dedicated listener / `agui-serve`), P6
(`@pando-ai/sdk/agui` + Next.js example), P7 (embedded CopilotKit route).
