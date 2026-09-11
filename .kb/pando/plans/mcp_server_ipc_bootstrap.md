---
created_at: 2026-09-11T22:20:23.371480205Z
updated_at: 2026-09-11T22:24:23.582093595Z
---
# Plan: put `pando mcp-server` (and other direct-DB entry points) on the IPC primary/secondary bootstrap

Date: 2026-09-11. Status: **P0 (failover correctness, G1–G6) IMPLEMENTED 2026-09-11**, see [[pando/fixes/ipc_failover_p0_inplace_promotion.md]]. **P1 (shared `wireIPC`, `BootstrapWithOptions`, `ModeMCP`) IMPLEMENTED 2026-09-11**, see [[pando/changes/ipc_wiring_p1_shared_wireipc.md]]. **P2 (mcp-server on the bootstrap) IMPLEMENTED 2026-09-11**, see [[pando/changes/mcp_server_ipc_bootstrap_p2.md]]. **P3 (role-aware, primary-only background services) IMPLEMENTED 2026-09-11**, see [[pando/changes/ipc_role_aware_services_p3.md]]. **P4 (other entry points: agui-serve, one-shot `cronjob run`, `cronjob.reload`, `kb.relink`, `db.ConnectCLI`) IMPLEMENTED 2026-09-11**, see [[pando/changes/ipc_other_entrypoints_p4.md]]. **P5 (remaining direct writers on secondaries: history, project, mcpgateway, design, AG-UI threads; plus handover-tolerant forwarding) IMPLEMENTED 2026-09-12**, see [[pando/changes/ipc_direct_writers_p5.md]]. P6 and G7 not started. Author: Claude (analysis task, point 1 of 3).
Builds on: [[pando/analysis/sqlite_locked_interrupted_errors.md]], [[pando/plans/unified_single_writer_master_plan.md]], [[pando/plans/unified_single_writer_phase1_bootstrap.md]], [[pando/plans/unified_single_writer_phase3_serialisation.md]], [[pando/plans/unified_single_writer_phase5_failover.md]], [[pando/plans/inter_instance_phase4_completed.md]], [[pando/plans/inter_instance_ipc_plan.md]], [[pando/analysis/remembrances-single-writer-proxy-gap-2026-05-27.md]], [[pando/plans/remembrances_ipc_proxy_implementation_plan.md]], [[pando/fixes/sqlite-connection-pool-exhaustion-mcp-server.md]].

Target scenario: TUI/desktop/ACP plus one or more `pando mcp-server --no-http` processes (started by Claude Code, Copilot, Cursor...) in the same project, all sharing `.pando/data/pando.db`. Any of them may start first. mcp-server processes come and go, so handover and failover are on the normal path, not rare edge cases.

---

## 1. How TUI/ACP/desktop bootstrap IPC today (evidence)

### 1.1 `ipcruntime.Bootstrap` (`internal/ipc/runtime/runtime.go:93-262`)
1. `ipc.PortsForPath(workdir)` (`internal/ipc/ports.go:22`): FNV-32a over the **raw string**. It does not normalise the path (no `Abs`, no `EvalSymlinks`).
2. `ipc.AcquireLock` (`lock_unix.go:21-57`): `flock(LOCK_EX|LOCK_NB)` on `<cwd>/.pando/ipc.lock`. On success it writes `LockInfo{InstanceID, PID, PubPort, RPCPort}` into the lock file. On failure it returns the existing LockInfo.
3. Secondary only: `killStalePrimary` (`runtime.go:106-110, 269-294`) probes `ipc.ping` with a 10 s timeout. If there is no answer it SIGKILLs the PID from the lock file and re-acquires the lock.
4. Primary branch (`runtime.go:119-172`):
   - `db.Connect()`: RW connection, goose migrations, pool of 8 conns, driver-default 60 s busy timeout.
   - `ipc.NewBus`, plus an `ipc.ping` handler.
   - Primary failover watcher, created but **not started**.
5. Secondary branch (`runtime.go:174-261`):
   - `db.ConnectRWSecondary()`: RW connection, 1 conn, `busy_timeout=200`.
   - `ipc.NewClient`.
   - `dbproxy.New(db.New(rwConn), client, rpcAddr)` using the ports from the lock file.
   - Subscribes to the `db.*` changepub topics. Events are only logged (`handleWriteChanges`, `runtime.go:382-401`).
   - Secondary watcher, **started immediately**.
6. `failover.DefaultConfig()` has `Enabled: true` (`watcher.go:41-48`), and `TestDefaultConfig` asserts it. The comments "disabled by default" at `runtime.go:75-77, 228-229` and `root.go:222-223` are stale. The `--auto-failover` flag (`root.go:1128`) therefore has no effect in practice.

