---
created_at: 2026-09-14T21:11:42.874886386Z
updated_at: 2026-09-14T21:11:42.874886386Z
tags:
    - feature
    - fix
    - agui
    - hitl
    - tests
    - sdk-typescript
---
# PANDO-T-0002 — HITL round trip against a live agui-serve, via a deterministic Go fixture agent

**Date:** 2026-09-14
**Status:** DONE. Closes [[.pmngr/tasks/PANDO-T-0002-round-trip-the-hitl-helpers-against-a-live-agui-serve.md]],
unblocks [[pando/features/PANDO-US-0008-typed-hitl-helpers.md]] and
[[pando/features/PANDO-US-0010-agui-hard-half-test-coverage.md]] on their remaining live-round-trip
acceptance criteria. Builds on [[pando/features/agui-run-parking-reattach-cancel.md]] (the pump/attach
run lifecycle this task exercised end-to-end for the first time and found a bug in).

## Problem

PANDO-US-0008 shipped `sdk/typescript/src/agui/hitl.ts` as faithful ports of
`internal/agui/hitl.go`'s `approvalFromMessage`/`answerFromMessage`, tested against literal cases and
against recorded-fixture replays -- but never against a real, live `agui-serve` actually raising a
permission prompt or an `AskUserQuestion` and being resumed. The chosen route (of the task's two
options) was a Go-side deterministic test fixture agent, no LLM call, driven from the SDK's existing
(previously skipped) `sdk/typescript/tests/agui-integration.test.ts`.

## The fixture agent

New files, all gated behind Go build tag `agui_fixture_agent` (never passed by `cmd/pando`'s default
build or any release build):

- `internal/config/fixture_agent.go` -- declares `AgentFixtureHITL AgentName = "fixture-hitl"` and an
  `init()` appending it to `KnownAgentNames`. Without the tag, the name is unknown, so
  `agui.ConfigFromApp` silently drops it from `AGUI.Agents`/`--agent` like any typo'd name -- the
  fixture agent cannot even be *selected* in a production binary, let alone activated.
- `internal/llm/agent/fixture_hitl_agent.go` -- `fixtureHITLProvider`, a `provider.Provider`
  implementation that never touches the network. `maybeFixtureProvider(agentName)` is the hook:
  returns the fixture provider only when `agentName == FixtureHITLAgentName` **and**
  `PANDO_AGUI_FIXTURE_AGENT=1` is set in the process environment (second, independent gate).
- `internal/llm/agent/fixture_hitl_agent_stub.go` -- the `!agui_fixture_agent` build of
  `maybeFixtureProvider`, which always returns `(nil, false)`. This is what ships in every normal
  binary; the fixture provider type is not even compiled in.
