---
created_at: 2026-09-11T12:52:53.254165943Z
updated_at: 2026-09-11T12:52:53.254165943Z
tags:
    - analysis
    - sqlite
    - ios-hang
---
# Session index "database is locked": residual risk after fixes 1 + 2 (2026-09-11)

Status: analysis + fix design only, nothing implemented. Point 3 of 3.
Builds on: [[pando/analysis/sqlite_locked_interrupted_errors.md]] (cause 2), [[pando/plans/mcp_server_ipc_bootstrap.md]] (fix 1), [[pando/fixes/context_enricher_agent_loop_first_event.md]] (fix 2).

Question: after fix 1 (mcp-server on the IPC bootstrap) and fix 2 (context enricher), can `remembrances session index failed … fts delete: database is locked` still happen?

**Answer: yes, unchanged.** Neither fix touches the mechanism. Telemetry shows that no mcp-server was running during the failure window, and the enricher only contributed a handful of index runs.

## 1. Telemetry, re-queried (Better Stack source 2751484)
- Window 11:19–11:26 UTC: **152 failures**.
  - **137 in `tui` mode** (the 2 mesnada `-p` subagents, which are secondaries). Text: `replace session events: dbproxy: INTERNAL (ReplaceSessionEvents): ipc: RPC error -32000: events: fts delete: sqlite3: database is locked`. The tx runs on the **primary** inside the writecoordinator and fails there.
  - **15 in `acp` mode.** Text: `replace session events: events: fts delete: …`, with no dbproxy prefix. That is the **primary's own direct indexer** for its own sessions.
  - That is roughly one failure every 1–3 s per process, meaning nearly every index run failed.
- The first `mcp` log line appears at 11:29, *after* the window. Fix 1 would not have prevented a single failure.
- The ACP process alone (11:16–11:18 and 11:27–11:34) produced 0 failures. With only one active session, the indexer rarely overlaps another commit.
- Correction to the earlier analysis:
  - Errors came from both paths, not only the ACP primary.
  - The read phase is not "multi-second": see §2.

## 2. Mechanism, re-verified
- **Writer:** `EventStore.ReplaceSessionEvents` (`internal/rag/events/events.go:145-190`).
  - Secondaries: `proxy != nil`, so the call goes through `WriteWithRetry(…, Long=30s)` (`events.go:156-163`). The primary's `DispatchWrite` → `RemembrancesWriteDispatcher` (`internal/rag/proxy/dispatcher.go:159-167`) then runs the same function directly.
- **Tx shape:** `s.db.BeginTx(ctx, nil)` (`events.go:166`), which issues `BEGIN ` with the default txlock, i.e. DEFERRED (`ncruces driver.go:354-357`). Steps:
  1. **READ:** `SELECT id,subject,content FROM events WHERE subject=? AND json_extract(metadata,'$.session_id')=?` (`events.go:229-256`).
  2. **First WRITE:** `INSERT INTO events_fts(events_fts,…) VALUES('delete',…)` per old row (`259-265`). The read→write lock upgrade happens here, hence the "fts delete" prefix. For a brand-new session it would be "insert event".
  3. `DELETE FROM events WHERE <same predicate>`: a second scan (`268-275`).
  4. N × (`INSERT events` + `INSERT events_fts`) (`177-184`, `192-226`).
  5. `COMMIT`.
- **Why busy_timeout does not help:** SQLite only calls the busy handler while `inTransaction==TRANS_NONE`. A read→write upgrade gets SQLITE_BUSY immediately if another connection holds the WAL write lock. It gets SQLITE_BUSY_SNAPSHOT immediately if any other connection committed after this tx's snapshot. The 60 s driver default (`driver.go:247-251`) is therefore irrelevant.
- **Schema** (`sqlite3 -readonly`):
  - `journal_mode=wal`, page_size 4096, 480,893 pages (~1.97 GB).
  - `events`: 46,064 rows (session 46,027; project 34), content 36 MB, embeddings 141 MB (768-dim float32 = 3,072 B/row), 284 distinct sessions, largest session 1,420 chunks (1.1 MB of text).
  - Indexes: only `idx_events_subject(subject)` and `idx_events_event_at`. `events_fts` is external-content FTS5 (porter unicode61). There are no triggers on events (the FTS is synced by hand).
  - `EXPLAIN`: `SEARCH events USING INDEX idx_events_subject (subject=?)`. This matches 99.9 % of rows, so it is effectively a full scan with json_extract. It takes ~40 ms in the native CLI on a warm cache. Expect roughly 3–10× that under WASM ncruces, where 7 of the 8 pool connections run with the default 2 MB cache (cache_size is only applied to 1 connection).