### 1.2 Per-entrypoint wiring (the same block is copy-pasted in 5 places)
- **TUI** (`cmd/root.go:214-301`) and **ACP** (`cmd/root.go:603-680`).
  - Primary:
    - `writecoordinator.New(ctx, db.New(conn), 256)`
    - `changepub.NewBusPublisher`
    - `dbproxy.RegisterHandlersWithCoordinator`
    - `registerBridgeHandlers` (`cmd/bridge_delegation.go:49`: bridge, delegation, `instance.ping`, `db.compact`)
    - `pandoApp.SetupIPC(bus)` (`internal/app/app.go:2229-2246`: session IPC publisher + remembrances dispatcher)
    - `bus.Start`
    - `bridge.New(...).Start` (publishes heartbeats, `bridge.go:175`)
    - `rt.Watcher.Start`
  - Secondary: `SetIPCSecondaryContext(...)` with a `busSetupFunc` closure, plus `rt.Watcher.SetPromoteCallback(pandoApp.PromoteToPrimary)`.
- **serve** (`cmd/serve.go:104-178`), **desktop** (`cmd/desktop.go:111-172`) and **app** (`cmd/app.go:95-172`).
  - Primary: same wiring, without `Watcher.Start`. The bridge still sends heartbeats.
  - Secondary: **no branch at all**. No `SetIPCSecondaryContext`, no promote callback (see gap G3).
- The instance registry announces with `ModeTUI/ModeACP/ModeDesktop/...` (`internal/instanceregistry/entry.go:10-20`). There is no MCP mode.

### 1.3 How writes are dispatched
- **sqlc `db.Querier`** goes through `DBProxy.directOrProxy` (`internal/ipc/dbproxy/proxy.go:233-277`). The secondary writes directly first. Only on BUSY/LOCKED does it send `db.write` to the primary.
- **Remembrances.** When a proxy is set, the stores **always** proxy:
  - `kb.go:190/219/444/553`, `backfill.go:46/58`, `memory.go:167`
  - `events.go:60/156`
  - `code/indexer.go:127/421/704`

  The writes arrive at `internal/rag/proxy/dispatcher.go`, which handles 11 methods, and run inside the single writecoordinator goroutine on the primary.
- **Direct writers that skip the proxy on secondaries:**
  - `history.NewService(rawQ, conn)` (`app.go:247`)
  - `project.NewService(rawQ)` (`app.go:251`)
  - `mcpgateway.NewGateway(conn)` (`app.go:566`)
  - `design.NewProvider(conn)` (`app.go:716`)

  (All fixed by P5, together with the AG-UI thread map that P4 later found.)

### 1.4 Background services disabled on secondaries: none
`app.New` starts the same things for every role:
- Startup code index plus fsnotify watcher (`internal/app/remembrances_code.go:16-90`). Only `StartupMode=="cronjob"` skips it.
- KB mirror / auto-import (`remembrances.go:75+`).
- KB link backfill (`remembrances.go:43-73`).
- Session indexer (`remembrances_indexer.go:21-79`).
- Memory GC (`app.go:457-476`).
- `CronService.Start` (`app.go:622-625`). ACP starts it again at `root.go:693`.
- MCP gateway init, project `SeedFromGlobal`.

On secondaries these are "safe" only because their writes are proxied. The cost:
- Every process reindexes the same file on every fsnotify event (N× embedding plus N× coordinator jobs).
- Every cron job fires once per process.

## 2. What mcp-server does today and why it was left out
- `cmd/mcp_server.go:99-124` does the following, and never takes the lock, starts a bus, or joins the registry:
  - `config.Load`. With no `--log-file`, slog goes to the in-memory `logging.NewWriter()` (`internal/config/config.go:1950-1956`), so stdout stays clean.
  - `db.Connect()`: RW connection plus migrations.
  - `app.New(ctx, conn, {SkipLSP, SkipMesnadaServer, StartupMode:"mcp"})` without `DBQuerier`. As a result:
    - the remembrances proxy is nil, so all KB/events/code writes are direct;
    - it runs its own code watcher, KB sync, backfill, session indexer, GC and cron.
