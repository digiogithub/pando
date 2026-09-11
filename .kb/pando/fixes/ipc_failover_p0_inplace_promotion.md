---
created_at: 2026-09-11T18:17:00.389254855Z
updated_at: 2026-09-11T18:17:00.389254855Z
tags:
    - fix
    - ipc
    - failover
    - sqlite
---
# Fix: IPC failover P0 — in-place promotion, zombie-lock guard, looping watcher, ordered handover, canonical workdir (2026-09-11)

Implements phase **P0 ("failover correctness")** of [[pando/plans/mcp_server_ipc_bootstrap.md]] (gaps G1–G6 of its §4, design §5.5). This is step 4 of [[pando/plans/sqlite_contention_fix_roadmap.md]]. It builds on the per-connection pool from [[pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md]] and on the message-level IPC ops from [[pando/features/session_index_incremental_per_message.md]]. G7 (the kill policy) and P1–P6 are **not** done. `cmd/mcp_server.go` is untouched.

## Per gap

### G1 — `PromoteToPrimary` could not write → in-place promotion
- **`internal/app/app.go` `App.PromoteToPrimary`**
  - The rewrite no longer closes `rt.SQLDB` or the IPC client, and no longer swaps `app.DBQuerier` or the stores.
  - Every service built by `app.New` therefore keeps valid references after promotion: session/message services, history, project, gateway, design provider, and the remembrances stores (the same `*sql.DB` and the same `*dbproxy.DBProxy`).
  - Steps, all under the new `app.ipcMu`:
    1. `db.PromoteToPrimaryPool(ctx, app.rwConn)`: the primary's busy_timeout and pragmas on **every** connection, `SetMaxOpenConns(8)`/`SetMaxIdleConns(4)`, then goose migrations.
    2. `ipcruntime.NewPrimaryBus` (with `ipc.ping`, like a Bootstrap primary), then `ipcBusSetupFunc` (coordinator + changepub + `RegisterHandlersWithCoordinator` + bridge handlers + bridge heartbeats), then `registerRemembrancesDispatcher()`.
    3. `startPrimaryBus`, which binds with retries.
    4. `DBProxy.Promote()`.
    5. `setupIPCLocked(bus)` (session IPC publisher, primary flag), plus recording the coordinator, lock and cancel for the handover.
    6. `watcher.SetPrimaryBus(bus)`.
    7. `reannounceAsPrimary()`.
    8. `startPrimaryServices` (P3 hook).
    9. Publish `instance.promoted`.
  - If an error occurs before step 4, `revertPromotedPool()` (`db.DemoteToSecondaryPool`) puts the pool back to 1 conn/200 ms, and the watcher releases the lock.
- **`internal/db/connect.go`**
  - The pool init callback now reads a per-pool `poolState` (atomic busy timeout plus atomic pragma string), registered in a `sync.Map` keyed by `*sql.DB`.
  - New: `reconfigurePool`, `PromoteToPrimaryPool`, `DemoteToSecondaryPool`, `runMigrations` (factored out of `Connect`), `ConnectAt`/`ConnectRWSecondaryAt` (path-based variants), and the role constants.
- **`internal/ipc/dbproxy/proxy.go`**
  - `client` is now an `atomic.Pointer[ipc.Client]`.
  - New:
    - `IsRemote()`: nil-receiver safe.
    - `Promote()`: swaps the client to nil and returns the old one, which the runtime still owns and closes.
    - `ErrNotRemote`
    - `Forward(ctx, method, params, timeout) (forwarded bool, err error)` and `ForwardWithResult[R]`: report forwarded=false when the proxy is nil, not remote, or lost its client to a promotion that raced the call.
  - `directOrProxy`/`directOrProxyVoid` retry the write directly if a promotion raced the fallback.
- **Stores** switched from `proxy != nil` to `Forward`/`IsRemote`:
  - `internal/rag/kb/kb.go`: add, delete, update.
  - `internal/rag/kb/backfill.go`: `BackfillLinks`, `RelinkAll`.
  - `internal/rag/kb/memory.go`: `upsertMemoryByKey`.
  - `internal/rag/events/events.go`: `SaveEvent`, `ReplaceSessionEvents`, `ReplaceMessageEvents`, `DeleteMessageEvents`. This covers the per-message indexing and the indexer's version-skew fallback, which call these methods.
  - `internal/rag/code/indexer.go`: upsert project, both `SetProjectStatus` writes, `IndexFile`, `DeleteProject`.
  - After promotion they all write directly; while still remote they forward exactly as before.

