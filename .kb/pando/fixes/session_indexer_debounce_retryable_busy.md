---
created_at: 2026-09-11T17:11:55.569757847Z
updated_at: 2026-09-11T17:11:55.569757847Z
tags:
    - fix
    - sqlite
    - ios-hang
---
# Fix: session-indexer load reduction (#4) + retryable busy/locked errors (#5) (2026-09-11)

Implements steps #4 and #5 of [[pando/plans/sqlite_contention_fix_roadmap.md]], ranked items #4 and #5 in the table of [[pando/analysis/session_index_locked_residual_risk.md]] section 5. Builds on step 1, [[pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md]] (already done: IMMEDIATE write transactions, per-connection busy_timeout, `idx_events_session`). Does **not** touch the context-enricher fix ([[pando/fixes/context_enricher_agent_loop_first_event.md]]) already present in the working copy — `isEphemeralIndexSession`/`ctxEnrichSessionIDPrefix` are reused as-is.

## Problem recap

`internal/app/remembrances_indexer.go` subscribed to every `app.Messages` Created/Updated event with a 1.2s per-session debounce. The agent persists every streamed ThinkingDelta/ContentDelta/ToolCall with `messages.Update` (no throttle), so the debounce reset on virtually every event and fired on any pause > 1.2s. Each run rebuilt and re-embedded the *whole* session transcript and called `EventStore.ReplaceSessionEvents` (replace-all) — up to ~12MB of JSON over IPC for large sessions on secondaries. Lock failures mapped to `ErrCodeInternal` (not retryable) in `internal/ipc/dbproxy/errors.go`, so every transient BUSY/LOCKED failure was permanent and logged at Error (152 failures in 8 minutes in the incident telemetry).

## #4 — Index only when it matters

### Event filter — `internal/app/remembrances_indexer.go`, `shouldIndexOnEvent`

```go
func shouldIndexOnEvent(ev pubsub.Event[message.Message]) bool {
	switch ev.Type {
	case pubsub.CreatedEvent:
		return true
	case pubsub.UpdatedEvent:
		return ev.Payload.IsFinished()
	default:
		return false
	}
}
```

Verified against `internal/llm/agent/agent.go` exactly which events each part of a turn produces, so nothing that must still be indexed is dropped:

- **User messages**: `createUserMessage` → `messages.Create` → `CreatedEvent`. Always qualifies.
- **Assistant messages**: `streamAndHandleEvents` creates an *empty* assistant message (`messages.Create`, `CreatedEvent`, qualifies) before streaming, then calls `messages.Update` on every `ThinkingDelta`/`ContentDelta`/`ToolUseStart`/`ToolUseStop` provider event with no throttle — these are `UpdatedEvent`s with no `Finish` part, so they're skipped. Every leg of a turn's assistant message is guaranteed to end with exactly one `Update` that *does* carry a `Finish` part (`AddFinish` on the `EventComplete`, cancellation, and panic-recovery paths, `agent.go` `finishMessage`/`processEvent`/`streamAndHandleEvents`), so the leg's final state is always still indexed.
- **Tool results**: created as a *single* one-shot `Tool`-role message with every result already attached (`streamAndHandleEvents`'s `a.messages.Create(...Role: message.Tool...)`, after the tool loop) — a `CreatedEvent` with no follow-up `Update` at all. Always qualifies, so tool results are indexed via the Created branch, not the Updated one.
- **Session title changes**: title generation (`agent.generateTitle`) runs in a background goroutine and calls `a.sessions.Save`, which is a **session**-service write, not a `messages` write — it was never wired into this **message**-subscribed indexer's trigger set, before or after this change (title changes were never a distinct trigger). What keeps titles current in the index is that `indexSessionConversation` re-fetches `sess.Title` fresh via `app.Sessions.Get` at the moment a run actually executes; since title generation is a small, fast LLM call kicked off at the very start of a turn and the assistant's own message-finish trigger only fires (via the new 5s debounce) well after that, the title is normally already saved by the time the run reads it. This is an existing property of the design (reading the session fresh at run time), not something added by this filter, and it is unaffected by the filter change.

### Debounce + rate cap + trailing run + concurrency guard — new `internal/app/session_index_scheduler.go`

Replaced the old `map[string]*time.Timer` (each firing its own untracked goroutine, unbounded overlap) with `sessionIndexScheduler`:

- `sessionIndexDebounce = 5 * time.Second` (was 1.2s): absorbs a whole turn's several finished legs into one run.
- `sessionIndexMinInterval = 15 * time.Second`: no two runs for the same session start less than this apart, regardless of event volume.
- One mutex-guarded `sessionIndexRunState{timer, running, dirty, lastRunAt}` per session. `notify()` (re)arms the debounce timer unless a run is already `running`, in which case it just sets `dirty=true` and returns — no new timer, no overlap. `run()` sets `running=true` before calling `indexFn`, and on completion, if `dirty` was set while it ran, schedules exactly one trailing run (`scheduleLocked(..., baseDelay=0)`, still subject to the `minInterval` floor via `lastRunAt`) — so a burst that arrives mid-run or mid-rate-limit is never silently dropped, only delayed. All state transitions happen under one lock, so `notify()` and a timer-fired `run()` cannot race each other into overlapping runs or a lost trailing run.
- The watcher (`initRemembrancesSessionIndexing`) now passes `subCtx` (the watcher's own cancelable context, canceled on `App.Shutdown`) instead of `context.Background()` as the run context, so a run's retry backoff (see #5) is canceled promptly on shutdown instead of outliving it. `stopAll()` on `subCtx.Done()` stops pending timers; a run already executing is not waited on — this matches the pre-existing shutdown behavior of this watcher (it was never added to `app.watcherWG` beyond the single subscription-loop goroutine) and was a deliberate choice to avoid changing `App.Shutdown`'s blocking semantics, which are out of this task's scope.

## #5 — Retryable busy/locked errors

### `internal/ipc/dbproxy/errors.go`

- New `ErrCodeBusy WriteErrorCode = "BUSY"`; `WriteError.IsRetryable()` now returns `true` for it (alongside the existing `ErrCodeTimeout`/`ErrCodeUnreachable`).
- `mapToWriteError` gained a case `isLockError(err) → ErrCodeBusy`, placed before the default/`ErrCodeInternal` fallback.
- New exported `dbproxy.IsBusyOrLockedError(err error) bool`: `errors.As` into `*WriteError` and check `Code == ErrCodeBusy`, else fall back to `isLockError(err)` directly. This covers **both** shapes a busy error can take by the time a caller sees it:
  1. **Direct** (the primary's own indexer calling `EventStore.ReplaceSessionEvents` with no proxy): the raw error from `s.db.BeginTx`/`tx.ExecContext`, never passed through `mapToWriteError` at all — `isLockError` handles this.
  2. **Round-tripped through IPC** (a secondary forwarding the write): the primary's `RemembrancesWriteDispatcher.DispatchRemembrancesWrite` (`internal/rag/proxy/dispatcher.go`) returns the raw store error *without* going through `mapToWriteError` (only the sqlc `db.Querier` cases in `dispatchWrite` do that) — the JSON-RPC layer (`internal/ipc/bus.go`) flattens it to a message-only `rpcError{Code:-32000, Message: err.Error()}`, and the secondary's `ipc.Client.Call` re-wraps that as a plain `"ipc: RPC error -32000: ...: database is locked"` string with no typed `*sqlite3.Error` surviving the round trip. `proxyVoidWrite`/`proxyWrite` then call `mapToWriteError` on *that* string, which reaches `ErrCodeBusy` via `isLockError`'s string-fallback branch.

### `internal/ipc/dbproxy/proxy.go` — `isLockError` hardened

While writing a test that reproduces a **real** `SQLITE_BUSY` (two real `BeginTx` calls colliding on a temp-file DB with `_txlock=immediate` + a short `busy_timeout`), found that the ncruces driver does **not** always wrap a lock collision as `*sqlite3.Error`: a bare `BeginTx` collision surfaces as a plain `sqlite3.ExtendedErrorCode`/`sqlite3.ErrorCode` value instead, which `errors.As(err, &sqlite3Error)` (targeting `*sqlite3.Error`) does **not** match — the old typed branch would have silently fallen through to the string fallback for this exact case (which still worked, by luck of the message text). Added a first check `errors.Is(err, sqlite3.BUSY) || errors.Is(err, sqlite3.LOCKED)` ahead of the `*sqlite3.Error` type-assertion, which works for both error shapes (`*sqlite3.Error.Is` and `ExtendedErrorCode.Is` both compare against an `ErrorCode`/`ExtendedErrorCode` target). Purely additive — a superset of what `isLockError` matched before — so no existing caller (`directOrProxy`/`directOrProxyVoid`'s fallback-to-proxy decision) can regress; it can only now also catch cases the typed branch used to miss.

### Indexer-level retry — `internal/app/remembrances_indexer.go`, `replaceSessionEventsWithRetry`

```go
const sessionIndexReplaceRetries = 3                       // + the initial attempt = 4 max
const sessionIndexReplaceBaseBackoff = 250 * time.Millisecond // doubles: 250ms/500ms/1s

func replaceSessionEventsWithRetry(ctx context.Context, sessionID string, replace func() error) error {
	backoff := sessionIndexReplaceBaseBackoff
	var err error
	for attempt := 0; attempt <= sessionIndexReplaceRetries; attempt++ {
		err = replace()
		if err == nil { return nil }
		if attempt == sessionIndexReplaceRetries || !dbproxy.IsBusyOrLockedError(err) { return err }
		logging.Warn("remembrances session index: transient lock, retrying", ...)
		select {
		case <-ctx.Done(): return err
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return err
}
```

`indexSessionConversation` calls it wrapping the existing `svc.Events.ReplaceSessionEvents(...)` call in a closure that captures the already-computed `chunks`/`chunkEmbeddings` — the retry loop itself never touches the embedder, so a retry never re-embeds. This is layered **on top of** `dbproxy.WriteWithRetry`'s own retry (3 attempts, 50/100/200ms backoff, now also retrying `ErrCodeBusy` since it's retryable) for the secondary/IPC path: on a secondary, a sustained lock could see up to 4 (indexer) × 3 (dbproxy) IPC round trips in the worst case, judged acceptable since this is a background, rate-capped (`sessionIndexMinInterval`), non-prompt-path job. Only the final failure is logged at Error (by `sessionIndexScheduler.run`, unchanged from before); each retry logs at Warn with the attempt number and backoff.

## Files / symbols touched

- `internal/ipc/dbproxy/errors.go` — `ErrCodeBusy`, `IsRetryable` update, `mapToWriteError` busy case, new exported `IsBusyOrLockedError`.
- `internal/ipc/dbproxy/proxy.go` — `isLockError` hardened with `errors.Is(err, sqlite3.BUSY/LOCKED)`.
- `internal/ipc/dbproxy/handlers_test.go` — `TestWriteError_IsRetryable` extended; new `TestMapToWriteError_BusyAndLockedErrorsAreErrCodeBusy` (real `BeginTx` collision + 3 string-shaped cases) and `TestIsBusyOrLockedError_NonBusyErrorsAreFalse`.
- `internal/app/remembrances_indexer.go` — new `shouldIndexOnEvent`; `initRemembrancesSessionIndexing` rewritten around `sessionIndexScheduler` and `subCtx`; new `replaceSessionEventsWithRetry` + constants; `indexSessionConversation`'s `ReplaceSessionEvents` call wrapped by it.
- `internal/app/session_index_scheduler.go` (new) — `sessionIndexScheduler`/`sessionIndexRunState`, `sessionIndexDebounce`/`sessionIndexMinInterval` constants.
- `internal/app/session_index_scheduler_test.go` (new) — debounce-collapses-burst, min-interval caps rate, trailing run guaranteed, no overlap under concurrent `notify`, independent sessions run independently, `stopAll` stops pending timers. All race-clean, no flaky fixed sleeps beyond short bounded `waitFor` polling.
- `internal/app/session_index_retry_test.go` (new) — `replaceSessionEventsWithRetry` unit tests (succeeds after transient busy, gives up after max attempts, non-retryable returns immediately, respects ctx cancellation) plus one end-to-end integration test (`TestIndexSessionConversationRetriesRealBusyErrorAndReusesEmbeddings`) against a real temp-file SQLite `events.EventStore` with a genuine `BeginTx` collision, asserting the embedder is called exactly once despite the retried write.
- `internal/app/remembrances_indexer_test.go` — new `TestShouldIndexOnEvent` table test; `recordingEmbedder` gained a `callCount` field (used by the new integration test; existing tests unaffected).

## Verification

- `go build ./...` — clean.
- `gofmt -l` on every touched file — clean.
- `go vet ./internal/app/... ./internal/ipc/... ./internal/rag/... ./internal/db/... ./cmd/...` — clean.
- `go test ./internal/app ./internal/ipc/... ./internal/rag/... ./internal/db/... ./internal/llm/agent ./cmd` — all pass except the 4 known pre-existing failures in `./internal/llm/agent` (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged` — read the real `.pando.toml`/global config, unrelated to this change, confirmed pre-existing).
- `go test ./internal/app/... -race` — full package, all pass, no data races (the new scheduler and retry code included). Did not additionally re-run `-race` on `./internal/rag/kb/...` for this task; the known pre-existing race in `KBStore.SyncDirectoryWithStats` (`internal/rag/kb/sync.go`/`links.go`, already documented in [[pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md]]) is untouched by this change.
- Did not write to the real `.pando/data/pando.db`; all new tests use `t.TempDir()`-backed SQLite files or in-memory fakes.

## Remaining work / not done here

- **#6 Incremental per-message indexing** — explicitly out of scope for this task (next step); see design proposal below, recorded separately.
- **IPC P0 failover fixes and beyond** ([[pando/plans/mcp_server_ipc_bootstrap.md]]) — unrelated, untouched.
- The nested retry multiplication (indexer × dbproxy) noted above is judged acceptable but not load-tested under production-scale concurrency; only the synthetic/real-temp-DB tests here.
- Title-change indexing still relies on the pre-existing "read session fresh at run time" property rather than an explicit session-pubsub trigger; this was already true before this change and is unaffected, but a session that finishes titling *after* the debounced run already executed (e.g. a very slow title provider) would only pick up the new title on the *next* qualifying message event, same as before this fix.

## Design proposal for #6 (incremental per-message indexing)

Not implemented, but sketched here from what this task's investigation of `indexSessionConversation`/`EventStore` surfaced, for whoever picks it up next:

- **Chunk layout**: today one session = N content-sized chunks (`embeddings.ChunkText` over the *entire* rendered transcript), replaced wholesale. Incrementally, chunk per **message** instead (optionally splitting a very large single message with the same `ChunkText` if it exceeds the chunk size) — each `events` row's `metadata` would carry `message_id` (new) alongside the existing `session_id`, `chunk_index`, `chunk_count`, plus a per-message `updated_at`/`message_version` (e.g. reuse the message's own `updated_at`) to detect staleness.
- **Metadata keys**: add `message_id`, `role`; keep `session_id`, `title`, `source`, `user_id` at the session level by duplicating them onto every message-chunk row (cheap, and keeps `SearchEvents`'s existing subject/session filters working unchanged) or by introducing a small `subject="session_meta"` row per session for title/attribution alone, searched separately from content. Simpler to duplicate given the current schema has no per-session-only row concept.
- **Update algorithm**: on a qualifying event, look up the message's existing chunk rows by `(session_id, message_id)` (needs a new expression index mirroring `idx_events_session`, e.g. `events(subject, json_extract(metadata,'$.message_id'))`); if the message's `updated_at`/hash matches what's stored, skip re-embedding and re-writing entirely (this is the O(1)-per-message win over today's O(n) full-session replace); otherwise re-chunk/re-embed only that message and replace only its rows (a new `EventStore.ReplaceMessageEvents(ctx, sessionID, messageID, subject, metadata, chunks, embeddings)`, structurally identical to `ReplaceSessionEvents` but scoped by `message_id` instead of `session_id` in `deleteSessionEventsTx`'s WHERE clause). Session-level metadata (title) changes would need their own light single-row update path, or continue to be refreshed opportunistically whenever any message in the session changes.
- **Search result shape change**: `SearchEvents` results are currently one row per transcript chunk with `chunk_index`/`chunk_count` metadata; per-message chunking makes each result map to exactly one message (better for showing "jump to this message" context, worse if a caller relied on chunk boundaries spanning multiple messages for continuity — check `internal/llm/tools`' consumers of session search results before changing the shape they receive).
- **Migration/backfill**: existing session rows have no `message_id` in their metadata. Either (a) leave old rows as-is (they still match on `session_id` for old-style whole-session search) and let each session naturally migrate to per-message rows the next time any of its messages changes, since a first per-message run would need to delete the old whole-session rows for that session before inserting message-scoped ones (a one-time `ReplaceSessionEvents(ctx, id, subject, nil, nil, nil)` per session to clear, followed by per-message inserts) — no backfill script needed, just a "first incremental run for this session clears the legacy rows" branch; or (b) a one-off backfill job (`pando db` subcommand or startup migration, following the pattern already used for the `ctxenrich-*` cleanup in [[pando/fixes/context_enricher_agent_loop_first_event.md]] §6.3) that re-indexes every session incrementally once, replacing every old whole-session row set in one pass. (a) is simpler and lower-risk; (b) gives a clean cutover date. Either way this is the natural point to also add the `idx_events_session_message` expression index in the same migration.