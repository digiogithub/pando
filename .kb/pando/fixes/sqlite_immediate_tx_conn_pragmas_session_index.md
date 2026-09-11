---
created_at: 2026-09-11T15:41:04.791769928Z
updated_at: 2026-09-11T15:41:04.791769928Z
---
# Fix: SQLite "quick wins" — IMMEDIATE write transactions, per-connection pragmas, session expression index (2026-09-11)

Implements step 1 ("Quick SQLite wins") of [[pando/plans/sqlite_contention_fix_roadmap.md]], the root-cause fix for the `events: fts delete: sqlite3: database is locked` failures analysed in [[pando/analysis/session_index_locked_residual_risk.md]] and [[pando/analysis/sqlite_locked_interrupted_errors.md]].

## What changed

### 1. `internal/db/connect.go` — global `_txlock=immediate` + per-connection init

Rewrote `Connect()`, `ConnectRWSecondary()`, `ConnectReadOnly()` around two new helpers:

- `buildDSN(path string, extra url.Values) string` — builds a `file:` URI DSN via `net/url` (so spaces/special characters in the path are escaped). The path is resolved to **absolute** first (`filepath.Abs`): SQLite's URI filename parsing expects an absolute path component, and `config.Get().Data.Directory` defaults to the relative `".pando"`, so a naive `file:<relative-path>?...` DSN fails to open ("sqlite3: unable to open database file") for the common case, not just an edge case. `":memory:"` and the empty path are returned unchanged (no query string can attach to that plain SQLite filename form); no production caller passes those, only tests.
- `openPool(dbPath string, opts poolOptions) (*sql.DB, error)` — opens the DSN through `sqlite3driver.Open(dsn, init)` (`github.com/ncruces/go-sqlite3/driver`), whose `init` callback runs on **every** physical connection the pool ever creates (now and later), unlike the old `sql.Open` + one-off `db.Exec(pragma)`, which — because `database/sql` hands that `Exec` to whichever single pooled connection happens to be idle — only ever reached one of the (up to 8) connections. `poolOptions` carries `busyTimeout`, `immediateWrites` (adds `_txlock=immediate` to the DSN), `perConnPragmas` (a semicolon-joined `PRAGMA` string run in `init`), `readOnly` (`mode=ro`), and the pool size knobs.

Verified in the vendored `github.com/ncruces/go-sqlite3@v0.25.0` source (`driver/driver.go`) before implementing:
- `connector.Connect`: `if !n.pragmas { c.Conn.BusyTimeout(time.Minute) }` runs **before** `n.init`, unconditionally whenever the DSN carries no `_pragma=` param (ours never does) — so our `init` callback's own `BusyTimeout` call always runs after, and overrides, the driver's 1-minute default.
- `newConnector`: `_txlock` is parsed **only** when the DSN starts with `"file:"` and has a `?query`; accepted values are `deferred` (default), `immediate`, `exclusive` (and `concurrent`, an SQLite extension not used here).
- `conn.BeginTx`: for `opts.Isolation == LevelDefault`, it uses `c.txLock` (the DSN's `_txlock`) **only when `!opts.ReadOnly`**; `LevelSerializable` always forces `immediate` and `LevelLinearizable` always forces `exclusive`, **regardless of `_txlock`**; a `ReadOnly` transaction always stays `deferred`, regardless of `_txlock`.

This confirms the global-DSN approach from the roadmap's sketch works exactly as needed: a plain `db.BeginTx(ctx, nil)` on a pool opened with `_txlock=immediate` issues `BEGIN IMMEDIATE` (grabs the write lock at `BEGIN`, before any read, so the read→write lock **upgrade** that SQLite's busy handler ignores never happens), while any transaction explicitly opened with `&sql.TxOptions{ReadOnly: true}` is unaffected and stays `deferred`. No per-call-site `LevelSerializable` fallback was needed.