### G2 — promotion did not clean up
- The promoted App records its lock (`ipcLockRelease`, a `sync.OnceFunc` around `ipc.ReleaseLock`), its bus (`IPCBus`), its coordinator and a cancel func. Shutdown releases and closes them (see G5).
- `reannounceAsPrimary()` reads the registry entry (`instanceregistry.New().Get`) and re-announces it with `IsPrimary=true` and the bound ports.
- `instance.promoted` is published.
- Background services are **not** moved. `startPrimaryServices` is a named no-op hook with a `TODO(P3)`.
- `App.IPCIsPrimary` (a plain bool written by the watcher goroutine, which was a data race) was replaced by an atomic `ipcPrimary` plus `IsIPCPrimary()`. There were no external users.

### G3 — zombie lock on serve/desktop/app secondaries
- **Behaviour today:** serve/desktop/app have no secondary branch at all. `runtime.Bootstrap` starts the secondary watcher with `onPromote == nil`, so the old `triggerFailover` took the flock and declared "promotion complete".
- **Fix:** `failover.Watcher.triggerFailover` returns `outcomeSkipped` **before** `AcquireLock` when no promotion callback is registered, and logs why.
- The watcher keeps monitoring, so if P1 wires a callback later it works immediately.
- The serve/desktop/app entrypoints themselves are left for P1 (`wireIPC`).

### G4 — the watcher stopped after one attempt
- `runSecondary` is now a loop. After any attempt that does not promote (lost race, failed promote, disabled, lock error, subscription closed), it schedules the next check with a jittered (×0.5–1.5), doubling backoff: `Config.RetryBackoffMin/Max`, defaults 1 s/30 s.
- A heartbeat or `instance.promoted` resets the backoff. A failed subscription is retried.
- On success it switches to publishing heartbeats on the bus set via `SetPrimaryBus`.
- It keeps **draining** the old subscription, which now points at its own PUB port. Otherwise zmq4 back-pressure could eventually block the PUB writer for every subscriber.
- `failoverMu` serialises attempts from the monitoring loop and from `CheckAndMaybeFailover` (prompt path). A `promotedC` channel lets the loop notice a promotion won on the prompt path.
- All role and bus access is under `mu`; `Start`/`Shutdown` used to read `role` unlocked.
- `TestDefaultConfig` still holds (Enabled, 5 s, 15 s).

### G5 — graceful handover race
- **Secondary side:** after `instance.shutdown`, `acquireLock` retries for `ShutdownAcquireWindow` (5 s) every jittered `ShutdownAcquireInterval` (100 ms). It stops early when the lock file names an instance other than the one that announced its shutdown, meaning another secondary already won.
- **Primary side:** new `App.releasePrimaryRole()`, the first thing `App.Shutdown` does:
  1. `coord.Drain` (bounded by 5 s), then `coord.Shutdown`.
  2. Release the lock.
  3. + 4. `Bus.Shutdown`, which publishes `instance.shutdown` and then closes the bus.
  5. The rest of Shutdown (extensions, agent-vcs cleanup, LSP ...) no longer delays the handover.

  It is idempotent, and a promotion that races it is refused.
- **`writecoordinator.Coordinator.Drain(ctx)`** (new):
  - Sets `draining`; `Submit` then returns `ErrDraining`.
  - Enqueues a barrier job and waits until every job queued before it has run.
- **Normal primaries** register the same resources via the new `App.SetIPCPrimaryHandover(coord, rt.ReleaseLock)` in `cmd/root.go`, for both TUI and ACP.
  - In ACP, `defer acpCoord.Shutdown()` was removed. Defers run LIFO, so it ran *before* `pandoApp.Shutdown` and discarded queued writes instead of draining them.
- **`ipcruntime.BootstrapResult`**:
  - `Cleanup` is now `sync.OnceFunc`, so it is idempotent.
  - New idempotent `ReleaseLock()`.
  - The primary Cleanup order is now `ReleaseLock` → `bus.Shutdown` (announce and close) → `watcher.Shutdown` → `conn.Close`. serve/desktop/app, which do not register the App handover yet, therefore also release the lock before announcing, just later.
