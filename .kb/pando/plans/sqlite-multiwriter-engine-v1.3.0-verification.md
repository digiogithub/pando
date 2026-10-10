---
created_at: 2026-10-10T21:49:08.288409525Z
updated_at: 2026-10-10T21:49:08.288409525Z
tags:
    - db
    - ipc
    - v1.3.0
    - verification
---
# SQLite multi-writer engine v1.3.0 — verification and follow-up fixes

Companion of [[sqlite-multiwriter-engine-v1.3.0]] (PANDO-EP-0022).

## Library bug found and fixed: go-sqlite-multiwriter v0.1.1
- v0.1.0 lost **acknowledged** commits when every writer process was SIGKILLed while the live log spanned ≥2
  segments: `wlog.replay` (log.go) treated the zero prefill at the end of a full multi-process segment
  (32 MiB) as a torn tail, set `stopped` and deleted later segments. Repro `TestMPAllKilledBig` (library
  mp_test.go): 173 acked vs 167 present.
- Fix: `continuesIn(id+1, prev)` — keep replaying when the next segment has a valid header (magic, id,
  salt) and its first record is epoch prev+1. Also `prepPaths` removes `*.prep.tmp` leftovers on last close.
- Published as **v0.1.1** (commit 2b14783 on digiogithub/go-sqlite-multiwriter main), Pando pins v0.1.1.
- Pando regression test: `internal/db` `TestCrashKilledWritersMultiSegment` (enabled, retries until ≥2 live
  segments at the kill).

## Pando fixes found during verification
- **Failover never happened after SIGKILL of the leader**: follower AcquireLock raced the kernel releasing the
  dead leader's flock → "another secondary promoted first" → watcher exited forever. Also a follower started
  before the leader bus was bound gave up subscribing. `internal/ipc/failover/watcher.go`: retry lock for
  `lockRetryWindow` (3 s), `runSecondary` loops `watchLeader` passes, subscription retried every second until
  HeartbeatTimeout. Takeover now < 1 s.
- **Concurrent first open**: one observed `unable to open database file (14)` when two serves started 2 s apart on
  a new DB (not reproducible in 150 later attempts). `internal/db/connect.go`: the whole open (engine open +
  migrations) runs under the exclusive `<db>.migrate.lock`; `pingWithRetry` retries SQLITE_CANTOPEN ≤5 s.

## Test inventory
- `internal/db/engine_driver_test.go`: write classification, RFC3339 binding (typeof=text), IsRetryable on
  real BUSY/BUSY_SNAPSHOT, RunTx deterministic conflict + 6×25 read-modify-write, autocommit RETURNING 8×40.
- `internal/db/engine_multiprocess_test.go` (unix): 4 processes × realistic writes (sessions, messages, KB+FTS,
  events+FTS, RunTx shared counter) → integrity_check, foreign_key_check, FTS integrity-check, per-child acked
  counts, goose rows; concurrent first start; crash kill 1/3 and 3/3; multi-segment crash.
- `internal/db/engine_maintenance_test.go`: auto_vacuum conversion, CompactPath in use / after exit / after
  SIGKILL recovery, engine-mix guard.
- `test/integration/single_writer` (`make test-integration`): new `TestLeaderFailoverAfterKill`,
  `TestConcurrentWritesFromManyProcesses` (2 serve + 4×5 concurrent `pando project add`, list count, no `-mw`
  leftovers after shutdown).
- Manual: real 3 GB DB copy — conversion 1m19s, quick_check ok, FTS ok, 200 autocommit writes 139 ms.
- Full `go vet ./...` and `go test ./...` green.

## Date/time handling (user question 2026-10-10)
DATETIME is only a declared type in SQLite; the VFS never sees values. Driver change ncruces→modernc: our
`pando-sqlite` driver binds time.Time as RFC3339Nano (as ncruces did; modernc default would be `t.String()`),
reads of DATE/DATETIME/TIMESTAMP still give time.Time, `_texttotime=1` for undeclared expressions. SQL-side
strftime defaults unchanged; sessions/messages use INTEGER ms.
