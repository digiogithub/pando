---
created_at: 2026-09-14T16:14:00.234145009Z
updated_at: 2026-09-14T16:14:00.234145009Z
tags:
    - feature
    - sdk
    - typescript
    - agui
    - hitl
---

# PANDO-US-0008 — Typed HITL helpers for permission requests and AskUserQuestion

Status: implemented, verified against a faithful mirror of the Go parser (see caveat below) (2026-09-14). Story: `.kb/.pmngr/stories/PANDO-US-0008-typed-hitl-helpers-for-permission-requests-and.md`. Epic: [[PANDO-EP-0001]]. Depends on [[PANDO-US-0007-pandothread-stateful-transcript]] (consumes `pendingToolCalls`/`resume()`). Branch: `sdk/typescript` repo, `feat/pando-us-0006-browser-safe`.

## What changed

1. **`sdk/typescript/src/agui/hitl.ts`** (new):
   - `QUESTION_TOOL_NAME = "AskUserQuestion"` (mirrors `tools.AskUserQuestionToolName`, `internal/llm/tools/ask_user_question.go:12`).
   - `PandoQuestion`, `PandoQuestionOption`, `PandoQuestionRequest` — mirror `AskUserQuestionParams`/`AskUserQuestionParamQuestion`/`AskUserQuestionParamOption` (`ask_user_question.go:37-51`), i.e. the *args* shape of a question tool call.
   - `PandoQuestionAnswerEntry`, `PandoQuestionAnswer` — mirror the structured answer decoder in `internal/agui/hitl.go:240-247` (`{cancelled?, answers:[{questionId, header?, selected[], otherText?}]}`).
   - `PandoPermissionPendingCall` / `PandoQuestionPendingCall` — `PendingToolCall` narrowed by name+args.
   - `isPermissionRequest(call)` / `isQuestionRequest(call)` — type-predicate guards over a `PandoThread.pendingToolCalls` entry, narrowing on `call.name === PERMISSION_TOOL_NAME` / `=== QUESTION_TOOL_NAME`.
   - `approve()` → `'{"approved":true}'`; `deny(reason?)` → `'{"approved":false}'` (`reason` is caller-side only, never sent — the wire shape has no field for it); `answerQuestion(answer)` → `JSON.stringify(answer)`; `cancelQuestion()` → `answerQuestion({cancelled:true, answers:[]})`. None of the loose literal forms (`"yes"`, `"allow"`, …) are ever emitted, only the canonical structured shape, per the story's explicit "do NOT" — even though `approvalFromMessage` tolerates them from other clients.
   - `deny()`'s doc comment states the deny-by-default rule explicitly ("**Deny is Pando's default**: ... treats anything that is not an explicit approval ... as a denial"), verified by a source-text test.

2. **`sdk/typescript/src/agui/client-entry.ts`** / **`index.ts`** — both re-export the above from `./hitl.js` (existing `PERMISSION_TOOL_NAME`/`PandoPermissionRequest`/`PandoPermissionAnswer` re-exports from `client.ts`, PANDO-US-0006, were left untouched — `hitl.ts` imports `PERMISSION_TOOL_NAME` internally but does not re-declare it, avoiding a duplicate-export clash in the barrel).