- **`ipc.Bus`**:
  - `Shutdown` is idempotent (`sync.Once`).
  - `Publish` after shutdown returns `ipc.ErrBusClosed`, which the watcher logs at DEBUG.
  - `Shutdown` lingers 100 ms between publishing `instance.shutdown` and closing the sockets. zmq4's PUB writer drops messages still queued at close (verified in `pub.go`: `pubMWriter.Close` does not flush).

### G6 — relative or symlinked workdir
- `runtime.canonicalWorkdir`: `filepath.Abs` + `filepath.EvalSymlinks`, falling back to the absolute path and then to the input. It is applied at the top of `Bootstrap`, before `PortsForPath`/`AcquireLock`.
- The secondary watcher is now bound to the **primary's ports from the lock file**, not its own derived ports. A promoted instance therefore binds, and records in the lock, exactly the ports every other secondary's `DBProxy` already targets, even against an older primary that hashed a differently spelled path.
- Stale "disabled by default" comments fixed in `runtime.go` (the `Watcher` field and secondary watcher block) and in `cmd/root.go` (the `--auto-failover` block). The `SQLDB` field comment is updated too (a secondary holds a 1-conn RW pool, not RO).

## Extra fixes found while implementing (needed for correctness)
- **Lock-file unlink race (`internal/ipc/lock_unix.go`)**
  - The old `ReleaseLock` did unlock, close, then `os.Remove`.
  - A process that opened the old inode just before the unlink could flock it while another created and flocked a new file at the same path: two primaries. The G5 retry loop would hit this window often.
  - Now `ReleaseLock` **truncates** the file while still holding the lock, then unlocks and closes. It never unlinks.
  - `AcquireLock` also checks that the locked fd is still the inode at the path (`sameFileAtPath`, guarding against older binaries that still unlink).
  - It retries briefly when the file is held but unparsable (the holder is rewriting it, or has just truncated it) instead of returning an error. That error would have made `Bootstrap` "continue as primary without IPC".
  - All five `ReadLockForPath` callers (`cmd/db.go`, `cmd/ipc.go`, `internal/project/{manager,delegation,delegate_external}.go`) already treat an unreadable file or a dead PID as "no primary". `pando ipc status` now correctly shows "no active primary" after a release instead of stale info.
  - Windows (`lock_windows.go`) is unchanged.
- **Bind retry (`startPrimaryBus`, up to 3 s every 50 ms)**
  - Found by the smoke test. In a graceful handover the secondary wins the lock immediately, but the old primary still owns its sockets during the 100 ms announcement linger. The first promotion failed with `bind PUB socket ... could not listen` and only recovered through the G4 loop.
  - It now waits for the ports instead. Covered by `TestPromoteToPrimaryWaitsForPortsToFree`.
- **Dispatcher ordering:** the remembrances dispatcher is registered **before** `bus.Start` on promotion. Otherwise the first forwarded remembrances write could get "unsupported remembrances write method", which secondaries misread as version skew and answer with a full-transcript rebuild.
- `SetIPCSecondaryContext` takes an `IPCBusSetupFunc` returning `(PrimaryWriteCoordinator, error)`. It no longer keeps a separate `ipcROConn`: it validates that the pool equals `app.rwConn`, and it is nil-safe for the watcher.

## Design decisions

### Busy-timeout switch on a live pool
- **Chosen:** an atomic per-pool state read by the init callback, plus re-applying the settings to the existing connections by checking out `MaxOpenConnections` connections at once and calling `BusyTimeout`/`Exec(pragmas)` through `(*sql.Conn).Raw` (`reconfigurePool`).
  - A pool can never hold more physical connections than `MaxOpenConnections`, so holding that many means holding **every** connection it has: existing ones are reconfigured directly, and any it has to open get the new values from the init callback.
  - A connection in use is waited for, bounded by ctx (60 s `promotePoolTimeout`), not skipped.
  - That is why the pool size is raised only **after** reconfiguring (from 1 → 8) and lowered only after (demotion).
- **Rejected:** recycling idle connections (`SetMaxIdleConns(0)`). A connection in use at that moment returns to the pool later still at 200 ms, so "no connection stays at 200 ms" could not be guaranteed.
- **Rejected:** DSN `_pragma=`. The ncruces driver then skips its own default and bakes the value in at open, so a live pool still cannot change it.
- Verified by `TestPromoteToPrimaryPoolReconfiguresEveryConnection`: it holds the only connection while promotion starts, asserts promotion waits, then sees all 8 connections at 10000 ms and `foreign_keys=1`, confirms migrations ran, and checks that demotion restores 1 conn/200 ms.

