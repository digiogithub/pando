---
id: PANDO-US-0010
type: story
title: agui test coverage for the protocol's hard half
status: backlog
priority: medium
parent: PANDO-EP-0001
milestone: PANDO-M-0001
author: claude
labels: [sdk, typescript, agui, tests]
estimate: 5
created: 2026-09-13T21:14:35Z
updated: 2026-09-13T21:14:35Z
---

## Description

As a maintainer, I want the interrupt, permission, state-patch and failure paths under test against fixtures recorded from a real server, so that a change to the Go adapter cannot silently break every browser client.

Today `sdk/typescript/tests/agui.test.ts` (318 lines) has 7 tests for `PandoAguiClient`/`parseSSE` — bearer header and URL shape, chunk-boundary reassembly, `runText` concatenation, `RUN_ERROR`, HTTP error-status mapping, the no-token case and `/info` — plus 5 for the CopilotKit glue. All of them mock `fetch`. The Bun and Deno suites (`tests/bun/`, `tests/deno/`) do not touch agui at all.

Add:

1. **A fixture recorder.** A script that runs `pando agui-serve --no-tls` on loopback, drives real runs, and writes the raw SSE byte streams to `tests/fixtures/agui/*.sse`. Fixtures are committed; the recorder is not part of CI.
2. **Interrupt then resume**: replay a stream ending in `RUN_FINISHED{outcome:"interrupt"}`, assert the pending tool call, call `resume()`, assert the second request body places tool messages after the last user message with matching `toolCallId` (`internal/agui/input.go:216-231`).
3. **Permission and question round-trips**: with the helpers from the HITL story, against a real `agui-serve` in an integration-tagged test — approve runs the tool, deny refuses it, a malformed answer denies, `cancelled:true` cancels the question.
4. **`STATE_DELTA` application**: a recorded `/todos` + `/tokenUsage` + `/files/-` sequence reduces to the expected `PandoState`.
5. **Failure paths**: an aborted stream mid-run (assert the `AbortSignal` path at `src/agui/client.ts:225-244` rejects cleanly and does not leave the reader open); a non-SSE response (a proxy returning HTML currently yields an empty, successful-looking run because nothing checks `Content-Type: text/event-stream` — add the check and test it); the 401 vs 403 distinction, since a missing token and a disallowed `Origin` are different server conditions (`internal/agui/server.go:47-71`).
6. **Cross-runtime**: the Bun and Deno suites import and run the agui client tests.

Also fix the correctness nit the tests will expose: `PandoAguiError` is constructed with `status: 0` for a `RUN_ERROR` (`client.ts:198`), conflating a protocol error with an HTTP one — give protocol errors their own discriminator.

Do NOT make CI depend on a live `agui-serve`: the round-trip tests are integration-tagged and skipped when the binary is absent; the replay tests must run from committed fixtures with no network.

## Acceptance Criteria

- [ ] `tests/fixtures/agui/` holds committed SSE fixtures produced by the recorder script, and the recorder is documented in the SDK README.
- [ ] Tests cover: interrupt then resume, permission approve/deny/malformed, question answer and cancel, `STATE_DELTA` application, abort mid-stream, non-SSE content-type, 401 vs 403.
- [ ] A non-`text/event-stream` response is rejected with a distinguishable error instead of completing as an empty run.
- [ ] A `RUN_ERROR` produces an error distinguishable from an HTTP failure without inspecting `status === 0`.
- [ ] `npm test`, `npm run test:bun` and `npm run test:deno` all exercise agui; the fixture-replay tests pass with no network.

## Notes

Size: M. Depends on the `PandoThread` story (transcript and `resume`), the HITL helpers story (answer payloads) and the typings story (event interfaces the assertions use). Last story of PANDO-EP-0001 in order. Evidence: `report-sdk-agui-client.md` §7.