- **The vulnerable window is short (~0.05–0.4 s), but the commit rate is high.**
  - The agent persists **every** ThinkingDelta/ContentDelta/ToolUseStart/Stop with `messages.Update` (`internal/llm/agent/agent.go:1862,1871,1880,1925,1943`). There is no throttle, and each one commits and fires the `update_messages_updated_at` trigger.
  - Two streaming subagents give tens of commits per second, so P(commit inside window) = 1−e^(−rate·window), about 90–100 %. That matches the "every run fails" telemetry.
- **Indexer** (`internal/app/remembrances_indexer.go`):
  - It subscribes to the process-local `app.Messages` and handles every Created/Updated event. The 1.2 s per-session debounce (`35, 61-72`) resets on each streamed delta, so it fires on every pause longer than 1.2 s (tool calls, time-to-first-token).
  - `time.AfterFunc` gives each session its own goroutine, so several sessions can index concurrently.
  - Each run rebuilds the **whole transcript** (`81-133`), re-embeds **all** chunks with `EmbedDocuments` (`139`), then calls replace-all (`150`).
  - The embedding happens **outside** the tx and outside the coordinator job: secondaries ship precomputed embeddings. The cost is in the payload instead: about 1.1 M floats ≈ 12 MB of JSON over IPC for a 1,420-chunk session, per run.
- **Errors are not retried.**
  - The lock error maps to `ErrCodeInternal`, which is not retryable (`internal/ipc/dbproxy/errors.go:38-72`).
  - The indexer only logs at Error level, which is what floods the telemetry.
- **Other read-first DEFERRED write transactions with the same flaw:**
  - `KBStore.deleteDocument` (`kb/kb.go:443-470`)
  - `EventStore.DeleteEvent` (`events.go:620`)
  - KB link `relinkBatch` (`kb/backfill.go:157`)
  - Write-first transactions (code `IndexFile` `indexer.go:434`, KB insert `kb.go:228`) are not affected: their busy handler applies.

## 3. Writers that can commit concurrently with the primary's replace tx (after fixes 1 + 2)

### Primary process (8-connection pool, all direct)
- The agent loop for every session it hosts (ACP/Zed, in-process task sub-agent sessions):
  - `messages.Create/Update` per streamed delta
  - `UpdateSession` (usage/cost)
  - history `files` (`history/file.go:106` `Begin`)
  - session goals, ACP session state
- Its **own session indexer**: direct `ReplaceSessionEvents`, one goroutine per session, so these can race each other.
- The single writecoordinator goroutine (forwarded sqlc and remembrances jobs). Serial among themselves, but concurrent with everything above.
- Code watcher `IndexFile` plus `embedSymbols` autocommit UPDATEs; KB sync/auto-import; KB link backfill; memory upsert, `IncrementMemoryHits` on recall and memory GC (`kb/memory.go:245,356,373`).
- `mcpgateway.RecordUsage`, an INSERT **per MCP tool call** (`mcpgateway/stats.go:24`, `gateway.go:145`).
- Evaluator `InsertSessionScore`/`InsertSkill` (`evaluator/service.go:176,489`), cron jobs, AG-UI threads, design store.
- agentvcs and telemetry: no DB writes (grep).

### Secondaries (`ConnectRWSecondary`: 1 conn, busy 200 ms)
- All sqlc writes go **direct first** through `directOrProxy[Void]` (`dbproxy/proxy.go:233-277`). That covers `CreateMessage`/`UpdateMessage` per delta (`proxy.go:305-315`); they only fall back to IPC on BUSY.
- P5 direct writers: history `WithTx`, `project.NewService(rawQ)`, mcpgateway stats/favorites, design provider (`app.go:247,251,566,716`).
- Remembrances writes always proxy.