- It is therefore an independent writer that nobody can see: `pando ipc status` does not show it, and the primary cannot coordinate with it.
- The stdio transport writes JSON-RPC to `os.Stdout` (`internal/mesnada/server/server.go:194`).
- On the `--no-http` path it returns `stdioSrv.Start()` with no signal handling. SIGTERM kills the process without running defers.
- **Why it was excluded.** History shows scope drift, not a decision:
  - The mcp-server skeleton landed on 2026-04-26 (`7fcdabc7`), before IPC Phase 4 (2026-05-06, TUI only).
  - `runtime.Bootstrap` and the entrypoint unification landed in `93385bc9` (2026-05-18). That commit touched only `cmd/{app,desktop,ipc,root,serve}.go`.
  - The master plan's entrypoint table lists only root/serve/app/desktop/acp.
  - No code comment or KB doc justifies the exclusion.
  - Later mcp-server work (memory tools, context7, the pool-cap fix `96c95b4b`) never revisited it.
  - Implicit reasons that were never written down: stdio cleanliness, startup latency, and treating mcp-server as a "tool server" rather than a session instance.

## 3. Status of earlier plans (the KB docs are stale)
- `inter_instance_phase4_completed`: done. The RO secondary was later replaced by direct-then-proxy on a RW 200 ms connection.
- `unified_single_writer_*` phase docs all say "not started". The code actually shows:
  - **P1 bootstrap:** done for 5 entrypoints. Missing for mcp-server, cronjob, agui-serve and the CLIs.
  - **P2 write contract:** done (WriteMeta, WriteTimeouts 5 s/30 s, WriteError/IsRetryable, 3-try backoff).
  - **P3 writecoordinator:** done (a single goroutine, so head-of-line blocking; see the analysis doc).
  - **P4 changepub:** done. Secondaries only log the events.
  - **P5 failover:** partial and **broken** (G1-G5 below). Enabled by default. Wired only in TUI/ACP. (G1–G6 fixed by this plan's P0 on 2026-09-11.)
  - **P6 observability:** partial (`pando ipc status`, role logs).
  - **P7 multi-process tests:** not done.
- `remembrances_ipc_proxy_implementation_plan` ("Ready for execution"): **implemented**.
  - `rag/proxy/dispatcher.go` handles 11 methods, more than planned: ReplaceSessionEvents, CodeDeleteFile and CodeUpdateLanguageStats were added.
  - `SetWriteProxy` exists on all 3 stores.
  - The dispatcher is registered in `SetupIPC`.
  - The remembrances gap analysis is resolved, except for the direct writers in §1.3.

## 4. Failover gaps that must be fixed first
Once mcp-server joins the topology, an mcp-server primary exiting triggers promotion inside TUI/desktop. Today an mcp-server exit affects nobody, so without these fixes the change makes things **worse**.

- **G1 — PromoteToPrimary cannot write** (`app.go:2372-2431`).
  - It closes `ipcROConn`, which is `rt.SQLDB`: the connection under the DBProxy's local querier *and* under the KB/events/code stores. It also closes `ipcClient`.
  - It then replaces `app.DBQuerier`. But `session.NewService(q)` and `message.NewService(q)` captured the old proxy at construction (`app.go:245-246`), and the stores keep the old proxy.
  - Result after promotion:
    - session/message writes fail with "sql: database is closed". That is not a lock error, so there is no fallback.
    - Remembrances writes call a closed client.
    - history/project/gateway/design use a closed connection.
- **G2 — promotion does not clean up.** It never re-announces to the registry, never keeps a reference to the lock file or bus for shutdown, and never starts the primary-only services.
- **G3 — zombie lock on serve/desktop/app secondaries.**
  - Their watcher is enabled with `onPromote == nil`. `triggerFailover` takes the flock, and with a nil callback declares "promotion complete" (`watcher.go:404-421`) without opening RW or binding the bus.
  - `AcquireLock` has already written this process's PID into the lock file.
  - The next instance to start probes a ROUTER that does not exist, waits 10 s, then **SIGKILLs the desktop app**.
- **G4 — the watcher stops after one attempt.** `runSecondary` returns after any `triggerFailover` (`watcher.go:298, 311, 331`): a lost race, a failed promote, or disabled. After that, a second primary death goes unnoticed.
- **G5 — graceful-handover race (key for "mcp-server primary exits").**
  - `app.Shutdown()` → `IPCBus.Shutdown()` publishes `instance.shutdown` (`bus.go:98-107`). `rt.Cleanup` releases the flock only later, because its defer runs after `app.Shutdown`, which itself can take seconds (extensions 5 s, agentvcs 10 s...).
  - Secondaries get the event and make a single `AcquireLock` attempt. It fails because the lock is still held, they log "another secondary promoted first", and their watchers exit (G4).
  - End state: **no primary**. Direct sqlc writes keep working, but every remembrances write, which always proxies, fails.
  - With SIGKILL/SIGTERM (no defers) the kernel releases the lock, so the only cost is a 15 s heartbeat gap.
- **G6 — relative or symlinked `--cwd`.** Different path strings produce different ports. A promoted instance binds `PortsForPath(its own string)`, while the other secondaries' `DBProxy.rpcAddr` is fixed to the old ports. `instance.promoted` carries the new addresses, but nobody re-points the proxy.
- **G7 — killStalePrimary.** An mcp-server spawned by Claude Code can SIGKILL a TUI that is SIGSTOPped or under a debugger. `handleRPC` runs one goroutine per request (`bus.go:208`), so a busy coordinator does not trigger this; only a truly suspended process does.

## 5. Proposed design

### 5.1 Extract one wiring helper (`cmd/ipc_wiring.go`)
```go
// wireIPC performs the post-app.New half of the IPC bootstrap for every
// long-lived entrypoint and returns a cleanup func (registry revoke, coordinator).
func wireIPC(ctx context.Context, rt *ipcruntime.BootstrapResult, pandoApp *app.App,
    instanceID, cwd string, mode instanceregistry.Mode, opts wireOptions) func()
```
- Registry: Announce/Revoke.
- Primary: coordinator + changepub + `RegisterHandlersWithCoordinator` + `registerBridgeHandlers(bus, id, app, opts.AcceptDelegations)` + `SetupIPC` + `bus.Start` + `bridge.Start` + `rt.Watcher.Start`.
- Secondary: `busSetupFunc` + `SetIPCSecondaryContext` + `SetPromoteCallback(pandoApp.PromoteToPrimary)`.
- Migrate root TUI, ACP, serve, desktop and app to it. This fixes the secondary wiring of serve/desktop/app.
- Add `instanceregistry.ModeMCP = "mcp"`.

### 5.2 `Bootstrap` options
`BootstrapWithOptions(ctx, workdir, id, Options{ProbeTimeout, AllowKillStalePrimary})`:
- Canonicalise `workdir` (`filepath.Abs` + `EvalSymlinks`) before `PortsForPath` and `AcquireLock` (fixes G6).
- mcp-server uses `ProbeTimeout: 3s` and `AllowKillStalePrimary: false`. If the primary is unresponsive, run as a degraded secondary (direct-first writes, remembrances writes fail loudly) and never kill the user's TUI/desktop.
- Other entrypoints keep today's defaults.

### 5.3 mcp-server flow (`cmd/mcp_server.go`)
1. `config.Load`, then the flag overrides.
2. Set `cwd` from `os.Getwd()` after `Chdir`, so it is always absolute.
3. `instanceID := uuid.New()`.
4. `rt := BootstrapWithOptions(...)`, then `defer rt.Cleanup()`.
5. `app.New(ctx, rt.SQLDB, {SkipLSP, SkipMesnadaServer, StartupMode:"mcp", DBQuerier: rt.Querier, IPCRole: rt.Role})`.
6. `wireIPC(..., ModeMCP, {AcceptDelegations:false})`. The process is ephemeral (it dies with the client agent), so it must not accept peer delegations.
7. `buildMCPServerTools` is unchanged. On a secondary, KB/code/memory tools read locally and write through the proxy transparently.
8. Add signal handling on the stdio path as well: SIGINT/SIGTERM or stdin EOF, then the ordered shutdown in §5.5.
- **Stdout stays clean.**
  - Bootstrap and app log through slog to the in-memory writer or `--log-file`.
  - `bus.go` and `bridge.go` use `log.Printf`, which goes to stderr (allowed for stdio MCP).
  - The only stdout writer remains the mesnada stdio encoder.
  - Add a regression test that runs Bootstrap and `wireIPC` with stdout redirected to a pipe and asserts zero bytes.
- **Startup latency.**
  - Healthy topology: the lock takes milliseconds, and a secondary adds one ping round-trip plus opening a 1-conn DB. A primary pays the migrations it already pays today.
  - Worst case, once the no-kill option exists: about 3 s.
  - `app.New` stays the dominant cost, unchanged.
  - A secondary skips the startup indexer, KB import and backfill (§5.4), so startup gets **lighter** than today.
  - Optional later step: answer MCP `initialize` before `app.New` finishes.

### 5.4 Role-aware background services
Add `AppOptions.IPCRole`, falling back to "secondary when DBQuerier is a remote *DBProxy". Move the primary-only services into `app.startPrimaryServices(ctx)`:
- startup code index + fsnotify watcher
- KB mirror / auto-import
- KB link backfill
- memory GC
- CronService.Start (also remove the second start at `root.go:693`)

Call it from `New` when primary and from `PromoteToPrimary`. Because the lock is per cwd, the primary's watcher covers the same tree. That gives exactly one indexer/watcher per project, which removes the N× reindex behind cause 3 in the analysis.

The session indexer stays per process, because it only sees its own message broker. Its writes proxy. The follow-up is to make it incremental (analysis cause 2).

### 5.5 Failover fixes (prerequisite, P0)
- **In-place promotion** instead of swapping objects:
  - Keep `rt.SQLDB`. It is already a RW connection.
  - On promotion:
    - run migrations
    - `SetMaxOpenConns(8)`
    - set busy_timeout. Better: move pragmas into the DSN `_pragma=` so every pooled connection gets them (analysis fix 3).
  - Add `DBProxy.Promote()`, which atomically clears `client` so the proxy becomes a passthrough. Stores check `proxy.IsRemote()` instead of `proxy != nil`.
  - Session/message services, stores, history and gateway keep valid references (fixes G1).
  - `PromoteToPrimary` then:
    - stores `lockFile` and `bus` for Shutdown
    - re-announces with `IsPrimary=true`
    - calls `startPrimaryServices`
    - publishes `instance.promoted`
- **Watcher:**
  - Never take the lock when `onPromote == nil` (fixes G3).
  - Loop instead of returning after a lost race or failed promote (fixes G4).
  - After `instance.shutdown`, retry `AcquireLock` for about 5 s with jitter (fixes G5).
- **Ordered graceful shutdown on the primary:**
  1. Stop accepting new `db.write` and drain the coordinator.
  2. `ReleaseLock`.
  3. Publish `instance.shutdown`.
  4. Close the bus.
  5. Run the rest of `app.Shutdown`.

  Implement this in `rt.Cleanup` / `App.Shutdown` ordering (fixes G5).
- **Lifecycle when mcp-server is primary and exits:**
  1. Stdin EOF or SIGTERM triggers the ordered shutdown.
  2. TUI/desktop secondaries receive `instance.shutdown`, and exactly one of them wins the flock.
  3. The winner promotes in place and starts the primary services. Because workdirs are canonicalised, it binds the same deterministic ports, so the other secondaries' proxies keep working.
  - On SIGKILL: the kernel frees the flock, and the same flow runs after the 15 s heartbeat timeout. During that gap remembrances writes fail after 3 retries.
  - Optional: have `WriteWithRetry` wait up to about 20 s on "unavailable" when the lock file shows a new PID. (Implemented by P5.)
- **Lifecycle when the primary dies while mcp-server is a secondary.** mcp-server can win the promotion and become primary. With §5.4 it then runs the indexers. The same rules apply.

## 6. Other direct-DB entry points
| Entry point | File | Lifetime | Recommendation |
|---|---|---|---|
| `agui-serve` | `cmd/agui_serve.go:115-125` | long-running server, full `app.New` | **Must** use Bootstrap + `wireIPC` (a peer of `serve`). High priority. |
| `cronjob run` | `cmd/cronjob.go:170-181` | short, but full `app.New` (KB sync/backfill/GC/cron start) and spawns Mesnada tasks; fired by the OS scheduler while the TUI runs | Bootstrap (normally a secondary) plus a "oneshot" role that starts no background services. Medium. |
| `kb relink [--force]` | `cmd/kb.go:46` | short, but bulk `kb_links` rewrites in long transactions | Forward to a running primary via a new `kb.relink` RPC (pattern: `compactViaRunningInstance`, `cmd/db.go:110-146`), else run locally. Medium. |
| `db compact` | `cmd/db.go:72` | short | Already forwards to a live primary. Fine. |
| `project list/add/remove/init/status` | `cmd/project.go:257` | tiny writes | Fine as short-lived, but `db.Connect` runs goose migrations from a possibly different binary. Add `db.ConnectCLI()`: no migrations when the DB exists, 5 s busy timeout. Low. |
| `design *` | `cmd/design.go:646` | short (`canvas`/`open` may stay up) | `ConnectCLI`. Low. |

`PromoteToPrimary` itself (`app.go:2389`) uses `db.Connect`. Replace it with in-place promotion (§5.5).

## 7. Phased implementation
- **P0 — failover correctness (hard prerequisite).** **IMPLEMENTED 2026-09-11**, see [[pando/fixes/ipc_failover_p0_inplace_promotion.md]].
  - `internal/ipc/failover/watcher.go`: nil-callback guard, monitoring loop, retry after shutdown.
  - `internal/ipc/dbproxy/proxy.go`: `Promote()`, `IsRemote()`.
  - `internal/rag/{kb,events,code}`: switch the proxy checks to `IsRemote()`.
  - `internal/app/app.go`: in-place `PromoteToPrimary`, Shutdown ordering, keep lock/bus.
  - `internal/ipc/runtime/runtime.go`: Cleanup order, path canonicalisation, fix stale comments.
  - `internal/db/connect.go`: DSN pragmas, promote helper.
  - Deviations from this sketch, with the reasons in the fix doc:
    - The busy_timeout switch uses an atomic per-pool init state and reconfigures every pooled connection by checking out `MaxOpenConnections` connections. DSN `_pragma=` was rejected because it cannot change a live pool.
    - Stores use `DBProxy.Forward`, which is safe when a promotion races the call.
    - The lock file is truncated, not unlinked, on release (this removes a two-primaries unlink race).
    - The secondary watcher and the promotion bind the lock-file ports, with a bind retry.
    - `startPrimaryServices` is only a hook (P3).
- **P1 — `cmd/ipc_wiring.go` `wireIPC`.** **IMPLEMENTED 2026-09-11**, see [[pando/changes/ipc_wiring_p1_shared_wireipc.md]].
  - Migrated `cmd/root.go` (TUI + ACP), `serve.go`, `desktop.go`, `app.go` to `wireIPC`. As a side effect, serve/desktop/app primaries now also get `SetupIPC` (the remembrances dispatcher was previously never registered there), `changepub` (app.go was silently missing it), and `rt.Watcher.Start` — not just the secondary branch and ordered handover the plan asked for.
  - `BootstrapWithOptions(ctx, workdir, id, Options{ProbeTimeout, AllowKillStalePrimary})`; `Bootstrap` now calls it with `DefaultOptions()` (10 s, kill allowed).
  - `instanceregistry.ModeMCP`.
  - `registerBridgeHandlers` gains an `acceptOverride *bool` parameter, resolved by the new `resolveAcceptDelegations`.
  - Also fixed, since it blocked validating the ordered handover end-to-end: ACP had no SIGINT/SIGTERM handler at all (`signal.NotifyContext` added around the ACP main `ctx`).
  - Deviations from the sketch, with reasons in the change doc:
    - `wireIPC`'s secondary branch does not call `rt.Watcher.Start` again — `ipcruntime.Bootstrap` already starts the secondary watcher unconditionally (P0), and starting it twice would double-close its `done` channel.
    - The primary and promotion wiring share one `primaryBusSetupFunc` (an `app.IPCBusSetupFunc`) instead of two separate blocks, so bridge heartbeats now start as soon as handlers are registered in both cases (previously TUI/ACP's own bootstrap-time wiring started the bridge only after `bus.Start` succeeded).
  - Validated with a two-process smoke test: `pando serve` (primary) SIGTERM'd, `pando acp` (secondary) promoted in 0.115 s, rebound serve's ports, and a `session/new` call after promotion wrote a real row — the serve entrypoint specifically, which had no working secondary/handover path before P1.
- **P2 — mcp-server on the bootstrap.** **IMPLEMENTED 2026-09-11**, see [[pando/changes/mcp_server_ipc_bootstrap_p2.md]].
  - `cmd/mcp_server.go`: absolute `cwd` (`os.Chdir` then always `os.Getwd()`), a new `bootstrapMCPServer(ctx, cwd)` helper wrapping `ipcruntime.BootstrapWithOptions(..., Options{ProbeTimeout: 3s, AllowKillStalePrimary: false})` + `app.New(..., DBQuerier: rt.Querier)` + `wireIPC(..., instanceregistry.ModeMCP, wireOptions{AcceptDelegations: &false})`, replacing the old `db.Connect()` path.
  - Ordered shutdown via a single `shutdownMCPServerOrdered` closure (stop transports → `pandoApp.Shutdown()` → registry revoke → `rt.Cleanup()`), triggered uniformly by SIGINT/SIGTERM, stdin EOF on the stdio transport, or an HTTP failure — unifying what were three separate branches (`--no-http`, `--no-stdio`, both) into one `waitForMCPServerShutdown` select, and fixing the `--no-http` path's complete lack of signal handling (previously a bare `return stdioSrv.Start()`).
  - Deviations from this sketch, with reasons in the change doc:
    - No `AppOptions.IPCRole` field existed yet at P2 time (added by P3); mcp-server passed only `DBQuerier`, matching every other migrated entrypoint at the time.
    - `buildMCPServerTools` needed no changes: the KB/events/code stores already pick `Forward`/`IsRemote` correctly per P0, so remembrances tool writes (`kb_add_document`, `remember`, `save_event`, code index tools, `kb_delete_document`, `forget`) work unchanged on a secondary.
  - Verified with an isolated three-scenario smoke test (mcp-server primary→ACP secondary EOF-promotes; ACP primary→mcp-server secondary proxied delete + stdout purity; SIGTERM of the primary while mcp-server is secondary). See the change doc for exact latencies.
- **P3 — role-aware services.** **IMPLEMENTED 2026-09-11**, see [[pando/changes/ipc_role_aware_services_p3.md]].
  - `AppOptions.IPCRole ipcruntime.Role`, since no import cycle exists. If it is unset, a remote `*DBProxy` means secondary; anything else means primary.
  - Every IPC entrypoint passes `rt.Role`:
    - TUI and ACP in `root.go`, and `bootstrapMCPServer`, pass it to `app.New`.
    - serve, desktop and app pass it through `api.ServerConfig.Role`, which `api.NewServer` forwards.
  - New `internal/app/primary_services.go`:
    - `New` registers each primary-only service instead of starting it.
    - `startPrimaryServices(trigger)` is idempotent: it runs at most once per App and is refused once Shutdown has begun.
    - It runs on the app's lifetime ctx and is called from `New` when primary and from `PromoteToPrimary`.
    - `Shutdown` calls `closePrimaryServices` before cancelling watchers or waiting on the WaitGroup.
    - Each role logs one Info line saying it started or skipped the services.
  - Gated services: startup code index + fsnotify watcher, KB auto-import, KB watch, KB link backfill, memory GC, cron, and the one-off enrichment-session cleanup (previously it never ran on a promoted primary).
  - Kept per process:
    - KB mirror/converter configuration.
    - The code-project-id resolution the enricher reads.
    - The session indexer.
    - MCP gateway init, which builds this process's own tool registry.
    - `SeedFromGlobal`: an idempotent one-shot, and a direct writer that belongs to P5.
  - ACP's duplicate `CronService.Start` was removed, so cron starts once, only on the primary.
  - `internal/cronjob` changes:
    - An unstarted service lists and runs jobs from the live config (secondary UX).
    - `Reload` before `Start` no longer pre-schedules entries (it would double-fire after a promotion).
    - `Stop` removes entries.
    - A pre-existing `reloadCh` data race is fixed.
  - Deviation: cron edits made through a *secondary's* WebUI reach a non-TUI primary only after a restart or a promotion. Cron lives in the config file, not the DB, and only the TUI watches that file. The follow-up is a `cronjob.reload` RPC, documented in the change doc.
  - Verified:
    - Unit tests (`-race`), including a real in-place promotion.
    - An isolated three-process smoke test: acp primary, plus acp and mcp-server secondaries. The secondaries started nothing. After a graceful handover the winner (acp in one run, mcp-server in the other) started the services exactly once with `trigger=promotion`, 0.215 s after the handover.
- **P4 — other entry points.** **IMPLEMENTED 2026-09-11**, see [[pando/changes/ipc_other_entrypoints_p4.md]].
  - `agui-serve` → `ipcruntime.Bootstrap` (serve's default options) + `app.New(DBQuerier, IPCRole)` + `wireIPC(..., instanceregistry.ModeAGUI)`. A new shared helper, `shutdownEntrypointOrdered`, runs the ordered shutdown on SIGINT/SIGTERM, with a 6 s watchdog as in serve. `--cwd` is now resolved to an absolute path.
  - `cronjob run` → `BootstrapWithOptions(ProbeTimeout: 3s, AllowKillStalePrimary: false)`, then `wireIPC(..., ModeCronJob, {AcceptDelegations: false, OneShot: true})`. Two new knobs:
    - `AppOptions.OneShot`: no primary-only services, even as primary.
    - `wireOptions.OneShot`: a secondary is never armed for promotion.
  - `cronjob.reload` RPC (resolves the P3 deviation above). All three cron REST handlers call `Server.reloadCronJobsEverywhere`, which reloads locally and then, on a secondary, sends a best-effort `App.ForwardCronJobsToPrimary`. The primary's handler is registered in `primaryBusSetupFunc`, so a promoted instance gets it too. It calls `config.SetCronJobsInMemory` (lock-aware; no file write, no `config.Reload()`) and then `CronService.Reload`.
  - `kb.relink` RPC. `pando kb relink [--force]` forwards through the new `dialRunningPrimary` helper, which `db compact` now uses too, and falls back to a local run. On the primary, `App.RelinkKB` uses the KB store's own batched IMMEDIATE-transaction path, not the single-goroutine writecoordinator, to avoid head-of-line blocking.
  - `db.ConnectCLI()`: no migrations when the DB exists, 5 s busy timeout, a pool of 4, and the same DSN and pragmas; a missing or empty DB falls back to `Connect`. Used by `project`, `design`, and the local fallbacks of `kb relink` and `db compact`. After P4 the only `db.Connect()` caller left is the bootstrap's own primary branch.
  - Also: registry modes `ModeAGUI`/`ModeCronJob`, and nil-DB guards in the two P4 entrypoints.
  - Verified with `-race` unit tests (including a real-bus `cronjob.reload` and `kb.relink` forward, and a one-shot secondary that is never promoted) and an isolated smoke test with 45/45 checks passing. That smoke found a pre-existing bug: `updateConfigFileAt` rewrites the whole project config, blanking `Data.Directory` (see the change doc).
- **P5 — remaining direct writers on secondaries.** **IMPLEMENTED 2026-09-12**, see [[pando/changes/ipc_direct_writers_p5.md]]. (The analysis items listed here — `BEGIN IMMEDIATE`, the `json_extract(metadata,'$.session_id')` index, the incremental session indexer, retrying lock errors in `WriteError.IsRetryable` — were already done earlier, see [[pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md]].)
  - Every writer was **routed through the proxy** (option 1); none was left as "direct-first plus retry".
  - `history.NewService(q db.Querier)` and `project.NewService(q db.Querier)` now take the app's querier (the `*DBProxy` on a secondary). History's `WithTx` wrapped a single INSERT, so it was dropped for the plain `CreateFile` write; the rename needed a new `DBProxy.UpdateProjectName` plus a `dispatchWrite` case, since it is not in the generated `db.Querier`.
  - The writers that hold a raw `*sql.DB` (design store, MCP gateway registry/stats, AG-UI thread map) use a new `dbproxy.SQLWriter` over **registered named statements** (`dbproxy.RegisterStatement` + the `ExecStatements` write method): only a name and typed arguments cross IPC, and a batch runs in one transaction on whichever side executes it, which is how design's `AddVersion`/`ReplaceNodes` keep their atomicity across the proxy. An older primary answers "unknown write method", and the secondary falls back to a bounded direct retry.
  - **Handover tolerance** (the §5.5 optional item and the P0 risk): forwarded writes now wait out a primary handover — `forwardWithHandoverRetry`, ~20 s bounded, re-reading the lock file between retries — instead of failing after 3 short retries. A drained/shut-down coordinator maps to the new retryable `ErrCodeUnavailable`, and an ambiguous outcome (timeout / lost response) is still only re-sent for void writes.
- **P6 — tests and docs.** See §8. Update the stale statuses in the `unified_single_writer_*` KB docs.

## 8. Test plan
- `internal/ipc/failover/watcher_test.go`:
  - nil `onPromote` never holds the lock
  - the watcher keeps monitoring after a lost race
  - retry-acquire after `instance.shutdown`
  - update `TestDefaultConfig` if the default changes
- `internal/ipc/runtime`: canonicalised ports (relative vs absolute vs symlink give the same ports), `AllowKillStalePrimary=false` never kills, stdout-clean.
- `internal/ipc/dbproxy`: `Promote()` flips to passthrough; concurrent writes during a promote.
- `internal/app`:
  - PromoteToPrimary regression on a temp DB. Session/message/KB/event writes must succeed after promotion; today they fail (G1).
  - A secondary starts no watcher/indexer/cron (via hooks or counters).
- `cmd/root_test.go`: the source-shape tests (`TestRunACPServerWithOptions_ConfiguresSecondaryIPCFailoverPath`, lines 183-200) move to `wireIPC`. Add the same kind of test for `mcp_server.go`.
- Multi-process (Phase 7, never done), as Python under `tests/` per project convention:
  - build the binary
  - start `pando mcp-server --no-http` (send `initialize`), then a `pando -p` secondary and the TUI/serve
  - assert the lock-file PID and `pando ipc status`
  - stop the mcp-server primary via stdin EOF, SIGTERM and SIGKILL; assert promotion within 20 s and that KB and session writes succeed
  - run 3 concurrent mcp-servers
- Commands: `go test ./internal/ipc/... ./internal/app ./cmd ./internal/rag/...`.

## 9. Open risks
- Failover is now a hot path, and every fix touches role state shared across goroutines. Use atomics and the `-race` detector.
- Version skew: a globally installed mcp-server binary vs the TUI binary.
  - Unknown dispatcher methods return `ErrMethodNotFound`.
  - Migrations are run by whoever is primary.
  - Mitigation: `ipc.ping` returns version and schema; secondaries warn or refuse to promote across major skew.
- Head-of-line blocking in the single writecoordinator grows as more instances proxy through it (CodeIndexFile runs embedding HTTP calls inline). See the analysis.
- During a 15 s heartbeat gap after a SIGKILL, MCP tool writes (`kb_add_document`, `remember`) return errors to the client agent. (P5's handover wait covers most of this gap: a forwarded write now waits up to ~20 s for the new primary.)
- Keeping SIGKILL of a suspended primary for the other entrypoints is still a policy decision.