- `internal/llm/agent/agent.go` -- `createAgentProvider` (the single choke point every provider
  construction path uses: `NewAgent`'s agentProvider/titleProvider/summarizeProvider, and
  `prepareProvider`'s per-turn rebuild) now calls `maybeFixtureProvider` first and returns early on a
  hit. 7 lines, purely additive.

**Why this design reaches the real code paths**: the fixture only fakes the *model*. Tool
construction (`agent.CoderAgentToolsWithMesnada`), the HITL substitution
(`agentPool.buildToolsLocked` wrapping `AskUserQuestion` with `hitlQuestionTool`),
`permission.Service`, the run lifecycle (`Runtime.pump`, suspend/resume, `beginResumeSegment`) and the
SSE writer are all untouched, real, and exercised exactly as a live LLM would exercise them --
confirmed by manual curl round trips and by the Jest suite below.

**Fixture behaviour** (`fixtureHITLProvider.decide`, driven by the last message in the turn's
history): a fresh user turn whose text contains `"askuserquestion"` (case-insensitive) raises a
deterministic 2-question `AskUserQuestion` call (q1 single-select "Environment"; q2
`multiSelect:true` "Frameworks") via the real tool; text containing `"write tool"` parses a `named
<file> ... contents '<text>'` shape (falling back to fixed values) and calls the real `write` tool,
which requires permission -- exactly hitl.go's synthesized `pando_permission_request` path. Any turn
whose last message is already a tool result just ends with a short text reply
(`FinishReasonEndTurn`) -- the fixture only needs to prove the round trip, not hold a conversation. A
150ms `fixtureResponseDelay` before every `StreamResponse` answer is deliberate -- see "Bug found"
below.

## Bug found and fixed: resume/reattach replayed stale history, not the live segment

While building the fixture, the very first resume (`approve()` after a permission interrupt) reliably
showed a resuming client's own HTTP response replaying **the old interrupted segment again**
(byte-identical, same timestamps) instead of the new segment's `RUN_STARTED`/tool-result/completion --
even though the server-side run had genuinely completed (file written, `RUN_FINISHED{success}`
happened). A second reattach (`GET .../threads/{id}/stream`) then answered 404 "no live run for this
thread": the run had already finished and been unregistered, so there was **no way** for a client to
ever observe the successful completion via the AG-UI stream contract.

Root cause: `internal/agui/run.go`'s `eventBuffer` is one ring spanning a run's *entire* lifetime
across every segment, with no per-segment cursor. `attachRun`'s `!first` (resume/reattach) path always
replayed from the buffer's absolute start and stopped at the *first* segment boundary it found
(`isSegmentBoundary`) -- which, for any run that had already interrupted once, is always the oldest
interrupt still in the buffer, never the segment the attaching request actually needs. This is not a
race (reproduced identically with and without added latency) -- it is deterministic, and would affect
any resume of any previously-interrupted run, real LLM or not; a real model's turn-taking cadence
apparently never happened to be exercised this way end-to-end with an assertion on the client's own
observed stream state before.

Fix (`internal/agui/run.go`, `internal/agui/server.go`):
- `eventBuffer` gained a `dropped` counter, `nextIndex()` (absolute position of the next append) and
  `snapshotFrom(from int)` (replay from an absolute position, translating around any eviction).
- `activeRun` gained `segmentStart int` + `markSegmentStart()`/`currentSegmentStart()`.
- `Runtime.beginResumeSegment` calls `run.markSegmentStart()` immediately before broadcasting the new
  segment's `RUN_STARTED`, so the mark lands exactly where that event does.
- `activeRun.replaySnapshot()` now calls `snapshotFrom(currentSegmentStart())` instead of a full-buffer
  `snapshot()`.

A run that has never resumed keeps `segmentStart == 0`, i.e. byte-identical behaviour to before for a
plain single-segment reattach. `internal/agui/run_test.go`'s `TestEventBufferBoundedAndLossy` (tests
`snapshot()` directly, unchanged) and `waitForBuffered` (uses `replaySnapshot()` with no resume in
play, so `segmentStart==0`) both still pass. The 150ms `fixtureResponseDelay` in the fixture provider
is a defense-in-depth test-harness choice, not a substitute for this fix -- it was added and proven
*not* to matter before the real fix (the bug reproduced with the delay too; only became invisible
after the buffer/segment fix landed).

## SDK side

`sdk/typescript/tests/agui-integration.test.ts` rewritten:
- No more `PANDO_AGUI_INTEGRATION_BIN`/LLM-credentials gate. `beforeAll` builds
  `go build -tags agui_fixture_agent -o <tmp>/pando-fixture .` from `PANDO_REPO_ROOT` (default: 3
  dirs up from the test file) into a fresh temp dir, unless `PANDO_AGUI_FIXTURE_BIN` overrides it. The
  suite is `describe.skip` only when neither override is given and `go version` fails -- otherwise it
  now runs as part of plain `npm test`.
- Spawns `agui-serve --agent fixture-hitl` with `PANDO_AGUI_FIXTURE_AGENT=1` **and an isolated
  `HOME`/`XDG_CONFIG_HOME`** for the child process. This isolation was not optional: `agui-serve` loads
  the real `~/.config/pando` (there is no subprocess equivalent of Go's
  `config.IsolateForTests`/`internal/config/testing.go`, which only isolates in-process Go tests), and
  a developer machine's own config can set `AutoApprove`/disable `HumanInTheLoop` for convenience --
  exactly what this sandbox's config did, silently auto-denying every permission prompt with no
  suspension at all until isolated.
