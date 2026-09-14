---
created_at: 2026-09-14T17:28:48.62968198Z
updated_at: 2026-09-14T17:28:48.62968198Z
tags:
    - feature
    - agui
    - concurrency
---
# PANDO-US-0017 + PANDO-US-0018 + PANDO-US-0019: run parking, reattach and cancel

Part of [[PANDO-EP-0003]] "AG-UI thread lifecycle and run durability". Closes the epic:
builds on PANDO-US-0011/12/13/14 (agent pool, profiles) and PANDO-US-0015/16
(see [[pando/features/agui-thread-api-and-messages-snapshot.md]]). All three stories
implemented and merged in one pass; `go build ./...` and
`go test -race ./internal/agui/... ./internal/config/...` green.

## Why: the old design

Before this change, `internal/agui/server.go`'s `stream()` was a single function tied
1:1 to one HTTP request's context: it directly consumed `run.events`/`run.suspend` and
wrote straight to that request's `SSEWriter`. A browser disconnect (`ctx.Done()`) called
`finishRun`, which cancelled the run's context — killing an in-flight turn just because
the tab was reloaded. There was no way to reattach to a live run, and no way to cancel
one except aborting the HTTP request (useless once a permission prompt had already
suspended it with no request attached).

## The new architecture: one pump goroutine per run

`internal/agui/run.go` now gives every `activeRun` a dedicated **pump** goroutine
(`Runtime.pump`), started once in `handleRun` when the run is created and alive for the
run's whole lifetime — across parks, disconnects, reattaches and interrupt/resume
segments. It is the **only** reader of `run.events`, `run.suspend` and the new
`run.cancelSignal`, and the only caller of the run's `translator`'s `Translate/Finish/Fail`
— this single-writer discipline is what keeps `translate.go`'s stateful translator safe
under concurrent attaches without adding any locking to it.

Everything else — HTTP handlers, possibly several at once for one thread — is a
**subscriber** (`activeRun.subscribe/unsubscribe`, a `chan Event`): it registers, replays
what it missed from a bounded ring buffer (`eventBuffer`, 500 events, drops oldest and
marks itself `lossy` when full), forwards live events for as long as it stays attached,
and detaches on its own request context or at a segment boundary (`RUN_FINISHED`/
`RUN_ERROR` — whether a true end or an interrupt the run will continue past in a future
segment). `Runtime.attachRun`/`attachLoop` (server.go) is the one function every attach
goes through: the original stream, a `GET {path}/threads/{id}/stream` reattach, a
`POST` carrying no new user message, and read-only followers (extra tabs) are all
exactly this call, parameterised only by `first bool` (skip the replay preamble for the
very-first attach, since `runPrelude` already wrote it directly and nothing has been
buffered yet).

## PANDO-US-0017 — park instead of cancel on disconnect

- `internal/agui/run.go`: `activeRun` gained `cancelSignal`, `buffer *eventBuffer`,
  `done chan struct{}`, `translator`/`suspended`/`finished`/`subs` (all `mu`-guarded).
  `park(d time.Duration, onExpiry func())` now takes an explicit duration (was hardcoded
  to `suspendGrace`) so a NEW call site can use a different one.
