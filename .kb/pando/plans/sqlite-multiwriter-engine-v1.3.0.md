---
created_at: 2026-10-10T20:56:20.660965637Z
updated_at: 2026-10-10T20:56:20.660965637Z
tags:
    - plan
    - db
    - ipc
    - v1.3.0
---
# SQLite multi-writer engine (EP-0022, v1.3.0)

**Date**: 2026-10-10 · **Epic**: PANDO-EP-0022 (stories PANDO-US-0126 … PANDO-US-0132) · **Status**: implemented, in review

Replaces the ZMQ primary/secondary single-writer model ([[unified_single_writer_master_plan]],
[[remembrances_ipc_proxy_implementation_plan]], [[fix_ipc_failover_p0_inplace_promotion]]) with
[go-sqlite-multiwriter](https://github.com/digiogithub/go-sqlite-multiwriter) v0.1.0 so every Pando process
reads and writes `pando.db` directly.

## Why
- Secondaries already wrote directly with `busy_timeout=200` and fell back to a `db.write` ZMQ RPC
  (`dbproxy`), remembrances writes always went over IPC (`rag/proxy` dispatcher), `mcp-server` and CLI
  commands opened the DB as unlocked writers. Result: `database is locked`, proxy hangs, failover bugs
  (promoted instance kept a closed proxy; serve/app/desktop followers had no promote callback).
- The library gives MVCC-like concurrent writers (private WAL lanes, page validation, first committer
  wins, shared commit log across processes, crash recovery).

## Analysis findings
- One DB file only (`<data.directory>/pando.db`); KB/events/code index/RAG share it. FTS5 only; **no
  sqlite-vec** (comments in `internal/rag/types.go` were wrong); embeddings are BLOBs scored in Go.
- Library runs on **modernc.org/sqlite**; Pando used **ncruces/go-sqlite3** (WASM). Driver switched.
  - Time: ncruces bound `time.Time` as RFC3339Nano, modernc as `t.String()` → our driver binds RFC3339Nano.
    Both decode DATE/DATETIME/TIMESTAMP columns to `time.Time`; `_texttotime=1` covers undeclared expressions.
- Under contention ~35% of autocommit statements fail with `SQLITE_BUSY_SNAPSHOT` (prototype, 3 procs ×
  4 goroutines) → retry is mandatory at driver level.
- Engine refuses databases with `auto_vacuum != NONE` (header bytes 52–55). Real user DB (3 GB) had
  `auto_vacuum=2` from `pando db compact` → one-time conversion needed (measured 1m19s, quick_check ok,
  FTS integrity ok, 200 autocommit writes 139 ms, close/compaction 1 s).
- Multi-process mode is unix-only → Windows uses stock SQLite (WAL, `_txlock=immediate`, busy_timeout 10 s).

## Design (internal/db)
- `driver.go`: driver `pando-sqlite` wraps modernc. Autocommit write statements (classified by leading
  keyword; anything not clearly a read is a write) retried on BUSY/BUSY_SNAPSHOT/LOCKED with µs→20 ms
  backoff for ≤30 s; write queries with rows (`RETURNING`) are drained into memory before returning so the
  retry happens before the caller reads. Statements inside explicit transactions pass through.
- `runtx.go`: `db.RunTx(ctx, conn, fn)` — whole-transaction retry. fn must have no side effects outside tx.
- `connect.go`: `Connect/Open/OpenNoMigrate/Close`, `SelectedEngine` (`PANDO_DB_ENGINE=sqlite|multiwriter`),
  `DSN`. Locks: `<db>.lock` shared per open DB for process lifetime (released by `db.Close`), exclusive for
  maintenance; `<db>.migrate.lock` exclusive around goose (goose.Up retried). Auto_vacuum conversion under
  the exclusive lock with stderr notice. Stock opener refuses when `<db>-mw` exists.
- `compact.go`: `CompactPath` needs exclusive usage lock (`ErrDatabaseInUse`), recovers a leftover `-mw`
  log first, then VACUUM with stock engine; auto_vacuum options only on the stock engine.
- Options: multiwriter MultiProcess, synchronous FULL (library default, tested path), foreign_keys(1),
  cache_size(-8000), pool 8/4, ConnMaxIdleTime 5 min.

## IPC after the change
- Deleted: `internal/ipc/dbproxy`, `internal/ipc/writecoordinator`, `internal/ipc/changepub`,
  `internal/rag/proxy`, `SetWriteProxy` paths in kb/events/code, `NewRemembrancesServiceWithProxy`,
  `ConnectReadOnly/ConnectForRole/ConnectRWSecondary`, `db.compact` RPC (`protocol.MethodDBCompact`,
  `DBCompactParams`), `killStalePrimary` (+ `runtime_kill_test.go`), METHOD_NOT_FOUND pause in the session
  indexer, `RegisterRemembrancesWriteDispatcher`.
- `ipc/runtime.Bootstrap`: `db.Connect()` first for every role; lock only elects the **leader** (bus +
  singleton jobs: code index/watcher, evaluator `RunBackground(isLeader)`).
- `cmd/ipc_wiring.go` `wireIPCRole` used by TUI, ACP, serve, app, desktop (uniform: leader SetupIPC + bus +
  bridge + watcher; follower `SetIPCSecondaryContext` + promote callback).
- `app.AppOptions.IPCFollower` (api passes `Role=="secondary"`) → `isSecondaryAtStartup`/`isLeader`.
- `PromoteToPrimary` no longer reopens the DB; `failover.triggerFailover` releases the lock when no
  promote callback is registered.
- `/db-compact` in a running instance explains to close instances and run `pando db compact`.
- 19 explicit transactions converted to `db.RunTx` (rag chunk, kb, backfill, events, code indexer,
  design store, history); embeddings moved before transactions (`kb.addDocument`, `events.SaveEvent`).

## Upgrade notes
All instances on a DB must be ≥ v1.3.0. User doc: `docs/database.md`.

## Verification
See [[sqlite-multiwriter-engine-v1.3.0-verification]] (filled after tests).
