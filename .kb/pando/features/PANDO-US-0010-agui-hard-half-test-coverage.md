---
created_at: 2026-09-14T16:50:40.226734571Z
updated_at: 2026-09-14T16:50:40.226734571Z
tags:
    - feature
    - sdk
    - typescript
    - agui
    - tests
---

# PANDO-US-0010 — agui test coverage for the protocol's hard half

Status: implemented and verified (2026-09-14), with two pre-existing gaps discovered and
documented (not fixed — out of scope). Story:
`.kb/.pmngr/stories/PANDO-US-0010-agui-test-coverage-for-the-protocol-s-hard-half.md`.
Epic: [[PANDO-EP-0001]]. Branch: `feat/pando-us-0006-browser-safe`,
`sdk/typescript` (separate git repo). Builds on
[[PANDO-US-0009-agui-event-input-typings-drift-check]] and
[[PANDO-US-0008-typed-hitl-helpers]]. Last story of PANDO-EP-0001.

## What changed

- **`PandoAguiRunError`** (new, `src/agui/client.ts`): a `RUN_ERROR` SSE event now raises this
  instead of `PandoAguiError(0, message)` — the old code conflated a protocol-level failure
  (rides an otherwise-200 stream) with an HTTP-level one. Carries `code` (mirrors
  `RunErrorEvent.code` / Go's `runErrorCode`). Exported from `agui/index.ts` and
  `agui/client-entry.ts`.
- **Non-SSE `Content-Type` guard** (`PandoAguiClient.run`, `client.ts`): a 200 response whose
  `Content-Type` is not `text/event-stream` (a proxy returning HTML/JSON) now throws
  `PandoConnectionError` instead of silently completing as an empty, successful-looking run.
- **`tests/fixtures/agui/*.sse`** (new): committed raw SSE byte streams —
  `interrupt-frontend-tool.sse`, `resume-frontend-tool.sse`,
  `state-delta-todos-tokenusage-files.sse`. **Provenance caveat**: hand-authored to the exact
  Go wire format (verified against `translate.go`/`state.go`/`frontend_tool.go`/`sse.go`), not
  captured from a live run — this sandbox had no LLM provider credentials and `internal/agui`
  was being edited concurrently by other agents, so a live capture was infeasible and would
  have been unrepeatable. Documented in `tests/fixtures/agui/README.md` and the recorder
  script's own doc comment; regenerate for real with `npm run record:agui-fixtures` once both
  a Go toolchain and provider credentials are available.
- **`scripts/record-agui-fixtures.mjs`** (new): spawns `pando agui-serve --no-tls` on loopback,
  posts real prompts, writes each run's raw response body verbatim to
  `tests/fixtures/agui/*.sse`. Not part of CI, not run by `npm test` — a maintainer tool,
  documented in the SDK README's new "Recording AG-UI fixtures" section. New npm script
  `record:agui-fixtures`.
- **New test files**:
  - `tests/agui-fixtures.test.ts` — replays the `.sse` fixtures: interrupt → pending tool call
    → `resume()` places the tool result after the last user message keyed by `toolCallId`
    (`internal/agui/input.go:216-231`); `STATE_DELTA` (`/todos` + `/tokenUsage` + `/files/-`)
    reduces to the expected `PandoState`.
  - `tests/agui-failure-paths.test.ts` — abort mid-stream (both before headers arrive and
    mid-read, asserting the SSE reader's lock is actually released via
    `stream.getReader()` not throwing afterward), non-SSE `Content-Type` (HTML and JSON
    cases, plus a `text/event-stream; charset=...` positive case), 401 vs 403
    (`internal/agui/server.go:47-71`) distinguishable via `PandoAguiError.status`.
  - `tests/agui-integration.test.ts` — permission/question round trip (approve, deny, malformed
    answer denies, `cancelQuestion()`) against a **real** `pando agui-serve`. Gated behind
    `PANDO_AGUI_INTEGRATION_BIN` (unset → whole suite `describe.skip`s, confirmed: 4/4 skipped
    in this sandbox). Not run here (no binary/credentials); written to run correctly on a
    maintainer's machine. "Question answer" (non-cancel) was already covered by the
    US-0008 fixture-replay test in `tests/agui-hitl.test.ts`, so only `cancelQuestion()` needed
    a new case here per the story's literal list.
  - `tests/bun/agui.test.ts`, `tests/deno/agui_test.ts` (new) — cross-runtime subset (chunk
    reassembly, `runText`, `PandoAguiRunError` vs `PandoAguiError`, 401 status, non-SSE
    rejection, `parentRunId`/`forwardedProps`). `PandoAguiClient`'s injectable `options.fetch`
    made this trivial to port — no subprocess/module mocking needed, unlike
    `tests/bun/client.test.ts`.
  - `tests/agui.test.ts` — 2 new tests (parentRunId/forwardedProps present/omitted, already
    added under US-0009) plus the RUN_ERROR test rewritten to assert `PandoAguiRunError`.

## Pre-existing environment issues found and fixed (blocking, unrelated to agui)

Both `npm run test:bun` and `npm run test:deno` were **completely broken before this task** —
neither had ever successfully run, for reasons unrelated to agui:

- `bunfig.toml`: `preload = []` fails to parse under Bun 1.3.14 ("Expected preload to be an
  array") — blocked every `bun test` invocation, including the pre-existing
  `tests/bun/client.test.ts`/`events.test.ts`. Fixed by removing the empty key (equivalent to
  omitting it).
- `deno.json`'s `test` task: type-checking the whole `tests/deno/` tree failed outright with
  `TypeError: Could not find constraint '@types/node' in the list of packages` before any test
  ran. Fixed by adding `--no-check` (also needed anyway: `src/agui/client.ts`'s relative
  imports one directory deep, e.g. `../exceptions.js`, hit an apparent Deno 2.4.5
  `--unstable-sloppy-imports` resolver bug that drops ambient DOM/web globals — `fetch`,
  `Response`, `AbortController`, `console`, etc. — reproduced in isolation; same-directory or
  root-level relative imports were unaffected). Also added `--unstable-sloppy-imports` (for the
  `.js` → `.ts` resolution the whole file tree relies on) and `--allow-net`/`--allow-write`
  (the latter unblocks `Deno.makeTempDir()` in the pre-existing `client_test.ts`).

After these fixes, `npm run test:deno` runs (it did not before) and reports **19 passed, 5
failed**. All 6 new agui tests pass. The 5 failures are pre-existing and unrelated to agui,
newly exposed only because the suite is now runnable at all:

- 3× `tests/deno/client_test.ts`: `PandoBinaryNotFoundError` — no `pando` binary anywhere on
  this sandbox's PATH, and the test's explicit `pandoPath` option is apparently not taking
  effect under Deno (worth a maintainer look, but subprocess-mode, not agui).
  bug, unrelated to agui.
- 2× `tests/deno/events_test.ts` (`collectResponse`): `TypeError: events is not iterable` — the
  test passes an `AsyncGenerator` to `collectResponse`, but `src/events.ts`'s `collectResponse`
  only accepts a plain `AgentEvent[]` (synchronous `for...of`). Genuine test/implementation
  signature mismatch, unrelated to agui, left untouched — worth its own fix ticket.

Not fixed (out of scope for PANDO-US-0009/0010, which are scoped to `agui`): both left exactly
as found, clearly flagged here for whoever picks them up next.

## Files touched

- `src/agui/client.ts` — `PandoAguiRunError`, SSE `Content-Type` guard, `runText` now throws
  the new class.
- `src/agui/index.ts`, `src/agui/client-entry.ts` — export `PandoAguiRunError`.
- `scripts/record-agui-fixtures.mjs` (new).
- `tests/fixtures/agui/*.sse` (new, 3 files) + `tests/fixtures/agui/README.md` (new).
- `tests/agui-fixtures.test.ts`, `tests/agui-failure-paths.test.ts`,
  `tests/agui-integration.test.ts` (new).
- `tests/bun/agui.test.ts`, `tests/deno/agui_test.ts` (new).
- `tests/agui.test.ts` — RUN_ERROR test updated, 2 new forwardedProps/parentRunId tests
  (shared with the US-0009 write-up).
- `bunfig.toml`, `deno.json` — pre-existing breakage fixed (see above).
- `README.md` — new "Recording AG-UI fixtures" section; `## Building` command list expanded
  (test:bun/test:deno/test:browser-build/check:agui-drift).
- `package.json` — `record:agui-fixtures` script (added alongside `check:agui-drift` under
  US-0009).

## Verified

From `sdk/typescript`: `npm run build` (pass), `npm run typecheck` (pass), `npm test` (140
total: 136 passed, 4 skipped — the integration suite — 0 failed), `npm run test:browser-build`
(pass), `npm run check:agui-drift` (pass), `npm run test:bun` (43/43 pass, was completely
broken before), `npm run test:deno` (19 passed / 5 failed — failures pre-existing and unrelated
to agui, see above; all 6 new agui tests pass).

## Acceptance criteria not fully machine-verified

- The permission/question live round trip (`tests/agui-integration.test.ts`) could not actually
  be executed in this sandbox (no `pando` binary, no LLM provider credentials) — written
  carefully against the real `hitl.go`/`server.go` mechanics (confirmed `HumanInTheLoop`
  defaults `true` via `viper.SetDefault("agui.humanInTheLoop", true)`, so no extra config is
  needed beyond not passing `--auto-approve`), but unverified end-to-end. Gated correctly
  (confirmed 4/4 tests skip cleanly when `PANDO_AGUI_INTEGRATION_BIN` is unset).
- The committed `.sse` fixtures are hand-authored to the Go wire format rather than genuinely
  recorded (see provenance note above and in `tests/fixtures/agui/README.md`).