- `internal/agui/server.go` `attachLoop`: on the last subscriber detaching from a still-
  live run, it arms `park(grace, ...)` where `grace` is `Config.DisconnectGrace` for a
  live segment or the (unchanged, longer) `suspendGrace` if `run.isSuspended()` — the two
  lifetimes are deliberately never conflated. Expiry calls `run.requestCancel("expired")`,
  which is pump-mediated (never touches the translator from the timer's own goroutine).
- New config key `AGUIConfig.DisconnectGrace` (`internal/config/config.go`, duration
  string, default `"2m"` resolved in `agui.ConfigFromApp` / `defaultDisconnectGrace`,
  `internal/agui/deps.go`).
- `eventBuffer` (run.go): bounded ring of already-translated `Event`s (not raw agent
  events) — replay is "resend what was already computed", no retranslation, no
  double-emit risk.

## PANDO-US-0018 — reattach with buffered replay, one run per thread

- `Runtime.attachRun`/`attachLoop` (server.go): the unified attach path described above.
  Replay stops at the first segment boundary it encounters, exactly like the live path,
  so "one HTTP response = one AG-UI run segment" holds whether the boundary arrived live
  or via catch-up replay.
- `Runtime.beginResumeSegment` (server.go) replaces the old `resumeRun`: it installs a
  new translator (`inheritEnded`/`suppressToolCall` as before) and broadcasts its
  `RUN_STARTED` **before** delivering the resolved tool results to `pending.resolve`.
  This ordering is load-bearing: `pending.resolve` wakes the blocked tool goroutine
  immediately, and without installing the new translator first, the (single) pump could
  observe the resumed agent's next event while still holding the old, already-closed-out
  translator. `Runtime.resumeCandidates` (server.go) splits detection (peek
  `pendingRegistry.isPending`, new) from delivery for exactly this reason — replaces the
  old `deliverToolResults`, which resolved immediately as it detected.
- One run per thread / TOCTOU fix: `runStore.lockThread` (run.go, new) serializes
  `handleRun`'s decide-and-register section per thread ID (not globally — other threads
  are never blocked). `runStore.put` is now conditional (fails if the thread already has
  a run) instead of last-write-wins; `remove` stays identity-checked. A new POST with a
  user message on a thread that already has a live run is rejected outright
  (`409 a run is already in progress for this thread`) — it is **never** abandoned
  (`abandonRun` and the old unconditional-abandon branch are deleted).
- `handleRun` (server.go) restructured: a request with no live run and a trailing user
  message starts one (as before); a request with no live run and no new user message is
  a reattach with nothing to attach to (`404`, was `400` — see
  `TestHandleRunRejectsInputWithoutUserMessage`); a request against a thread that already
  has a live run resolves as a resumption, the loser case, or a reattach
  (`handleExistingThreadRun`).
- `GET {path}/threads/{id}/stream` (new route, `handleStream`): 404 for no live run,
  otherwise `streamAttach` → `attachRun(..., first=false)`.

## PANDO-US-0019 — cancel endpoint

- `POST {path}/runs/{id}/cancel` (new route, `handleCancelRun`): idempotent (unknown or
  already-finished thread → `204`), works for a live or suspended run, works from any
  request (not only the one streaming it).
- `pendingRegistry.cancelAll(sessionID)` (frontend_tool.go, new): force-delivers a
  cancellation `Message{Error:...}` to every call still waiting on the client for a
  session. This closes a real gap: `hitl.go`'s `awaitClient` (permission prompts) selects
  on the **adapter's base context**, not the run's — so `run.stop()`'s context
  cancellation alone never reaches it. `cancelAll` is the uniform fix, since every
  blocking wait (frontend tool, HITL question, permission) fundamentally blocks on a
  `pendingRegistry` channel.
- `activeRun.requestCancel(code)` (run.go, new): stops the run's context immediately,
  then wakes the pump via `cancelSignal` (buffered 1) to do the translator-touching part
  of teardown (`t.Fail("...", code)`, broadcast, remove from `runStore`) — kept on the
  pump goroutine for the same single-writer reason as everything else.
- `handleCancelRun` waits (bounded, `cancelTeardownTimeout = 5s`) on `run.done` before
  responding, so it can truthfully answer "gone from `runStore`". **Ordering bug found
  and fixed while writing the tests for this**: `run.done` must close only *after*
  `finishRun` (which does `runs.remove`) has run, not merely after `broadcastFinal` has
  handed the final frame to subscribers (an attach can return from its HTTP handler the
  instant it reads that frame, which is earlier). `Runtime.finalizeRun` now closes `done`
  last, after `finishRun`.
- Cancel never touches `agui_threads` or the session — `handleDeleteThread`'s existing
  direct `finishRun` call is unchanged and untouched by this work.

## Files touched

