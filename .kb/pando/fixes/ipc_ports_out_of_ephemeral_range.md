---
created_at: 2026-09-12T10:51:25.782467962Z
updated_at: 2026-09-12T10:51:25.782467962Z
tags:
    - fix
    - ipc
    - ports
    - failover
---
# Fix: IPC derived ports moved out of the OS ephemeral range + loud bind failures

Date: 2026-09-12. Related: [[pando/changes/ipc_multiprocess_tests_p6.md]], [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/ipc_failover_p0_inplace_promotion.md]].

## Problem

`ipc.PortsForPath` hashed the canonical workdir (FNV-32a) into the **40000-60000** window.
That window overlaps the OS ephemeral/dynamic port range on every supported platform, so an
unrelated program can transiently own the port; the Pando primary then fails to bind its bus and
`bus.Start` error handling only did `logging.Warn("... continuing without IPC")` and carried on.
The instance stays primary (holds lock + RW DB) but is unreachable over RPC, so secondaries fail
with "connection refused". Found while de-flaking `TestCronJobReloadRPCReachesPrimary`.

Ephemeral ranges: Linux 32768-60999, macOS 49152-65535, Windows 49152-65535. "Above 61000" is
therefore wrong (collides on macOS/Windows); the window must sit **below 32768**.

## Chosen range: 20000-25999 (RPC up to 26000)

`base = 20000 + fnv32a(abs_path) % 6000`, `pub = base`, `rpc = base + 1` (pair scheme and
determinism unchanged).

- Entirely below 32768, so it is outside the ephemeral range on Linux, macOS and Windows.
- Above the well-known / registered ports dev tooling uses (3000, 3306, 5000, 5432, 6379, 8000,
  8080, 9000, 9090).
- Below the next dense cluster: 26257 CockroachDB, 27015 Steam, 27017-27019 MongoDB, 28015
  RethinkDB. The width was traded from 20000 to 6000 slots to buy that clearance; cross-project
  hash collisions are still rare (~0.7% for 10 projects) and no longer silent.
- The constants carry a comment documenting the three ephemeral ranges and the history.

## Compatibility with a running old-binary primary

Verdict: **a new binary can never silently become a second primary**, and the one theoretical
hole was closed.

- `runtime.Bootstrap` derives ports only to *offer* them to `AcquireLock`. Whoever loses the
  flock reads the primary's `LockInfo` and uses `lockInfo.PubPort/RPCPort` for the DB proxy, the
  SUB subscription and `killStalePrimary`'s liveness probe. So a new binary joining an old-binary
  primary connects to the old 4xxxx ports; only the derived numbers differ. Symmetrically, an old
  binary joining a new-binary primary reads the new 2xxxx ports from the lock file.
- Promotion (`app.PromoteToPrimary`) binds `app.ipcPubPort/ipcRPCPort`, which come from
  `SetIPCSecondaryContext(rt.PubPort, rt.RPCPort)` = the lock-file ports — correct, existing
  secondaries keep working across a handover.
- **Bug found and fixed:** `runtime.Bootstrap` passed the *derived* ports to
  `failover.NewWatcherForSecondary`, and the watcher writes those into the lock file when it wins
  the race (`ipc.AcquireLock(w.workdir, w.instanceID, w.pubPort, w.rpcPort)`), while the app binds
  the lock-file ports. Identical with one binary, but under version skew the promoted instance
  would advertise ports nobody listens on. It now passes `res.PubPort/res.RPCPort`.
- **Second hole closed:** `AcquireLock` on a held lock whose file was empty/half-written (the
  primary truncates before rewriting) returned an error; `Bootstrap` treated any lock error as
  "continue as primary" — a second RW writer on the same DB. Now `waitForLockInfoRetry`
  (10 x 20 ms) rides out the rewrite window, and an unreadable-but-held lock returns the new
  `ipc.ErrPrimaryLockHeld`; `Bootstrap` retries once and then **fails hard** with an Error log
  instead of starting a second primary.
- `pando ipc status` now labels the derived ports as "Derived ..." and the lock-file ports as
  "(in use)", so the two can no longer be confused.

## Loud failure instead of "continuing without IPC"

New `internal/ipc/bind.go`:

- `StartBusWithRetry(ctx, bus, pub, rpc)` retries the bind for 3 s every 50 ms (same budget as the
  handover linger), then logs at **Error** with both ports, the per-port state from
  `describePortHolder` (a plain `net.Listen` probe: "in use by another process" / "free now"),
  the retry budget, the likely cause and an `ss -ltnp` hint, and returns an error wrapping
  `ipc.ErrBusBindFailed`.
- stdout stays clean (mcp-server stdio): logging only, no `fmt.Print`. The ACP path in
  `cmd/root.go` also moved off `logger.Printf` to `logging.Error`.
- Owning PID is deliberately not resolved: there is no cheap portable way and the failure path
  must not fork external tools.

**Deliberate behaviour on failure (documented in the helper's doc comment):** the instance keeps
the lock and the RW connection. It is still the only legitimate writer for the workdir; releasing
the lock would let a second process open the DB read-write. Follow-up (not done here): secondaries
of such a primary still fail at RPC time; the lock file has no "IPC unavailable" marker and
`instanceregistry` still shows `IsPrimary: true`. A future change could add a health flag to
`LockInfo`/the registry so `pando ipc status` reports "primary without IPC" directly.

## Files / symbols

- `internal/ipc/ports.go` — `portBase` 40000→20000, `portRange` 20000→6000, documented constants.
- `internal/ipc/bind.go` (new) — `StartBusWithRetry`, `describePortHolder`, `busBindRetryFor`.
- `internal/ipc/errors.go` — `ErrPrimaryLockHeld`.
- `internal/ipc/lock_common.go` — `waitForLockInfoRetry`, `lockInfoReadAttempts/Delay`.
- `internal/ipc/lock_unix.go`, `lock_windows.go` — return `ErrPrimaryLockHeld`.
- `internal/ipc/runtime/runtime.go` — `ErrPrimaryLockHeld` handling in `Bootstrap`; watcher gets
  the lock-file ports.
- `internal/app/app.go` — `PromoteToPrimary` uses `StartBusWithRetry`.
- `cmd/root.go` (TUI + ACP), `cmd/app.go`, `cmd/serve.go`, `cmd/desktop.go` — `StartBusWithRetry`
  + Error-level logging.
- `cmd/ipc.go` — "Derived PUB/RPC port" labels.
- `internal/ipc/ipc_test.go` — updated range test; new
  `TestPortsForPathWindowOutsideEphemeralRange`, `TestAcquireLockSecondaryUsesPrimaryPortsFromLockFile`,
  `TestAcquireLockUnreadableLockInfoIsNotPrimary`, `TestStartBusWithRetryFailsLoudlyOnPortConflict`.

## Verification

- `go build ./...`, `go vet ./internal/ipc/... ./cmd ./internal/app` clean.
- `go test -race -count=1 ./internal/ipc/... ./cmd/... ./internal/app/...` all pass;
  `-count=5` on `./internal/ipc ./internal/ipc/runtime ./internal/ipc/failover` also passes.
- `TestCronJobReloadRPCReachesPrimary`, `projectWithFreeIPCPorts` and
  `tests/test_ipc_multiprocess.py` do **not** exist in this checkout (P0/P6 have not landed here
  yet); nothing else in `tests/*.py` hardcodes IPC ports. When P6 lands, its helper should keep
  picking free ports — the new window makes that easier, not harder.