Chosen busy timeouts (`primaryBusyTimeout = 10s`, `secondaryBusyTimeout = 200ms`) and pragmas match the roadmap: `Connect()` (primary, 8 conns) gets `_txlock=immediate`, 10s busy_timeout, `foreign_keys=ON; synchronous=NORMAL; cache_size=-8000` on every connection, plus the one-off `journal_mode=WAL; page_size=4096` (these two persist in the file header, so — unlike the others — they only need to run once, exactly as before). `ConnectRWSecondary()` (1 conn) gets `_txlock=immediate`, 200ms busy_timeout (kept fast so it still fails over to the IPC proxy quickly, per its existing contract), `synchronous=NORMAL; foreign_keys=ON`, plus the one-off `journal_mode=WAL`. `ConnectReadOnly()` (currently dead code, not called anywhere in the repo, only defined) got `mode=ro` and a busy_timeout, but **not** `_txlock=immediate` — that DSN param is meaningless (and could turn a harmless `BeginTx(ctx, nil)` into an outright error) on a connection that can never write.

Bonus fix folded in here: `ConnectReadOnly`'s DSN was already `file:<path>?mode=ro`, so it had the exact same latent relative-path bug `buildDSN` now fixes — just never observed because the function is unused.

### 2. `internal/rag/kb/kb.go` — `KBStore.addDocument`: compute chunks/embeddings before opening the write transaction

Found while auditing "no write tx does network I/O while open" (part of the task-1 checklist): the non-proxy path of `addDocument` opened its `BeginTx`, inserted the document row (acquiring the write lock), indexed wiki links, chunked the content, and **then** called `s.embedder.EmbedDocuments` (a network call — local/remote embedding provider, `kbEmbeddingsTimeout = 45s`) — all before `tx.Commit()`. That holds the SQLite write lock for up to 45s. This was already true before this fix (the write lock in a DEFERRED tx is acquired by the first write statement, which ran before the embed call either way), but it becomes a much sharper problem now that busy_timeout is a bounded 10s on the primary: a write tx that legitimately holds the lock for 45s would make every *other* writer fail with "database is locked" after only 10s, instead of the old implicit 60s driver-default wait. Fixed by moving chunking + embedding before `BeginTx`, mirroring the proxy branch (which already had to precompute embeddings to ship them over IPC) — the two branches now share the same precomputed `chunks`/`embedVecs`, and the transaction only ever does local inserts. Audited every other write-transaction call site (all 19 `BeginTx`/`Begin` sites in the repo — see below): no other one does network I/O while a transaction is open. `events.go`'s `SaveEvent` already computed its embedding before `BeginTx`; `chunk.go` and the direct code-indexer paths take embeddings as already-computed parameters.

### 3. `internal/db/migrations/20260911000001_add_events_session_index.sql`

```sql
CREATE INDEX IF NOT EXISTS idx_events_session
    ON events(subject, json_extract(metadata, '$.session_id'));
```
Matches the exact expression `deleteSessionEventsTx`'s `SELECT`/`DELETE` use (`internal/rag/events/events.go`) byte-for-byte, so SQLite can pick it. Confirmed with a new `EXPLAIN QUERY PLAN` test (see below). This shrinks the session lookup from a scan of the whole `events` table (99.9% of rows have `subject='session'`, so `idx_events_subject` alone barely helps) down to roughly the rows of that one session — shortening both the read and, since the write lock is now held for the whole transaction under `_txlock=immediate`, the write-lock hold time.

## Files / symbols touched

- `internal/db/connect.go` — added `buildDSN`, `poolOptions`, `openPool`, `primaryBusyTimeout`/`secondaryBusyTimeout`; rewrote `Connect`, `ConnectRWSecondary`, `ConnectReadOnly`. `ConnectForRole` unchanged.
- `internal/rag/kb/kb.go` — `KBStore.addDocument`: reordered to embed before `BeginTx`; no behavior change to `AddDocument`/`UpdateDocument`/the proxy request shape.
- `internal/db/migrations/20260911000001_add_events_session_index.sql` — new goose migration (Up creates `idx_events_session`, Down drops it).
- `internal/rag/events/events_test.go` — added `idx_events_session` to the hand-rolled test schema (`openTestEventStoreDB`) so it matches the post-migration schema.
- New tests: `internal/db/connect_test.go`, `internal/rag/events/session_index_test.go` (below).

