---
created_at: 2026-09-14T16:36:03.477801209Z
updated_at: 2026-09-14T16:36:03.477801209Z
tags:
    - feature
    - agui
---

# PANDO-US-0015 + PANDO-US-0016: AG-UI thread API and MESSAGES_SNAPSHOT resync

Part of [[PANDO-EP-0003]] "AG-UI thread lifecycle and run durability". Builds on
PANDO-US-0011 (tool allow-list, `filterAGUITools`/`aguiToolAllowed`) and
PANDO-US-0012/13/14 (profiles, `resolveAgent`/`resolveProfile`, per-session
overrides). Both stories implemented and merged in the same pass; tests green.

## PANDO-US-0015 — Thread API: list, read messages, delete

Files touched:
- `internal/agui/threads.go` — `threadStore.list(ctx, limit, offset)` (new):
  queries `agui_threads` directly (`ORDER BY updated_at DESC, thread_id DESC`),
  degraded/no-DB mode returns `(nil, nil)`. New `threadRecord` struct. New HTTP
  handlers `handleListThreads`, `handleThreadMessages`, `handleDeleteThread`,
  plus `ThreadSummary` (wire shape) and `threadPaginationParams`
  (`?limit=&offset=`, default 50, cap 200).
- `internal/agui/server.go` — `Register()` now mounts `GET {path}/threads`,
  `GET {path}/threads/{id}/messages`, `DELETE {path}/threads/{id}`, all through
  `authorize()`. New `runPrelude` helper (also used by US-0016, see below).
- `internal/agui/state.go` — `stateStore.delete(threadID)` (new): drops a
  thread's shared-state document, called by `handleDeleteThread` so a thread id
  reused after deletion does not inherit stale todos/files/sub-agents.
- `internal/agui/transcript.go` (new file) — `toAGUIMessages([]message.Message)
  []Message`: the single AG-UI `Message[]` converter, shared by the read route
  and the MESSAGES_SNAPSHOT resync. A Pando "tool" message carrying multiple
  `ToolResult` parts is expanded into one AG-UI message per result (matching
  `toolCallId`). `aguiRole`/`toAGUIToolCalls` are the role/tool-call mapping
  helpers.

Behavioural notes:
- `handleDeleteThread` order: cancel any active run for the thread
  (`r.runs`/`finishRun`), drop its state document (`r.states.delete`), delete
  messages (`Messages.DeleteSessionMessages`), delete the session
  (`Sessions.Delete`), forget the `agui_threads` binding (`threads.forget`).
  Idempotent: an unknown/already-deleted thread id returns 204, not an error.
- Ownership scoping is exactly "rows in `agui_threads`" — no fallback to
  scanning `Sessions.List()` by the `agui: ` title prefix, per the story's
  explicit "do NOT" constraint.
- The pre-existing dangling-binding recovery in `sessionForThread`
  (`runtime.go`) is unchanged and still the safety net for a session deleted
  through some other path.

Tests: `internal/agui/threads_test.go` (`TestThreadStoreListNewestFirstAndPaginated`,
`TestThreadRoutesRequireAuthorizeOnDedicatedListener` via `r.Handler()`,
`TestHandleListThreadsNewestFirstPaginated`,
`TestHandleThreadMessagesConvertsToAGUIShape`,
`TestHandleThreadMessagesUnknownThread404s`,
`TestHandleDeleteThreadRemovesEverythingAndIsIdempotent`,
`TestHandleDeleteThreadUnknownThreadIsIdempotent`,
`TestDeleteThreadThenNextRunStartsFreshSession`), plus fakes
`fakeSessionService`/`fakeMessageService` (in-memory, satisfy `session.Service`/
`message.Service`) and `newThreadTestRuntime` builder added to the same file.
`internal/agui/transcript_test.go` (new) covers `toAGUIMessages` directly.

## PANDO-US-0016 — MESSAGES_SNAPSHOT on first run of a pre-existing thread

Files touched:
- `internal/agui/runtime.go` — `sessionForThread` signature changed to
  `(sessionID string, existed bool, err error)`: `existed=true` for a known
  thread binding or a client reusing an existing Pando session id as its
  thread id; `existed=false` only for a session created by this call.