- Fixed a **latent, never-exercised bug in the test itself**: `thread.pendingToolCalls[0]` is not
  reliably the permission/question call. For a permission prompt, the model's own wrapping tool call
  (e.g. `write`) also has a `TOOL_CALL_END` with no `TOOL_CALL_RESULT` yet in that segment (it's
  blocked mid-execution, not finished), so it is *also* "pending" per `PandoThread`'s tracking, and it
  streamed first. All three permission tests now select with
  `thread.pendingToolCalls.find(isPermissionRequest)`. (`AskUserQuestion` has only one pending call --
  it's a real single tool call from the start -- so this never affected the question tests.)
- New test: `answerQuestion()` with a multi-select answer (`q2`) and an "Other" free-text answer (`q1`,
  client-side affordance, available regardless of `multiSelect`) in the same round trip.

## Fixture regeneration -- NOT done, and why

The task's "should" item asked to regenerate `sdk/typescript/tests/fixtures/agui/*.sse` (hand-authored
per PANDO-US-0010's provenance note) via `scripts/record-agui-fixtures.mjs` now that a deterministic
agent exists. Not done, reported rather than forced: the three committed fixtures
(`interrupt-frontend-tool.sse`, `resume-frontend-tool.sse`, `state-delta-todos-tokenusage-files.sse`)
exercise a **frontend-declared tool call** (`get_weather`) and **todo/state-delta** behaviour --
neither is part of HITL, and this task's fixture agent only implements the two prompt kinds T-0002
scoped it to (permission + `AskUserQuestion`). Regenerating those three specific files for real would
need either (a) extending the fixture agent's scope to simulate frontend-tool-call and todo-writing
turns deterministically (a separate, larger piece of work), or (b) a real LLM call -- this sandbox
does have working credentials (Claude OAuth, GitHub Copilot, local Ollama models) in the developer's
own `~/.config/pando`, but using them was deliberately avoided: it is the developer's real account,
and the whole point of this task's chosen approach (over the rejected recorded-cassette/credentials
route) is that the round trip must not need credentials at all. Flagged as a follow-up, not attempted
here.

## Verification

Go (repo root):
- `go build ./...` -- clean.
- `go build -tags agui_fixture_agent ./...` -- clean.
- `go vet ./...` and `go vet -tags agui_fixture_agent ./...` -- clean.
- `go test ./...` -- clean, 101 packages ok / no test files, 0 FAIL (repo-wide baseline preserved).
- `go test ./internal/llm/agent ./internal/api` -- ok (CLAUDE.md's verified command).
- `go test -race ./internal/agui ./internal/config ./internal/llm/agent ./internal/api` -- agui,
  config, api clean; `internal/llm/agent` has a **pre-existing, unrelated** data race between
  `TestRunResetsResurrectionCount` (`resume_test.go`) and `TestSessionModelIDFollowsOverride`
  (`session_model_surface_test.go`) -- `agent.Run()`'s background goroutine in the former is not
  joined before the test returns, and races the latter's `config.SetForTests()`. Reproduces in
  isolation with just those two tests, on files this change never touched
  (`agent.go`'s `effectiveContextWindow`/`ensureHistoryFitsBeforeSend`, `config.SetForTests`); the only
  `agent.go` edit here is the additive 7-line `createAgentProvider` hook, which touches no shared
  state. Reported, not fixed (out of scope for this task).
- `gofmt -l` on every touched file -- clean.
- Manual smoke tests: hand-built fixture binaries + curl round trips for both permission (approve) and
  question (multi-select + Other) scenarios, confirming real `internal/agui/hitl.go` encoders, a real
  suspend/resume across two HTTP requests, and a real file write gated on the permission answer.

sdk/typescript:
- `npm run build`, `npm run typecheck`, `npm test` (141/141, was 136 + this suite's 5, previously
  skipped), `npm run test:browser-build`, `npm run check:agui-drift` -- all clean.
- No stray processes, ports or temp files left behind (`afterAll` kills the server -- waits for exit,
  then SIGKILL as a fallback -- and removes the project/home/build temp dirs; verified with `ps aux`
  and directory listings after each run).

## Files touched

Pando (Go): `internal/config/fixture_agent.go` (new), `internal/llm/agent/fixture_hitl_agent.go`
(new), `internal/llm/agent/fixture_hitl_agent_stub.go` (new), `internal/llm/agent/agent.go` (+7),
`internal/agui/run.go` (eventBuffer/activeRun segment-start fix), `internal/agui/server.go`
(`beginResumeSegment` calls `markSegmentStart`).

sdk/typescript (separate repo): `tests/agui-integration.test.ts` (rewritten).

## Acceptance criteria status

- [x] A permission request raised by a real `agui-serve` is approved through `approve` and the run
      resumes -- `approve()` test, green.
- [x] An `AskUserQuestion` raised by a real `agui-serve` is answered through `answerQuestion`,
      including multi-select and "Other" free-text, and the run resumes -- new test, green.
- [x] The test runs automated: part of `npm test`, hermetic, no LLM credentials, no manual steps.
- [ ] Fixture regeneration for the three pre-existing (non-HITL) `.sse` files -- not attempted, see
      above; a follow-up, not a blocker for T-0002's own criteria.

No commit was made in either repository (coordinator commits).