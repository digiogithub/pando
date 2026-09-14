---
id: PANDO-US-0007
type: story
title: "PandoThread: a stateful transcript, state and interrupt helper"
status: done
priority: high
parent: PANDO-EP-0001
milestone: PANDO-M-0001
author: claude
labels: [sdk, typescript, agui]
estimate: 8
created: 2026-09-13T21:14:35Z
updated: 2026-09-13T21:14:35Z
---

## Description

As a chat-panel developer, I want a `PandoThread` object that owns the thread id, the transcript and the state document, so that I do not have to re-derive Pando's event reduction and resume rules from the Go source.

`PandoAguiClient` today is transport plus type declarations: `run()` yields events and `runText()` concatenates `TEXT_MESSAGE_CONTENT` (`src/agui/client.ts:192-202`). Nothing accumulates a transcript, nothing applies `STATE_DELTA`, nothing detects an interrupt.

Build `src/agui/thread.ts` exporting a `PandoThread` class over `PandoAguiClient`:

- owns `threadId` (generated once, reused across runs — the Go side honours a Pando session id used as the threadId, `internal/agui/runtime.go:138-144`), `messages: AguiMessage[]` and `state: PandoState`;
- reduces `TEXT_MESSAGE_START/CONTENT/END` into assistant messages, `REASONING_START / REASONING_MESSAGE_START / REASONING_MESSAGE_CONTENT / REASONING_MESSAGE_END / REASONING_END` (`internal/agui/translate.go:169-176,282-291`) into a separate reasoning channel that is NOT merged into the visible text, and `TOOL_CALL_START/ARGS/END` + `TOOL_CALL_RESULT` (`translate.go:254-279,200`) into tool calls with their results;
- seeds `state` from `STATE_SNAPSHOT` (emitted after every `RUN_STARTED`, `internal/agui/server.go:316`, and on resume at `:377`) and applies `STATE_DELTA` with a hand-rolled RFC 6902 applier supporting `add`/`replace`/`remove`, including the `/files/-` append form. Pando only emits the pointer shapes at `internal/agui/state.go:252,274,328,334`; do NOT add `fast-json-patch` or any other runtime dependency for this;
- surfaces `CUSTOM` `pando.*` signals (`pando.summarize`, `pando.frontendToolsDisabled`, `pando.<agentEventType>`) through a callback rather than swallowing them;
- detects `RUN_FINISHED{outcome:"interrupt"}` (`internal/agui/events.go:220-227`, emitted at `server.go:470-504`) and exposes the pending tool calls;
- exposes `resume(toolCallId, result)` that POSTs the same threadId with the accumulated transcript plus tool messages placed **after the last user message** with matching `toolCallId` — the exact shape `internal/agui/input.go:216-231` requires, which `deliverToolResults`/`resumeRun` (`server.go:356-399`) consume to re-attach without starting a new agent run.

Do NOT invent server routes: there is no thread list, no history fetch and no reattach endpoint (`server.go:26-33` mounts `/info`, `OPTIONS` and the run POST only), and `MESSAGES_SNAPSHOT` is never emitted. `PandoThread` is the client-side owner of the transcript, full stop. Do NOT change `PandoAguiClient`'s signature; `PandoThread` composes it.

## Acceptance Criteria

- [ ] `new PandoThread({client}).send("hi")` yields a transcript containing the user message and the assembled assistant message; reasoning text is retrievable separately and never concatenated into the assistant content.
- [ ] `STATE_DELTA` application covers `add`, `replace`, `remove` and the `/files/-` append form; a unit test replays a recorded `/todos` + `/tokenUsage` + `/files/-` sequence and asserts the resulting `PandoState`.
- [ ] A run ending in `RUN_FINISHED{outcome:"interrupt"}` leaves `thread.pendingToolCalls` non-empty and `thread.isInterrupted === true`.
- [ ] `resume(toolCallId, result)` produces a `RunAgentInput` whose `messages` end with the assistant tool-call message followed by tool messages, all after the last user message, and is accepted by the Go side (see the round-trip test in the HITL story).
- [ ] Tests live in `sdk/typescript/tests/agui-thread.test.ts` and replay a recorded Pando SSE stream rather than hand-written events.
- [ ] No new runtime dependency appears in `package.json`.

## Notes

Size: L. Depends on the browser-safe build story of this epic for the export surface (`./agui/client` / a `./agui/thread` subpath), but implementation can start in parallel. Feeds the HITL helpers story, which builds its typed answers on `resume()`. Evidence: `report-sdk-agui-client.md` §2.2-§2.4 and §3.1. Decision already taken: hand-rolled patch applier over `fast-json-patch`.
