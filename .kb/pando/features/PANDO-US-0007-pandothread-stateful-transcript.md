---
created_at: 2026-09-14T16:13:33.757656254Z
updated_at: 2026-09-14T16:13:33.757656254Z
tags:
    - feature
    - sdk
    - typescript
    - agui
    - pandothread
---

# PANDO-US-0007 — PandoThread: a stateful transcript, state and interrupt helper

Status: implemented and verified (2026-09-14). Story: `.kb/.pmngr/stories/PANDO-US-0007-pandothread-a-stateful-transcript-state-and-interrupt-helper.md`. Epic: [[PANDO-EP-0001]]. Branch: `sdk/typescript` repo, `feat/pando-us-0006-browser-safe` (continues on top of [[PANDO-US-0006-sdk-browser-safe-agui-client]]).

## What changed

All inside `sdk/typescript/` (separate git repo, own remote `pando-typescript-sdk`; no Go files touched).

1. **`sdk/typescript/src/agui/thread.ts`** (new) — `PandoThread` class composing `PandoAguiClient` (its signature is untouched):
   - `threadId` generated once (`randomId("thread")`) or reused from `options.threadId` (a Pando session id, per `internal/agui/runtime.go:138-144`).
   - `messages: AguiMessage[]` — reduces `TEXT_MESSAGE_START/CONTENT/END` into an assistant message keyed by `messageId`; `TOOL_CALL_START/ARGS/END` into `AguiToolCall` entries appended to that same message's `toolCalls` (or, for the permission prompt's synthetic call which carries no `parentMessageId`, `internal/agui/hitl.go:78`, a dedicated message keyed `toolcall-<callId>`); `TOOL_CALL_RESULT` into a separate `tool`-role message.
   - `reasoning: Map<messageId, string>` — `REASONING_MESSAGE_CONTENT` deltas accumulate here, never into `AguiMessage.content`.
   - `state: PandoState | undefined` — seeded by `STATE_SNAPSHOT` (deep-cloned via JSON round-trip), patched by `STATE_DELTA` through a hand-rolled RFC-6902 applier (`applyJsonPatch`, exported): supports `add`/`replace`/`remove` including the `/files/-` append form and RFC-6901 pointer escaping (`~0`/`~1`); `move`/`copy`/`test` throw (Pando never emits them). Throws if a delta arrives before any snapshot. Zero new runtime dependency (no `fast-json-patch`).
   - `customEvents: PandoCustomEvent[]` + `options.onCustom` callback — every `CUSTOM` `pando.*` event is kept/forwarded, never dropped.
   - `isInterrupted` / `pendingToolCalls: PendingToolCall[]` (`{id, name, argsText, args}`) — recomputed on `TOOL_CALL_END`/`TOOL_CALL_RESULT`/`RUN_FINISHED`; true after `RUN_FINISHED{outcome:"interrupt"}`.
   - `send(prompt, options?)` — pushes a user message, resends the full accumulated `messages` array (AG-UI clients own the visible history).
   - `resume(toolCallId, result, options?)` — appends a `tool`-role message (`{id, role:"tool", toolCallId, content: result}`) after the current transcript tail and reruns, producing the exact shape `internal/agui/input.go:216-231` (`TrailingToolMessages`) requires for `deliverToolResults`/`resumeRun` (`internal/agui/server.go:356-399`) to re-attach instead of starting a new run. Removes the id from the pending set immediately (the adapter never echoes a suppressed call's result back via `translator.suppressToolCall`).
   - Both `send`/`resume` return `AsyncGenerator<AguiEvent>`; draining it is what applies the reduction (mirrors `PandoAguiClient.run()`'s own generator style) — a caller can render live while it drains, or just `for await (...) {}` to settle state.

2. **`sdk/typescript/src/agui/client-entry.ts`** and **`sdk/typescript/src/agui/index.ts`** — both now additionally re-export `PandoThread`, `applyJsonPatch` and their types from `./thread.js`. No new `package.json` subpath or tsup entry was needed: `thread.ts` is bundled as part of the existing `./agui/client` (browser-safe) and `./agui` entries.

3. **`sdk/typescript/tests/fixtures/agui-recorded-stream.ts`** (new) — literal SSE frame text (not hand-built `AguiEvent` objects) mirroring three real turns: a basic run (reasoning + text + tool call/result + `/todos`+`/tokenUsage`+`/files/-` `STATE_DELTA`s + one `CUSTOM`), a permission-prompt interrupt, and its resume. Reused by both this story's tests and PANDO-US-0008's.

4. **`sdk/typescript/tests/agui-thread.test.ts`** (new, 9 tests) — feeds the fixtures through a mocked `fetch` into a real `PandoAguiClient`/`PandoThread`, asserting: transcript assembly with reasoning kept separate; thread id reuse; `pendingToolCalls`/`isInterrupted` after an interrupt; the exact `resume()` HTTP body shape (tool message after the assistant tool-call message, both after the last user message); `applyJsonPatch` add/replace/remove/`/files/-`/pointer-escaping/unsupported-op/out-of-bounds behavior; and that an out-of-order `STATE_DELTA` throws.

5. **`sdk/typescript/package.json`** — `jest.transform["^.+\\.tsx?$"][1].tsconfig` gained `"rootDir": "."` (ts-jest only; the real `tsconfig.json` used by `tsup`/`tsc` still has `rootDir: "src"`). Needed because `tests/agui-thread.test.ts` and `tests/agui-hitl.test.ts` both import the shared fixture under `tests/fixtures/`, which sits outside `src/`; without this, ts-jest failed every such test file with `TS6059`.

## Why

`PandoAguiClient.run()` was transport-only — no transcript accumulation, no `STATE_DELTA` application, no interrupt detection — so every consumer had to re-derive Pando's event-reduction and resume rules from `internal/agui`. `PandoThread` is the client-side owner of that projection. It also unblocks PANDO-US-0008 (HITL helpers), which consumes `pendingToolCalls` and `resume()`.

## Verification (from `sdk/typescript/`)

- `npm run build` — tsup, clean; `dist/agui/client-entry.js` unaffected (thread.ts has no Node/CopilotKit imports).
- `npm run typecheck` — `tsc --noEmit` (main) and `tsc --noEmit -p tsconfig.agui-browser.json` (DOM-only, `src/agui/` scope) both clean.
- `npm test` — Jest, **118/118 passed** (96 pre-existing + 22 new across `agui-thread.test.ts` and `agui-hitl.test.ts`, see [[PANDO-US-0008-typed-hitl-helpers]]).
- `npm run test:browser-build` — real Vite build of the `tests/browser-build/` fixture against the freshly built `dist/`: PASS, zero warnings, no `copilotkit`/`node:` string in the bundle (`grep -il copilotkit` on every chunk `client-entry.js` imports returns nothing; a `copilotkit`-mentioning chunk exists but is only reachable from `index.js`/`copilotkit.js`, not `client-entry.js`).

## Acceptance criteria status

All 6 satisfied:
- ✅ `send("hi")` yields a transcript with the user + assembled assistant message; reasoning retrievable via `thread.reasoning`, never in `content`.
- ✅ `STATE_DELTA` covers add/replace/remove + `/files/-`, tested via a replayed `/todos`+`/tokenUsage`+`/files/-` sequence (recorded-shape fixture) plus a focused `applyJsonPatch` unit test for `remove` (Go doesn't currently emit `remove` for `files`/`todos`, so that case is exercised as a pure-function test rather than forced into the recorded stream).
- ✅ Interrupt detection: `isInterrupted`/`pendingToolCalls` after `RUN_FINISHED{outcome:"interrupt"}`.
- ✅ `resume()` shape verified by inspecting the literal POST body.
- ✅ Tests live in `sdk/typescript/tests/agui-thread.test.ts`, replaying recorded SSE bytes.
- ✅ No new runtime dependency in `package.json` (only a test-tooling `rootDir` tweak inside the existing `ts-jest` transform config).

## Things deliberately not done (per story's "MUST NOT")

- `PandoAguiClient`'s signature is untouched.
- No thread-list/history-fetch/reattach endpoint invented — `PandoThread` is purely a client-side projection.
- No `fast-json-patch` or any other runtime dependency added.

Related: [[PANDO-US-0006-sdk-browser-safe-agui-client]], [[PANDO-US-0008-typed-hitl-helpers]] (built on top of this).