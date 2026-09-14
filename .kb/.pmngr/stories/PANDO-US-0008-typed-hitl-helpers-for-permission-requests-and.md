---
id: PANDO-US-0008
type: story
title: Typed HITL helpers for permission requests and AskUserQuestion
status: in_review
priority: high
parent: PANDO-EP-0001
milestone: PANDO-M-0001
author: claude
labels: [sdk, typescript, agui, hitl]
estimate: 5
created: 2026-09-13T21:14:35Z
updated: 2026-09-13T21:14:35Z
---

## Description

As a UI developer, I want typed `approve()` / `deny()` / `answerQuestion()` helpers, so that my permission card and question dialog produce exactly the payloads Pando's Go side accepts instead of prose I guessed from `hitl.go`.

Two HITL shapes arrive as tool calls on an interrupted run:

- **Permission**: a synthetic tool call named `pando_permission_request` with args `{toolName, action, description, path, params}` (`internal/agui/hitl.go:38,115-137`). The answer is parsed by `approvalFromMessage` (`hitl.go:140-172`), which accepts `{"approved":true}`, `{"allow":true}`, or the bare literals `true|yes|approve|approved|allow|accept`. **Anything else, including no answer at all, is a deny** — that is the security-relevant default and the helper must document and preserve it.
- **Question**: a real `AskUserQuestion` tool call (`hitl.go:176-220`). The answer may be prose or the structured form `{cancelled, answers:[{questionId, header, selected[], otherText}]}` (`hitl.go:228-262`).

The SDK ships constants and doc comments only: `PERMISSION_TOOL_NAME`, `PandoPermissionRequest` and `PandoPermissionAnswer` are exported from `src/agui/client.ts:37-54` and re-exported by the barrel (`src/agui/index.ts:45-52`); there is no typed question request/answer at all and no function that emits either payload.

Deliver in `src/agui/hitl.ts`:

- `QUESTION_TOOL_NAME`, `PandoQuestionRequest`, `PandoQuestionAnswer` types mirroring `hitl.go:228-262`;
- `isPermissionRequest(toolCall)` / `isQuestionRequest(toolCall)` narrowing guards over the pending tool calls a `PandoThread` exposes;
- `approve()` / `deny(reason?)` returning the canonical `{"approved":boolean}` JSON string, and `answerQuestion({cancelled, answers})` returning the structured JSON;
- a `cancelQuestion()` shortcut producing `{cancelled:true, answers:[]}`.

The helpers return the tool-result payload; the delivery path is `PandoThread.resume(toolCallId, result)`.

Do NOT emit the loose literal forms (`"yes"`, `"allow"`) from the helpers even though the server accepts them — one canonical shape, the literals stay supported only as inputs the server tolerates. Do NOT add a timeout or retry: the suspension window is server-side, 10 minutes (`internal/agui/frontend_tool.go:41`) with the run reaped at 11 (`run.go:28`).

## Acceptance Criteria

- [ ] `approve()` and `deny()` outputs are accepted by `approvalFromMessage` in a round-trip test that runs a real `agui-serve`, triggers a permission prompt, answers with the helper output and asserts the tool ran (approve) or was refused (deny).
- [ ] An empty / malformed answer is shown by test to be treated as a deny, and the deny-by-default rule is stated in the exported doc comment.
- [ ] `answerQuestion()` output is accepted by the Go question parser in the same round-trip harness, including the `cancelled:true` path.
- [ ] `isPermissionRequest` / `isQuestionRequest` narrow correctly against the `pando_permission_request` and `AskUserQuestion` names, and are exported from the browser-safe subpath.
- [ ] Tests in `sdk/typescript/tests/agui-hitl.test.ts`.

## Notes

Size: M. Depends on the `PandoThread` story (it consumes `pendingToolCalls` and `resume()`) and on the browser-safe build story for the export surface. Shares the recorded-fixture harness with the agui test-coverage story of this epic. Evidence: `report-sdk-agui-client.md` §2.3.