- `internal/agui/state.go` — `stateStore.get` signature changed to
  `(tracker *stateTracker, created bool)`: `created=true` when a brand-new
  document was built (first contact, or a rebind after a session change).
- `internal/agui/server.go` — `handleRun` computes
  `resync := existed && attached` and calls the new `runPrelude(ctx, sse, t,
  state, sessionID, resync)`, which replaces the old inline
  `sse.WriteAll(t.Start())` / `sse.Write(state.Snapshot())` pair and adds,
  when `resync`, a `MESSAGES_SNAPSHOT` built from `Deps.Messages.List` via
  `buildMessagesSnapshot` — built before the agent run starts, so a large
  history cannot block the event loop. A `Messages.List` error is swallowed
  (logged at Debug) rather than failing the run.
- `internal/agui/transcript.go` — `buildMessagesSnapshot(msgs, maxMessages,
  maxBytes) MessagesSnapshotEvent` and `capMessages([]Message, maxMessages,
  maxBytes) (kept []Message, truncated bool)`: keeps the most recent messages
  within both caps (either `<= 0` means uncapped on that dimension), always
  keeps at least the newest message even if it alone exceeds the byte cap.
- `internal/agui/events.go` — `MessagesSnapshotEvent` gained a `Truncated
  bool json:"truncated,omitempty"` field; `NewMessagesSnapshot(msgs,
  truncated)` signature changed accordingly (it had zero prior call sites, so
  this was safe).
- `internal/agui/deps.go` — `Config.MessagesSnapshotMaxMessages` /
  `MessagesSnapshotMaxBytes` (new fields, not yet exposed via
  `config.AGUIConfig`/TOML — internal knobs only, resolved to
  `defaultMessagesSnapshotMaxMessages=200` /
  `defaultMessagesSnapshotMaxBytes=256<<10` in `ConfigFromApp`; a hand-built
  test `Config{}` defaults both to 0, meaning uncapped, which is intentional).

Key design decision: "first run of that attach" is detected via
`stateStore.get`'s new `created` return value rather than any new
per-thread bookkeeping — a fresh `stateTracker` for a thread whose session
already existed is exactly "this process has not served this thread since
either process start or its last rebind/eviction", which is what the story's
"once per stream attach" wants (process-memory-scoped, matching how
`stateStore`/`threadStore`'s in-memory halves already behave).

Tests: `internal/agui/server_test.go`
(`TestRunPreludeOrderingOnPreExistingThreadResync` — asserts
RUN_STARTED→STATE_SNAPSHOT→MESSAGES_SNAPSHOT via the existing
`frameTypes`/`indexOf` helpers from `interrupt_test.go`,
`TestRunPreludeBrandNewThreadEmitsNoMessagesSnapshot`,
`TestRunPreludeSkipsResyncWhenNotRequestedEvenWithHistory`,
`TestRunPreludeTruncatesLargeHistoryWithinLatencyBudget`,
`TestRunPreludeToolCallAndResultSurviveIntoTheSnapshot`) exercise the new
`runPrelude` function directly (no real agent pool needed — same pattern
`interrupt_test.go` already uses for `r.stream`). `internal/agui/transcript_test.go`
covers `capMessages`/`buildMessagesSnapshot` cap/truncation edge cases directly.
`internal/agui/state_test.go` updated for the `get` signature change plus new
`TestStateStoreDelete`.

## Verification

`go build ./...` clean. `go test ./internal/agui/... ./internal/config/...`:
all pass (139+ tests in `internal/agui`, including 25 new/changed). Also ran
`go test ./internal/agui/... -race`: clean. `go vet ./internal/agui/...` and
`gofmt -l internal/agui/`: clean. Scope stayed inside `internal/agui/` (no
edits to `internal/api/`, `internal/rag/`, `internal/llm/tools/`, `sdk/`, or
`internal/config/config.go` — neither story needed the config surface).

No acceptance criteria were left unsatisfied. One judgment call worth noting
for future stories in this epic: `MessagesSnapshotMaxMessages`/`MaxBytes` are
Go-level `Config` fields with hardcoded defaults, not yet wired to
`config.AGUIConfig`/TOML — a future story should add that surface if
per-deployment tuning is wanted.