### Shutdown order
drain → release lock → announce → close bus → rest.
- Releasing before announcing means a secondary reacting to `instance.shutdown` finds the lock free. Before, it lost, logged "another secondary promoted first", and its watcher exited (G4), leaving **no** primary.
- Draining first means the writes secondaries already forwarded are applied before the next primary can race them.
- Writes that arrive after draining get `ErrDraining`. That maps to `ErrCodeInternal` on the secondary (not retryable), so remembrances writes during the handover window can fail. This is a known, accepted cost; see the risks.

### `Forward` helper
- Stores need one uniform "forward, or write locally" decision that stays correct when a promotion lands between the check and the call.
- `Forward` returns forwarded=false on `ErrNotRemote`, and the store falls through to its direct path.

## Verification
- `go build ./...` is clean. `go vet` on `./internal/db/ ./internal/ipc/... ./internal/rag/{kb,events,code} ./internal/app ./cmd` is clean.
- `gofmt -l` is clean on all touched files. It flags `internal/rag/kb/types.go` and `internal/rag/code/graph.go`, which are pre-existing and untouched.
- `go test -race ./internal/ipc/... ./internal/app ./internal/rag/events ./internal/rag/kb`: all ok, no data races. The pre-existing `SyncDirectoryWithStats` race did not trigger in these runs; it is timing-dependent.
- `go test ./internal/rag/... ./internal/db/... ./cmd ./internal/mesnada/...`: all ok.
- `./internal/llm/agent`: only the 4 known pre-existing failures (the caveman tests and `TestApplyToolDiscoveryWithoutManagerIsUnchanged`).
- `cmd` `TestRunACPServerWithOptions_ConfiguresSecondaryIPCFailoverPath` still passes.

### New tests
- `internal/ipc/failover/watcher_failover_test.go` (fake subscriber, real flock in a temp dir):
  - a nil callback never holds the lock through timeouts and a shutdown event;
  - keeps monitoring after lost races, then promotes when the lock frees;
  - retries after 2 failed promotes;
  - keeps monitoring while disabled, then promotes once re-enabled;
  - retry-acquire after `instance.shutdown` wins when the old primary releases 300 ms later;
  - a promoted watcher heartbeats on the `SetPrimaryBus` bus and announces shutdown once.
- `internal/ipc/dbproxy/proxy_promote_test.go`:
  - `IsRemote`/`Forward` are nil-safe;
  - `Promote` flips to passthrough (`WriteWithRetry` returns `ErrNotRemote`, `Forward` returns false, probe returns nil, direct writes work);
  - concurrent writers during `Promote` under `-race`: 150/150 rows.
- `internal/ipc/writecoordinator/drain_test.go`: queued jobs complete, then `ErrDraining`; `Drain` is idempotent and safe after `Shutdown`.
- `internal/ipc/lock_release_test.go`: release truncates instead of unlinking and reads as "no primary"; re-acquire works; the replaced-inode check.
- `internal/db/promote_pool_test.go`: see the busy-timeout section above; also rejects a pool not opened by `openPool`.
- `internal/ipc/runtime/runtime_canonical_test.go`: absolute, relative, `./x/`, `x/../x`, symlink and `symlink/.` spellings give the same canonical path, ports and lock contention; fallback for a missing dir.
- `internal/ipc/runtime/runtime_cleanup_test.go`: a real `Bootstrap` primary (isolated HOME, `config.ResetForTests`); `ReleaseLock` frees the lock; `ReleaseLock`/`Cleanup` twice are harmless; DB and bus closed.
- `internal/app/promote_test.go`:
  - **G1 regression.** A secondary fixture: a temp DB, `ConnectRWSecondaryAt`, a DBProxy to a dead endpoint, and KB/events stores with that proxy. After `PromoteToPrimary`:
    - same pool and proxy, `IsRemote` false, primary role;
    - 8 conns, all at `busy_timeout=10000`;
    - session and message writes via the pre-built services;
    - KB add, event replace and message-event replace, all direct (3 s deadline);
    - a *second* secondary forwards both a sqlc `CreateSession` and a remembrances `ReplaceMessageEvents` over real ZMQ to the promoted bus.

    Before the fix, promotion closed the pool, so the session write failed with "database is closed".
  - `TestPromoteToPrimaryWaitsForPortsToFree`.
  - `TestPromoteToPrimaryRefusedAfterHandover`.
  - **G5 ordering:** a real bus plus a ZMQ subscriber; on receiving `instance.shutdown` the subscriber takes the lock successfully, so the lock was already free. The coordinator rejects writes afterwards and `Publish` returns `ErrBusClosed`.