- `internal/agui/run.go` — rewritten: `eventBuffer`, `subscriber`, `activeRun` (new
  fields/methods), `runStore.lockThread`/conditional `put`, `Runtime.pump`,
  `Runtime.finalizeRun`, `Runtime.handleSuspend` (was `suspendRun`, now pump-internal and
  broadcast-based), `drainQueuedTranslated` (was `drainQueued`). `finishRun` unchanged.
- `internal/agui/server.go` — `handleRun` restructured around `runStore.lockThread`;
  new `handleExistingThreadRun`, `resumeCandidates`, `beginResumeSegment`, `streamAttach`,
  `handleStream`, `handleCancelRun`, `attachRun`, `attachLoop`, `isSegmentBoundary`.
  Removed: `stream`, `suspendRun`, `resumeRun`, `deliverToolResults`, `drainQueued`,
  `abandonRun` (dead after the abandon-on-new-POST behaviour was removed). `runPrelude`
  unchanged.
- `internal/agui/frontend_tool.go` — `pendingRegistry.isPending`, `pendingRegistry.cancelAll`.
- `internal/agui/deps.go` / `internal/config/config.go` — `Config.DisconnectGrace` /
  `AGUIConfig.DisconnectGrace` (only config key touched, per the story's scope).
- `internal/agui/doc.go` — new "Run lifetime and durability" doc section.
- Tests: `internal/agui/run_test.go` (new — eventBuffer, goroutine-leak assertion,
  buffered replay, second-tab follower, all 5 cancel ACs, grace-period distinction);
  `internal/agui/interrupt_test.go` (rewritten for the pump/attach model, `abandonRun`
  test removed); `internal/agui/server_test.go` (fake `agent.Service`/pool wiring for a
  real end-to-end TOCTOU race test, loser/reattach handler tests); `hitl_test.go`,
  `frontend_tool_test.go` updated for the new `park`/`runStore` signatures.

## Verification

- `go build ./...` clean.
- `go test -race ./internal/agui/... ./internal/config/...` green, including
  `TestConcurrentPostsProduceExactlyOneRun` (8 concurrent POSTs × 5 attempts, exactly one
  `svc.Run` call every time, no `RUN_ERROR{session_busy}`), `TestParkedRunExpiryLeavesNoGoroutine`
  (explicit `runtime.NumGoroutine()` leak check) and the 5 PANDO-US-0019 cancel ACs.
  Stress-verified with `-count=25` and eight separate fresh `-race` runs after fixing two
  test-only races found along the way (see "bugs found" below) — no flakes, no hangs.
- Known pre-existing unrelated failures in `internal/llm/agent` (caveman + tool-discovery
  tests) were left untouched, as instructed.

## Bugs found and fixed during this work (not pre-existing)

1. **`run.done` closed too early** (see PANDO-US-0019 section above): a genuine ordering
   bug in this new code, not a test artifact — fixed by moving `close(run.done)` to the
   end of `finalizeRun`, after `finishRun`.
2. **Replay didn't stop at a segment boundary**: a reattach that caught up via the
   buffered ring (rather than live) could replay a full `RUN_FINISHED{interrupt}` and then
   still enter the live wait loop, unlike a live attach which ends its response right
   there. Fixed in `attachRun` by checking `isSegmentBoundary` on each replayed event too.
3. **Parallel-suspend interrupt corruption**: since the pump no longer exits on suspend
   (by design — it must keep running to support reattach), a second parallel tool call
   suspending while the first is still awaiting its result would have produced a second
   `RUN_FINISHED{interrupt}` with no `RUN_STARTED` in between. Guarded in `Runtime.pump`:
   a suspension notification arriving while `run.isSuspended()` is already true is
   deferred (logged, not processed) — it stays queued in `pendingRegistry` and is picked
   up once the client's next request resolves it, same as the pre-existing round-trip
   behaviour.

## Related

- [[PANDO-EP-0003]]
- [[pando/features/agui-thread-api-and-messages-snapshot.md]] (PANDO-US-0015/16, the
  thread API and `runPrelude` this work extends rather than duplicates)
- [[pando/plans/backlog_execution_waves_2026-09-14.md]]