### Single process
**Yes, the error can still happen with only one process.** Any of these can commit between the SELECT and the first write while the indexer tx is open:
- two concurrently active sessions in one process
- an in-process task sub-agent streaming while the parent waits in a tool call longer than 1.2 s
- an indexer run overlapping the coordinator job, a code/KB watcher write, or a `RecordUsage` INSERT
- the same session resuming streaming after a tool call (the tx starts 1.2 s plus the embedding latency after the last event)

It is rarer than with streaming secondaries, and zero was observed in ACP-only periods, but it is not structurally prevented. **Fix 1 removes one class of independent writer.** After fix 1, mcp-server becomes a secondary with direct-first sqlc writes, so it still commits concurrently. **Fix 2 removes about 1–3 index runs per session start.** Neither closes the window.

## 4. Likelihood and impact
- **Likelihood:**
  - About 100 % of index runs fail while ≥2 sessions stream concurrently (subagents, swarms, desktop + ACP).
  - Low but nonzero with a single active session.
  - Scales with the delta commit rate and session size (via the read window).
- **Data impact:** lost or stale session index updates only. Replace-all is atomic, and a failed tx rolls back cleanly. It self-heals on the next successful run, but if the last run of a turn fails, the index stays stale until the next message. There is no corruption.
- **Cost impact:** each failed attempt has already paid a full-transcript re-embedding (local ollama/GPU contention with the chat model and enricher). A forwarded attempt also pays a ~MB-sized JSON encode/decode plus a coordinator slot. The Error-level logs flood telemetry (152 events in 8 minutes).
- **Stall impact:**
  - The *failure* is instant, so it never blocks the prompt path.
  - The *successful* runs are the stall risk. They hold the WAL write lock for a second scan, N FTS deletes, and N inserts of ~4 KB rows: hundreds of ms to seconds for 1k-chunk sessions. During that time:
    - primary prompt-path writes wait on the busy handler (60 s cap);
    - secondaries' direct writes fail after 200 ms and fall back to the coordinator, which is busy executing this very job (head-of-line), with a 5 s timeout and 3 retries.
  - This is a contributor to the ACP "hang", not the whole cause.

## 5. Remaining fixes, ranked

| # | Fix | Effort | Risk | Removes |
|---|---|---|---|---|
| 1 | BEGIN IMMEDIATE for write txs. Targeted: `&sql.TxOptions{Isolation: sql.LevelSerializable}` (ncruces maps it to `immediate`, `driver.go:350-351`) in ReplaceSessionEvents, deleteDocument, DeleteEvent, relinkBatch. Or global: `file:` URI DSN with `_txlock=immediate` (covers all 19 `BeginTx` sites, goose, history; `ReadOnly` txs stay deferred) | S | Low: txs now *wait* instead of failing, so pair with #2. Audit that no tx does network I/O while open (none found in the rag stores) | **All** read→write-upgrade BUSY / BUSY_SNAPSHOT failures (this error, and the same class in KB/backfill) |
| 2 | Per-connection init via `driver.Open(dsn, init)`: `BusyTimeout` (primary 10 s, secondary 200 ms), `foreign_keys=ON`, `synchronous=NORMAL`, `cache_size` on **every** pooled connection | S | Medium-low: FKs were OFF on 7/8 primary connections, so latent FK/cascade differences may surface; run the tests | Implicit 60 s waits on the prompt path, config drift on reopened connections, inconsistent FK cascades |
| 3 | Expression index (migration): `CREATE INDEX idx_events_session ON events(subject, json_extract(metadata,'$.session_id'))`; queries must keep the identical expression | S | Low | Shrinks the read window and the second DELETE scan from 46k rows to one session's rows (~ms). On its own it mostly (not fully) removes the error |
| 4 | Index only when it matters: skip `UpdatedEvent`s whose message has no `FinishPart`, raise the debounce to about 5 s, add a max-rate per session, skip `ctxenrich-*`/`title-*` sessions | S | Low | Cuts index runs by about 10× (embeddings, IPC payloads, lock holds) |
| 5 | Lock errors retryable: `mapToWriteError` → a new `ErrCodeBusy` with `IsRetryable()==true`; indexer retries 3× with backoff (250 ms/500 ms/1 s), since replace-all is idempotent; log Warn until the last attempt | S | Low | Transient residue, including busy-timeout expiry. Mitigation only: without #1 each retry still fails about 90 % of the time under streaming |
| 6 | Incremental session indexing: chunk per message, `metadata.message_id` + `updated_at`, replace only new or changed messages (plus title), keep a per-session watermark | M | Medium (chunk layout and search result shape change; needs a backfill or lazy migration) | O(n) re-embedding and O(n) tx size per run, the 12 MB IPC payloads, and head-of-line blocking in the coordinator |
| 7 | Serialise the primary's own writes through the coordinator or a process write mutex | L | Medium-high (touches every service) | Redundant once #1 + #2 exist; only worth it for metrics/backpressure |
| 8 | Secondaries always proxy (no direct-first) | M | Medium (IPC load, every write depends on primary liveness; see failover gaps G1–G5) | Not needed for this error; reduces cross-process commit bursts |
| – | Embeddings outside the tx and the coordinator | – | – | Already true for ReplaceSessionEvents. Only the code indexer `embedSymbols` runs inside a coordinator job (cause 3) |