### Manual two-process smoke test (fully isolated)
- A scratch binary (`go build -o <scratchpad>/smoke/pando .`) was driven by `<scratchpad>/smoke/smoke.sh eof|term`.
- Isolation: each run happens inside `unshare -Urmn` (private user+mount+**net** namespace, so its loopback cannot clash with real instances and it has no outbound network) with a tmpfs over `/tmp/pando-instances`, `HOME` in the scratchpad, and a throwaway project dir. The real `.pando/` was never touched.
- The script:
  1. starts primary `pando acp --cwd <tmp>/proj --debug --log-file primary.log` (stdin from a fifo);
  2. then the secondary likewise;
  3. stops the primary: **eof** closes its stdin (graceful ACP exit), **term** sends SIGTERM (ACP has no SIGTERM handler, so no defers run and the kernel frees the flock);
  4. sends ACP `initialize` plus `session/new` to the secondary and counts `sessions` read-only with sqlite3.
- **eof** (final run):
  - The primary logs `acp: stdin closed by client (EOF)`, then `IPC handover: primary role released` about 100 ms later (the announcement linger).
  - The secondary logs `received graceful shutdown` → `trigger=shutdown` → `acquired lock` (same millisecond, so the lock was already free) → `PromoteToPrimary complete`, 101 ms later (the bind retry waited out the old sockets).
  - Promotion was observed 0.109 s after the stdin close. The same ports were rebound, the lock file names the secondary, and the registry re-announces it `is_primary: true`.
  - `sessions before=0 after=1` (a write after promotion). The lock file is 0 bytes after the final exit.
  - The first eof run revealed the port-bind race fixed above. It recovered via the G4 loop 100 ms later, but that relied on a failed promote.
- **term**: `subscription closed` → promoted in 8 ms; `sessions 0 → 1`; on the secondary's own exit, `IPC handover: primary role released`, and the watcher's redundant shutdown publish is logged at DEBUG (`bus is shut down`).

## What remains
- **P1 `wireIPC`**
  - serve/desktop/app still have no secondary branch, no promote callback (so G3 now keeps them safely non-promoting, but they never take over) and no `SetIPCPrimaryHandover`: their lock is released by `rt.Cleanup` (in the right order, but only at process end).
  - They also never call `SetupIPC`, so the remembrances dispatcher is not registered on a serve/desktop/app primary (pre-existing).
  - Also in P1: `BootstrapWithOptions`, `ModeMCP`.
- **P2** mcp-server on the bootstrap.
- **P3** role-aware services (fill `startPrimaryServices`, gate them in `New`).
- **P4–P6** per the plan.
- **G7** kill policy (SIGKILL of a suspended primary): untouched.
- **ACP SIGTERM:** `runACPServerWithOptions` ignores only SIGPIPE, so SIGTERM kills ACP without the ordered handover. Failover still works via the kernel flock release and `subscription closed`, but queued forwarded writes are lost. A signal handler belongs with P2's signal and EOF work.

## Risks and unverified
- Writes forwarded during the handover window fail with `ErrDraining` (not retryable) or with unreachable/timeout after 3 short retries. `WriteWithRetry` does not wait for the new primary; the plan's optional "wait up to ~20 s when the lock shows a new PID" is not implemented.
- `reconfigurePool` waits for in-use connections, up to 60 s. A long-running transaction on the secondary pool delays promotion, bounded.
- The instance registry is still hardcoded to `/tmp/pando-instances`; the smoke test isolated it with a tmpfs mount.
- Multi-process tests (P6, Python under `tests/`) are still to be written. The smoke test is a manual script in the scratchpad, not checked in.
- Windows: `lock_windows.go` still removes the file on release (`O_EXCL` create semantics); it is not affected by the unlink race in the same way and is not changed or tested here.