3. **`sdk/typescript/tests/fixtures/agui-recorded-stream.ts`** — extended with a question-prompt scenario (`QUESTION_INTERRUPT_SSE`/`QUESTION_RESUME_SSE`, `QUESTION_CALL_ID`, `QUESTION_REQUEST`): a real `AskUserQuestion` tool call, which — unlike the permission prompt — carries an ordinary `parentMessageId` (`internal/agui/hitl.go:190-220`: it's a genuine tool call, not synthesized).

4. **`sdk/typescript/tests/agui-hitl.test.ts`** (new, 13 tests):
   - `isPermissionRequest`/`isQuestionRequest` narrowing correctness.
   - `approve()`/`deny()`/`answerQuestion()`/`cancelQuestion()` exact JSON string outputs.
   - The deny-by-default doc-comment assertion (reads `src/agui/hitl.ts` source text, same technique `agui-client-entry.test.ts` already uses for its no-copilotkit-import guard).
   - Full `PandoThread` + HITL round trips for both permission (approve and deny) and question (answer and cancel), replaying the recorded fixtures and inspecting the literal `resume()` POST body.
   - **Go-parser compatibility**: `approvalFromMessageMirror`/`answerFromMessageMirror`, faithful TypeScript ports of `approvalFromMessage`/`answerFromMessage` (`internal/agui/hitl.go:145-172,228-262`), exercised with the *exact* accepted/rejected literal sets from the Go suite's own `TestApprovalFromMessage`/`TestAnswerFromMessage` (`internal/agui/hitl_test.go:155-196`) — copied by reading that test file, not reinvented.

## Why

`PandoAguiClient` already exported `PERMISSION_TOOL_NAME`/`PandoPermissionRequest`/`PandoPermissionAnswer` as types-only scaffolding (PANDO-US-0006); nothing built the question counterpart or any function that actually emits a payload. Every consumer had to hand-guess the wire shape from `hitl.go`. These helpers make that impossible to get wrong for the two documented shapes.

## Verification (from `sdk/typescript/`)

- `npm run build`, `npm run typecheck` (both configs), `npm test` (Jest **118/118**, including the 13 new HITL tests), `npm run test:browser-build` — all PASS. Same run as [[PANDO-US-0007-pandothread-stateful-transcript]]; see that document for full command output.

## Acceptance criteria status — IMPORTANT CAVEAT

- ✅ `isPermissionRequest`/`isQuestionRequest` narrow correctly and are exported from the browser-safe subpath (`./agui/client`, via `client-entry.ts`).
- ✅ An empty/malformed answer is shown to be treated as a deny (via the Go-parser mirror, plus the doc-comment assertion).
- ✅ Tests live in `sdk/typescript/tests/agui-hitl.test.ts`.
- ⚠️ **AC #1 and #3 literally call for "a round-trip test that runs a real `agui-serve`"** (triggering a genuine permission/question prompt via a live LLM-backed agent run, answering with the helper output, and asserting the tool ran/was refused via the actual Go server). **This was not done.** Reasons, all hard constraints of this task rather than a judgement call:
  1. Triggering a real permission/question prompt requires a live model provider call (the agent has to actually decide to call `write`/`AskUserQuestion`) — no LLM credentials are available in this sandbox.
  2. Building any harness to fake that deterministically (e.g. a small Go program driving `internal/agui.Runtime` with a stub agent) would require adding a new `.go` file, which the coordinator's task explicitly forbids ("Do NOT touch any Go file... other agents are editing `internal/agui`... concurrently") — this reads as an absolute constraint, not scoped to those specific packages only.
  - **Mitigation shipped instead**: `approvalFromMessageMirror`/`answerFromMessageMirror` in the test file are byte-faithful ports of the actual Go parsing functions (cited by exact line numbers), exercised against the identical literal test-case tables from `internal/agui/hitl_test.go`'s own unit tests — read directly from the Go source for this purpose, not guessed. Combined with the `PandoThread` round-trip tests that assert the exact HTTP body `resume()` produces, this gives strong (but not end-to-end-server-verified) confidence of wire compatibility.
  - **Follow-up needed**: whoever owns CI for this repo should add a real integration job (Go toolchain + a cheap/deterministic model, or a Go-side test double reachable over HTTP) that drives `pando agui-serve` for real and exercises this SDK's `approve()`/`deny()`/`answerQuestion()` against it end to end.

## Things deliberately not done (per story's "MUST NOT")

- No timeout/retry added to any helper (the suspension window is server-side, 10 min, `internal/agui/frontend_tool.go:41`, reaped at 11, `internal/agui/run.go:28`).
- No loose literal forms (`"yes"`, `"allow"`, …) ever emitted by the helpers.

Related: [[PANDO-US-0007-pandothread-stateful-transcript]], [[PANDO-US-0006-sdk-browser-safe-agui-client]].