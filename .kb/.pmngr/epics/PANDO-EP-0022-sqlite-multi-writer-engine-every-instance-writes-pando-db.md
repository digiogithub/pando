---
id: PANDO-EP-0022
type: epic
title: "SQLite multi-writer engine: every instance writes pando.db directly (v1.3.0)"
status: in_progress
priority: high
labels: [db, ipc, release]
created: 2026-10-10T20:30:13Z
updated: 2026-10-10T20:30:13Z
started: 2026-10-10T20:30:13Z
---

## Description
Replace the ZMQ primary/secondary single-writer machinery for `pando.db` with
[go-sqlite-multiwriter](https://github.com/digiogithub/go-sqlite-multiwriter) v0.1.0 so every Pando
process writes the database directly and safely.

Today the IPC primary owns writes; secondaries write directly with a 200 ms busy timeout and fall back
to a `db.write` ZMQ RPC (`dbproxy`, `writecoordinator`, remembrances dispatcher, `changepub`), while
`mcp-server` and several CLI commands open the DB as unlocked independent writers. This produces
`database is locked` errors, proxy hangs and a lot of failover code.

Target architecture:
- One engine layer in `internal/db`: driver `pando-sqlite` = modernc.org/sqlite + multiwriter VFS in
  multi-process mode (unix). Windows: stock modernc SQLite, WAL, `busy_timeout`, `BEGIN IMMEDIATE`.
- Autocommit writes retried transparently on `SQLITE_BUSY_SNAPSHOT`; explicit transactions through
  `db.RunTx` (whole-transaction retry).
- Cross-process locks: shared lifetime lock per process, exclusive maintenance lock (auto_vacuum
  conversion, `pando db compact`), migration lock around goose.
- IPC keeps only non-DB roles: leader election for singleton jobs (code index + watcher, evaluator),
  bus for instance browsing, remote view and hot-peer delegation.
- `ncruces/go-sqlite3` removed.

Analysis/plan: KB `pando/plans/sqlite-multiwriter-engine-v1.3.0.md`.

## Acceptance Criteria
- Every entrypoint opens `pando.db` through the engine; no `db.write` RPC remains.
- 3–5 concurrent Pando-like processes writing sessions/messages/KB/events/code index: no lost writes,
  `PRAGMA integrity_check` = ok, FTS5 `integrity-check` ok, invariants hold.
- Processes killed mid-write leave a database that recovers without manual repair.
- Concurrent first start runs migrations exactly once.
- Existing DBs with `auto_vacuum=INCREMENTAL` are converted once, safely.
- `go test ./...` green; release v1.3.0 tagged.

## Notes
All Pando instances sharing a database must run >= v1.3.0: an older binary opening the file directly
while the engine holds commits in its `-mw` log is unsafe.