### Sketches
```go
// (1) internal/rag/events/events.go — targeted immediate tx (same in kb.deleteDocument, DeleteEvent, relinkBatch)
var writeTx = &sql.TxOptions{Isolation: sql.LevelSerializable} // ncruces: BEGIN IMMEDIATE
tx, err := s.db.BeginTx(ctx, writeTx) // busy handler applies here; no read→write upgrade later
```
```go
// (1 global + 2) internal/db/connect.go
import sqlite3 "github.com/ncruces/go-sqlite3"; sqldrv "github.com/ncruces/go-sqlite3/driver"
func openPool(dbPath string, busy time.Duration) (*sql.DB, error) {
    dsn := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: "_txlock=immediate"}).String() // escapes spaces etc.
    return sqldrv.Open(dsn, func(c *sqlite3.Conn) error {
        if err := c.BusyTimeout(busy); err != nil { return err } // ctx-aware handler, overrides the 1-min default
        return c.Exec(`PRAGMA foreign_keys=ON; PRAGMA synchronous=NORMAL; PRAGMA cache_size=-8000;`)
    })
}
// Connect(): openPool(p, 10*time.Second) + MaxOpen 8; ConnectRWSecondary(): openPool(p, 200*time.Millisecond) + MaxOpen 1.
// Keep one-off PRAGMA journal_mode=WAL (persistent). Drop the per-pool Exec loop.
```
```sql
-- (3) new goose migration
-- +goose Up
CREATE INDEX IF NOT EXISTS idx_events_session ON events(subject, json_extract(metadata, '$.session_id'));
-- +goose Down
DROP INDEX IF EXISTS idx_events_session;
```
```go
// (4) remembrances_indexer.go event filter
if ev.Type == pubsub.UpdatedEvent && ev.Payload.FinishPart() == nil { continue } // skip mid-stream deltas
if isEphemeralSession(sessionID) { continue }                                  // ctxenrich-*, title-*
```

Tests:
- A concurrency test on a temp DB: goroutine A loops autocommit `UpdateMessage`; goroutine B loops `ReplaceSessionEvents` on a 5k-row table. Today it must reproduce BUSY; with #1 it must show zero failures. Run with `-race`.
- A migration test that asserts `EXPLAIN QUERY PLAN` uses `idx_events_session`.
- `go test ./internal/rag/... ./internal/db ./internal/ipc/dbproxy ./internal/app`.

## 6. Verdict
- Fixes 1 + 2 do **not** remove point-2 errors. The observed incident (2 streaming subagents + ACP, no mcp-server) would reproduce identically. Even a single process can hit it with concurrent sessions or background writers.
- **Minimal set to eliminate "database is locked" for point 2:**
  - **#1** IMMEDIATE on read-first write txs, at least ReplaceSessionEvents; the global `_txlock=immediate` is preferred.
  - **#2** An explicit per-connection busy_timeout, so IMMEDIATE waits are bounded and consistent.
  - **#3** The session-id expression index, to shrink the scan and the write-lock hold.
- After those, the only remaining path to the error is a genuine write-lock hold longer than busy_timeout.
- **Strongly recommended next:**
  - #4 (finished-message filter and debounce) plus #5 (retryable busy) as cheap load and residue reduction.
  - #6 (incremental indexing) to remove the large replace transactions that cause head-of-line blocking and prompt-path stalls, which feed the ACP/iOS hang.