## Call-site audit ("any read-only tx using nil options should pass `ReadOnly: true`")

Grepped every `BeginTx`/`.Begin(` in the repo (excluding tests): `internal/design/store.go` (2), `internal/rag/chunk.go` (4), `internal/rag/code/indexer.go` (4), `internal/rag/events/events.go` (4), `internal/rag/kb/kb.go` (3), `internal/rag/kb/backfill.go` (1), `internal/history/file.go` (1) — 19 total, matching the roadmap's count. Read every one's body. **None of them are read-only transactions** — every one performs at least one write. They split into:
- **Write-first** (`design.AddVersion`/`ReplaceNodes`, `code.IndexFile`/`IndexFileDirect`/`DeleteProject*`, `kb.addDocument`/`AddDocumentWithEmbeddings`, `chunk.InsertChunk`, `history.createWithVersion`): the first statement in the tx is already a write, so the busy handler already applied even under the old DEFERRED default — unaffected by, and not requiring, this fix.
- **Read-then-write** (`events.ReplaceSessionEvents`/`deleteSessionEventsTx`, `events.DeleteEvent`, `kb.deleteDocument`, `kb.backfill.relinkBatch`, `chunk.UpdateChunk`/`DeleteChunk`/`DeleteCollection`): these are exactly the ones the read→write lock-upgrade bug affects. They all pass `nil` `TxOptions`, so the global `_txlock=immediate` DSN change covers every one of them automatically — **no call site needed an explicit change**. This is why the "global DSN" branch of the task was preferred over threading `&sql.TxOptions{Isolation: sql.LevelSerializable}` through four call sites: it covers this whole class (including two the roadmap didn't originally name — `chunk.UpdateChunk`/`DeleteChunk`/`DeleteCollection`) with one change, and the audit confirms no read-only transaction exists anywhere in the repo that the same global setting could have hurt.

## Verification

- `go build ./...` — clean.
- `gofmt -l` on every touched `.go` file — clean.
- `go vet ./internal/db/... ./internal/rag/... ./cmd/...` — clean.
- `go test ./internal/db/... ./internal/rag/... ./internal/ipc/... ./internal/app ./internal/session/... ./internal/message/... ./internal/history/... ./cmd` — all pass.
- `go test ./internal/llm/agent ./internal/api` (the CLAUDE.md-verified command) — `internal/api` passes; the 4 pre-existing failures in `internal/llm/agent` (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged`) are unrelated — they read the real `.pando.toml` — and were pre-existing before this change (not caused by it).
- **FK violations from enabling `foreign_keys=ON` on all 8 primary connections (previously ~7/8 ran with FKs off):** none surfaced in the full test run above. Not independently verified against the real, 1.97GB production `.pando/data/pando.db` (out of scope not to touch it) — see risks below.
- New tests:
  - `internal/db/connect_test.go`: `TestBuildDSN` (memory/empty passthrough, escaping, and — added after catching a regression, see below — relative-path resolution); `TestOpenPoolAppliesPragmasToEveryConnection` (opens 5 concurrent `db.Conn(ctx)` on a temp-file pool with primary-like options, asserts `foreign_keys=1`, `busy_timeout=10000`, `synchronous=1` on every one — this is the test that would have caught the old "only one pooled connection gets the pragma" bug); `TestOpenPoolSecondaryLikePragmas` (200ms variant); `TestOpenPoolImmediateWritesUsesBeginImmediate` (a plain `BeginTx` blocks a second plain `BeginTx` under a short busy_timeout, while an explicit `ReadOnly` tx is unaffected).
  - `internal/rag/events/session_index_test.go`: `TestReplaceSessionEventsConcurrentWritesNoLockErrors` (temp-file DB, ~3000 seed rows, goroutine A hammers autocommit inserts on an unrelated table while goroutine B runs 200 rounds of the real `EventStore.ReplaceSessionEvents` on a "hot" session; asserts **zero** lock failures with `_txlock=immediate`) and `TestReplaceSessionEventsConcurrentWritesDeferredFails` (same workload, DEFERRED DSN and the pre-index schema — reliably reproduces the bug: **~80/200 iterations failed with "database is locked" in every local run**, skips instead of failing if a given run doesn't reproduce it since it's inherently timing-dependent). Both pass with `-race`. `TestSessionEventsQueryUsesExpressionIndex`: `EXPLAIN QUERY PLAN` on the exact `deleteSessionEventsTx` query, asserts the plan mentions `idx_events_session`.

### Regression caught and fixed during verification

The first version of `buildDSN` wrapped the raw path in a `file:` URI without resolving it to absolute first. `config.Get().Data.Directory` defaults to the **relative** path `".pando"` (`internal/config/config.go`'s `defaultDataDirectory`), so `Connect()` routinely builds a DSN from a relative path — not just in some edge case. A relative `file:` URI path is not reliably opened by SQLite ("sqlite3: unable to open database file"), unlike a plain relative filename (which resolves against the process cwd exactly like any other file access). This broke `cmd.TestDesignSystemExtractRejectsUnknownSource` (and would have broken `Connect()` for essentially every real invocation that doesn't set an absolute `data.directory`). Fixed with `filepath.Abs` in `buildDSN` before building the URI, confirmed by re-running the previously-failing test, and added a permanent regression test (`TestBuildDSN/relative_paths_resolve_against_the_process_cwd_and_still_open`, using `t.Chdir`). Root-caused by temporarily restoring the pre-fix file via `jj file show -r @- internal/db/connect.go` and re-running the failing test to confirm it was newly introduced, not pre-existing.

### Pre-existing issue found, not fixed (out of scope)

`-race` on `./internal/rag/kb/...` reports a data race in `KBStore.SyncDirectoryWithStats` (`internal/rag/kb/sync.go:150,239,253` via `links.go:225,243`) — a map written by a worker goroutine while `filepath.WalkDir`'s callback goroutine reads the same map, unrelated to file paths or embeddings this change touches. Confirmed pre-existing by restoring the original `kb.go` and reproducing the identical race. Not fixed here — out of scope for the SQLite contention roadmap; worth its own fix.

## Risks / unverified

- Not tested against the real production `.pando/data/pando.db` (1.97GB, per [[pando/analysis/session_index_locked_residual_risk.md]]) — per instructions it was only opened read-only via `sqlite3 -readonly` for schema inspection during the earlier analysis, never written. If that database has any pre-existing FK-violating rows, turning `foreign_keys` fully ON (previously effectively off on 7/8 primary connections) could newly reject an `INSERT`/`UPDATE`/cascading `DELETE` that used to silently succeed. Recommend a one-time `PRAGMA foreign_key_check` against a copy of the real database before rolling this out.
- `secondaryBusyTimeout` stayed at 200ms as instructed ("keep the secondary's existing semantics of failing fast"); combined with `_txlock=immediate`, a secondary's direct-first writes now fail over to the IPC proxy on the same 200ms budget as before, just also covering the (previously non-existent, since remembrances writes always proxy) read-then-write case — no behavior change expected here, but not exercised by an actual multi-process secondary/primary test, only the single-process `openPool` unit tests.
- `primaryBusyTimeout = 10s`: chosen per the task and roadmap. `KBStore.addDocument`'s fix (embedding before the tx) removes the one identified >10s write-lock hold; no other write transaction in the audited 19 call sites does unbounded work inside the tx, but this wasn't exhaustively load-tested under production-scale concurrency (multiple subagents + swarms), only the synthetic two-goroutine test.

## What's left (roadmap, unchanged by this step)

- #4 Indexer load reduction (finished-message filter, longer debounce, per-session rate cap, skip `ctxenrich-*`/`title-*` sessions).
- #5 Retryable busy errors (`ErrCodeBusy` in `internal/ipc/dbproxy/errors.go`, indexer retry with backoff).
- #6 Incremental session indexing (per-message chunks instead of full-transcript replace).
- IPC P0 failover fixes and beyond ([[pando/plans/mcp_server_ipc_bootstrap.md]]).

See [[pando/plans/sqlite_contention_fix_roadmap.md]] (updated alongside this document) and [[pando/analysis/session_index_locked_residual_risk.md]] for the full ranked list and rationale.