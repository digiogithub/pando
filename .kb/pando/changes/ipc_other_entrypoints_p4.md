---
created_at: 2026-09-11T21:50:28.697022716Z
updated_at: 2026-09-11T21:50:28.697022716Z
tags:
    - change
    - ipc
    - cron
    - kb
    - sqlite
---
# Change: P4 — other entry points on the IPC bootstrap (agui-serve, cronjob run, cronjob.reload, kb.relink, db.ConnectCLI) (2026-09-11)

Implements phase **P4** of [[pando/plans/mcp_server_ipc_bootstrap.md]] (§6, §7 P4, §8). Builds on:
- [[pando/fixes/ipc_failover_p0_inplace_promotion.md]]: in-place promotion, the nil-callback watcher guard, ordered handover.
- [[pando/changes/ipc_wiring_p1_shared_wireipc.md]]: `wireIPC`, `BootstrapWithOptions`, `primaryBusSetupFunc`.
- [[pando/changes/mcp_server_ipc_bootstrap_p2.md]]: mcp-server as the reference non-TUI entrypoint.
- [[pando/changes/ipc_role_aware_services_p3.md]]: `AppOptions.IPCRole`, `startPrimaryServices`. This change also resolves P3's cron cross-process follow-up.

P5 (the remaining direct writers) and P6 (the multi-process suite) are not started.

## What changed

### 1. `pando agui-serve` joins the bootstrap (`cmd/agui_serve.go`)
- It is a long-running peer of `serve`. The old path was `db.Connect()` + `app.New(conn, {StartupMode:"agui"})`. The new path:
  1. `ipcruntime.Bootstrap` with serve's default options.
  2. `app.New(..., DBQuerier: rt.Querier, IPCRole: rt.Role)`.
  3. `wireIPC(..., instanceregistry.ModeAGUI, wireOptions{})`.
- `AcceptDelegations` follows the config, as for serve.
- **Ordered shutdown.** One `defer shutdownEntrypointOrdered(pandoApp.Shutdown, unwireIPC, rt.Cleanup)`, registered before the AG-UI runtime's `defer runtime.Close()`. On SIGINT/SIGTERM the steps run in this order:
  1. The listener stops.
  2. The runtime closes.
  3. `App.Shutdown` hands over the IPC role (drain, lock release, `instance.shutdown`).
  4. The registry entry is revoked.
  5. `rt.Cleanup` runs.
- A 6 s force-exit watchdog was added, the same as serve's. The IPC handover is the first thing `Shutdown` does, so only slower teardown can be cut.
- `resolveWorkingDir` now always returns `os.Getwd()` after the chdir, so the directory is absolute. Before, a relative `--cwd` string reached the lock, ports and registry as typed.
- Guard: if Bootstrap ends up with no DB (a secondary whose DB failed to open), the command returns an error instead of letting `app.New` panic on the nil pool. See the risks section.

### 2. `pando cronjob run` as a one-shot process (`cmd/cronjob.go`)
`bootstrapCronJobRun(ctx, cwd)` replaces `db.Connect()`:
- **`ipcruntime.BootstrapWithOptions(..., Options{ProbeTimeout: 3s, AllowKillStalePrimary: false})`.** An unattended OS-scheduler run must never kill the user's instance because it answered one probe slowly. If the primary is unresponsive, the run continues as a degraded secondary.
- **`app.New(..., DBQuerier, IPCRole, OneShot: true)`.** This is the new knob (see below).
- **`wireIPC(..., instanceregistry.ModeCronJob, wireOptions{AcceptDelegations: &false, OneShot: true})`.**
- **Shutdown.** SIGINT/SIGTERM go through `signal.NotifyContext`. The run ends with the single `defer shutdownEntrypointOrdered(a.Shutdown, unwireIPC, rt.Cleanup)`.
- **Same nil-DB guard** as agui-serve.

### 3. The one-shot knob (small, documented in both places)
**`app.AppOptions.OneShot bool`** (→ `App.oneShot`):
- `applyStartupIPCRole` logs `IPC role: one-shot process, primary-only background services disabled` (with `role`, `startup_mode`, `services`) and starts nothing, for either role.
- `startPrimaryServices` returns false for any trigger. This also covers a promotion, even though a one-shot process is never armed for one.
- Effect: `cronjob run` never starts the code watcher, the KB sync/watch/backfill, memory GC or the cron scheduler, **even when it ends up primary**. Before P4 it ran all of them except the code indexer. That skip, based on `StartupMode=="cronjob"`, is unchanged.

**`cmd` `wireOptions.OneShot bool`** (secondary side):
- `wireSecondary` still calls `SetIPCSecondaryContext`, but **does not** call `SetPromoteCallback`, and logs `IPC: one-shot secondary, failover promotion not armed`.
- The secondary watcher keeps running with no callback. P0's G3 guard means it then never takes the lock, so the one-shot run can neither become primary nor leave a zombie lock behind.
- Why no promotion: a run that lasts a few seconds would grab the lock only to hand it over again, which delays the long-running instance that should win.

**On the primary side nothing changes.** A one-shot primary (no other instance running) serves the bus, so a secondary started during the run can still forward its writes, and it hands over in order when it exits. Only the background services are off.

### 4. `cronjob.reload` RPC: cron edits from a secondary reach the primary
**Write paths found.** The only cron-config writer is `config.UpdateCronJobs`, called by the three REST handlers in `internal/api/handlers_cronjobs.go` (create, update, delete). Serve, desktop and app share this API server, so it covers the WebUI settings and the cron API. Other surfaces were checked:
- The TUI cron dialog only lists jobs and runs them ("run now"); it has no edit path.
- `pando_setup` has no cron command.
- No other `Update*` touches `CronJobs`.

**Sender side:**
- **`App.ForwardCronJobsToPrimary(ctx, jobs) (forwarded bool, err error)`** (`internal/app/ipc_maintenance.go`). On a secondary it sends `protocol.MethodCronJobReload` with `CronJobReloadParams{CronJobs: json}` (10 s timeout). On a primary, or with no IPC, it returns (false, nil).
- **The three handlers** now call a single `Server.reloadCronJobsEverywhere(ctx)`: first the local `CronService.Reload`, as before, then the best-effort forward.
  - The forward is detached from the request (`context.WithoutCancel`) with a 5 s timeout.
  - A failure is only logged (Warn). The edit is already saved and applies after the primary's next restart or promotion.
  - The forward also runs when the local `CronService` is nil (Mesnada is disabled on the secondary but enabled on the primary).

**Receiver side:**
- **`registerPrimaryMaintenanceHandlers(bus, pandoApp)`** (new `cmd/ipc_primary_rpc.go`) is called from **`primaryBusSetupFunc`**, so a primary at startup and a promoted secondary register the same handler.
- The handler decodes the params (`decodeCronJobReloadParams`) and calls **`App.ApplyCronJobsFromPeer(jobs)`**:
  - **`config.SetCronJobsInMemory`** (new in `internal/config`) validates the jobs and swaps `cfg.CronJobs` in memory: no file write, **no `config.Reload()`**, so in-memory overrides such as mcp-server's flag overrides survive.
  - It then calls `CronService.Reload(jobs)`.
  - It returns `CronJobReloadResult{Jobs, Scheduler}`.
- **Why the in-memory config is updated too, not only the scheduler.** A later cron edit made on the primary's own WebUI builds its new list from `cfg.CronJobs`. A stale copy there would silently drop the secondary's edit.
- **`SetCronJobsInMemory` is lock-aware** (`ErrIfLocked("cronJobs")`). `TestConfigMutatorsAreLockAware` requires every mutator to be either lock-aware or exempted with a reason. Lock-aware is the honest choice: a host-policy overlay that locks cron on the primary must still win over a peer that may run under a different policy or binary.

### 5. `kb.relink` RPC (`cmd/kb.go`, `cmd/ipc_primary_rpc.go`, `internal/app/ipc_maintenance.go`)
**CLI side:**
- `pando kb relink [--force]` → `runKBRelink(ctx, cwd, force)`.
- It first calls `kbRelinkViaRunningInstance`. This follows the `compactViaRunningInstance` pattern: read the lock, a 3 s `instance.ping` probe, `MethodKBRelink` with `KBRelinkParams{Force}`, a 30 min timeout, and a clear message on `ErrMethodNotFound` from an older primary.
- If no live primary answers, it relinks in-process on a `db.ConnectCLI()` pool with a bare `kb.NewKBStore` (as before, minus the migrations).
- Refactor: the lock read, client and probe are now one helper, **`dialRunningPrimary(ctx, workdir) *runningPrimary`**, and `compactViaRunningInstance` (`cmd/db.go`) uses it too.

**Primary handler:** `App.RelinkKB(ctx, force)`.
- It uses `app.Remembrances.KB`. If Remembrances is disabled on the primary, it uses a bare store on `app.rwConn` instead.
- It refuses on an IPC secondary, where the store would silently skip the pass because the proxy is remote.
- It returns an error when the primary has `KBWikiLinks` disabled.

**Execution path chosen: the KB store's own write path, not the writecoordinator.**
- `backfillLinks` already writes in batches of 50 documents per short transaction. Those are IMMEDIATE (`_txlock=immediate`), so the primary's 10 s busy timeout applies.
- That is exactly the path the primary's own `kb-link-backfill` service uses.
- The writecoordinator is a single goroutine. A bulk relink queued on it (minutes for `--force` on a large KB) would head-of-line block every write forwarded by secondaries until it finished.
- The per-batch transactions already serialise with the coordinator's writes through SQLite's write lock, so nothing is gained by routing through it.
- `--force`'s `DELETE FROM kb_links` is one statement, as before.
- The bus runs each RPC in its own goroutine, so a long relink does not block other RPCs.

### 6. `db.ConnectCLI()` (`internal/db/connect.go`)
**`ConnectCLI()` / `ConnectCLIAt(path)`:**
- **The DB file exists:** it does **not** run goose migrations, because a CLI from a possibly different binary must not migrate the schema. It uses `openPool` with the same `_txlock=immediate` DSN (`buildDSN`) and the same per-connection pragmas as the primary (`foreign_keys`, `synchronous`, `cache_size`), a **5 s** busy timeout (`cliBusyTimeout`), and a small pool (4 open, 1 idle).
- **The DB file is missing, or empty** (0 bytes, never migrated): it creates the directory and falls back to `ConnectAt`, which runs the full migrations, since there is no other owner to defer to.

**Callers switched from `db.Connect()`:**
- `cmd/project.go` (`runWithProjectService`)
- `cmd/design.go` (`runWithDesignService`; `canvas`/`open` stay up, and the small pool covers the preview)
- `cmd/kb.go`: the local fallback
- `cmd/db.go`: the `db compact` local fallback. This is a trivial extra fix: the plan had called compact "fine", but it had the same migrate-from-another-binary issue.

**Remaining `db.Connect()` callers after P4:**
- Only `internal/ipc/runtime/runtime.go`, which is the primary bootstrap itself. This is correct.
- A grep of `cmd/` finds no other `db.Connect*`/`sql.Open` caller. `cmd/llm_proxy.go` only announces `ModeProxy` and has no DB.

### 7. Registry modes
- `instanceregistry.ModeAGUI = "agui"` and `ModeCronJob = "cronjob"` were added, because no existing mode fits. `ModeWebUI` would mislabel agui-serve and `ModeNonInteractive` would mislabel cron runs.
- The TUI Instances panel `modeStr` shows them as `AGU` and `CRN`.

## Files and symbols
- **New:**
  - `cmd/ipc_primary_rpc.go`: `registerPrimaryMaintenanceHandlers`, `decodeCronJobReloadParams`, `runningPrimary`, `dialRunningPrimary`, `kbRelinkViaRunningInstance`, `kbRelinkForwardTimeout`.
  - `internal/app/ipc_maintenance.go`: `ForwardCronJobsToPrimary`, `ApplyCronJobsFromPeer`, `RelinkKB`, `cronJobReloadCallTimeout`.
- **`cmd/`:**
  - `cmd/ipc_wiring.go`: `wireOptions.OneShot`, `shutdownEntrypointOrdered`, `primaryBusSetupFunc` (now also registers the maintenance handlers), `wireSecondary` (one-shot skips the promote callback).
  - `cmd/agui_serve.go`: `runAGUIServe`, `resolveWorkingDir`.
  - `cmd/cronjob.go`: `runCronJobRun`, `bootstrapCronJobRun`, `cronJobRunProbeTimeout`.
  - `cmd/kb.go`: `runKBRelink`.
  - `cmd/db.go`: `compactViaRunningInstance` now uses `dialRunningPrimary`; the local fallback uses `ConnectCLI`.
  - `cmd/project.go`, `cmd/design.go`.
- **`internal/`:**
  - `internal/app/app.go`: `AppOptions.OneShot`, `App.oneShot`.
  - `internal/app/primary_services.go`: the one-shot branches in `applyStartupIPCRole` and `startPrimaryServices`.
  - `internal/config/config.go`: `SetCronJobsInMemory`.
  - `internal/api/handlers_cronjobs.go`: `reloadCronJobsEverywhere`, `cronJobPropagateTimeout`.
  - `internal/ipc/protocol/rpc.go`: `MethodCronJobReload`, `MethodKBRelink`, `CronJobReloadParams`, `CronJobReloadResult`, `KBRelinkParams`, `KBRelinkResult`.
  - `internal/db/connect.go`: `ConnectCLI`, `ConnectCLIAt`, `cliBusyTimeout`, `cliMaxOpenConns`, `cliMaxIdleConns`.
  - `internal/instanceregistry/entry.go`: `ModeAGUI`, `ModeCronJob`.
  - `internal/tui/components/instances/view.go`: `modeStr`.
- **Tests:**
  - `internal/db/connect_cli_test.go` (new)
  - `internal/app/ipc_maintenance_test.go` (new)
  - `cmd/ipc_p4_test.go` (new)
  - `cmd/root_test.go`: `TestEntrypointsUseSharedIPCWiring` now also covers `agui_serve.go` and `cronjob.go`.

## Tests
- **`internal/db`:**
  - `TestConnectCLIAtSkipsMigrationsOnExistingDB`: no `goose_db_version` table; every one of the 4 pooled connections has `busy_timeout=5000` and `foreign_keys=1`; MaxOpen is 4.
  - `TestConnectCLIAtMigratesMissingDB`
  - `TestConnectCLIAtMigratesEmptyFile`
- **`internal/app`:**
  - `TestOneShotStartsNoPrimaryServicesEvenAsPrimary`: primary and secondary roles, startup and promotion triggers; 0 starts.
  - `TestApplyCronJobsFromPeerUpdatesMemoryAndScheduler`: in-memory config and running scheduler updated (`NextRun` set); **no file under the project or HOME is created or rewritten**; an invalid schedule is refused and changes nothing.
  - `TestForwardCronJobsToPrimaryIsNoopWithoutSecondaryIPC`
  - `TestRelinkKB`: first pass, idempotent repeat (0 candidates), force, and refusal on a secondary.
- **`cmd`:**
  - `TestCronJobReloadRPCReachesPrimary`: a real bus with a Bootstrap primary and secondary; the secondary's `ForwardCronJobsToPrimary` reaches the primary's running scheduler.
  - `TestKBRelinkForwardsToPrimaryElseRunsLocally`: the local fallback with no instance, then forwarded to a real primary (non-force links only the new document; force relinks both).
  - `TestWireIPCOneShotSecondaryIsNotPromoted`: after the primary's graceful handover, the one-shot secondary is still not primary 1.5 s later and the lock is free.
  - `TestP4EntrypointsSourceShape`: agui/cronjob wiring and options; `OneShot: true` twice in cronjob.go; no `db.Connect()` left in the six files; `ConnectCLI` used; the maintenance handlers in `primaryBusSetupFunc`; 3 `UpdateCronJobs` calls and 3 propagations in the cron handlers, so a future fourth write path fails the test.
  - `TestShutdownEntrypointOrdered`
  - `TestDecodeCronJobReloadParams`

## Verification
- `go build ./...`: clean.
- `go vet ./cmd ./internal/app ./internal/db ./internal/cronjob ./internal/api ./internal/config ./internal/ipc/...`: clean.
- `gofmt -l` on touched packages: only the pre-existing `cmd/test_ollama_main/main.go`.
- `go test -race -count=1 ./internal/app/... ./internal/ipc/... ./cmd/... ./internal/cronjob/... ./internal/db/...`: all ok, no races.
- `go test -count=1 ./internal/rag/... ./internal/mesnada/... ./internal/api/... ./internal/config/... ./internal/instanceregistry/...`: all ok. The config package initially failed `TestConfigMutatorsAreLockAware` on the new `SetCronJobsInMemory`, which was fixed by making it lock-aware.
- `internal/llm/agent` was not run (the 4 known pre-existing failures there are unrelated).

### Isolated smoke test (scratch binary; the real `.pando/` and `/tmp/pando-instances` were never touched)
**Setup:**
- `smoke4/run_p4_smoke.sh` + `p4_smoke.py` in the session scratchpad.
- Inside `unshare -Urmn` (private user, mount and net namespace, loopback only), with a tmpfs over `/tmp/pando-instances` and a throwaway HOME and project.
- The project `.pando.toml` enables Remembrances (`KBWikiLinks`), Mesnada and a cron job whose engine is deliberately nonexistent, so no real agent is ever launched.
- A = `pando acp` is the primary.

**Results, all 45 checks PASS:**
1. **`cronjob run p4smoke` with A primary:**
   - It exited 0 in **0.48 s** (`Task spawned`).
   - It logged `one-shot process ... role=secondary startup_mode=cronjob` and `one-shot secondary, failover promotion not armed`.
   - Nothing was started or scheduled: no cron, no code index, no primary services.
   - A was still alive and still named in the lock; nobody was promoted.
2. **`kb relink` and `kb relink --force`:** printed `Forwarding the link rebuild to the running Pando instance (pid A)` and `Indexed 2 link(s) across 2 document(s)` (0.03 s).
3. **`pando serve` as a secondary, then `POST /api/v1/cronjobs` (token from `/api/v1/token`):**
   - The response was 201, and the edit was persisted to `.pando.toml`.
   - serve logged `saved cron configuration handed to the IPC primary`.
   - A (**ACP, a non-TUI primary**, which is exactly P3's gap) logged `cronjob.reload: applied ... jobs=2 scheduler=true` and `cronjob_event=scheduled name=p4new` exactly once, **0.018 s** after the POST returned.
   - serve itself scheduled nothing.
4. **`pando agui-serve --no-tls --no-token`:**
   - It registered as `mode: agui`, `is_primary: false`.
   - After SIGTERM it exited 0 in 0.03 s and revoked its registry entry. A was unaffected.
5. **Teardown, then a lone one-shot primary:**
   - serve was stopped with SIGTERM (exit 0), then A with stdin EOF (exit 0). The lock was released.
   - `cronjob run` alone was then a **one-shot primary** (`role=primary`): it still started no primary services, exited 0 in 0.57 s, and released the lock.
   - No stray processes remained.

**What the first smoke runs revealed:**
- Run 1: the only failure was a harness bug (a missing API token: 401).
- Run 2 found a **pre-existing** bug, described under the risks below. The harness now sets `[Data] Directory` explicitly, and the nil-DB guards were added to the two P4 entrypoints.

## Deviations from the plan sketch
- `db compact`'s local fallback also moved to `ConnectCLI`; the plan listed it as "fine".
- `compactViaRunningInstance` was refactored onto the shared `dialRunningPrimary`, with the same behaviour.
- `SetCronJobsInMemory` checks the overlay lock (required by the config lock-coverage test).
- A one-shot *primary* still serves the bus and runs the primary watcher; only the background services are off. The plan only said "starts no background services".
- nil-DB guards were added in agui-serve and cronjob run (see the risks).

## Follow-ups and risks
- **Pre-existing, found by the smoke:** `config.updateConfigFileAt` (behind every `Update*`, including `UpdateCronJobs`) unmarshals the file into a fresh `Config`, applies the mutator and writes the **whole struct** back.
  - Every absent key becomes an explicit zero value. For example `[Data] Directory = ''` blanks the default `.pando`, and every process started afterwards fails with `data.dir is not set`.
  - On a secondary, `Bootstrap` then "continues with no DB" (`rt.SQLDB == nil`), and `app.New` **panics** (nil `*sql.DB` in `SeedFromGlobal`).
  - P4 guards its two entrypoints. serve, desktop, app, TUI, ACP and mcp-server still share the nil-SQLDB panic path.
  - Both issues are out of P4's scope and deserve their own fix: write only the changed keys, and have Bootstrap fail instead of returning a DB-less secondary.
- **No primary → secondaries cron push.** A secondary's in-memory `cfg.CronJobs` goes stale after an edit made elsewhere. It schedules nothing, but its next edit builds on its stale copy and can drop a concurrent edit (a lost update across processes, pre-existing).
- `SetCronJobsInMemory` writes `cfg.CronJobs` on the bus handler goroutine. This is the same unsynchronised global-config pattern `UpdateCronJobs` already uses from HTTP handlers.
- `ConnectCLI` never migrates an existing DB. A **newer** CLI against an **older** schema may fail on new columns or tables (an explicit error rather than a silent migration). A warning based on the goose version could be added later.
- **agui-serve's thread store** (`agui.Deps.DB = rt.SQLDB`) writes directly. On a secondary that is the 1-conn, 200 ms pool, which makes it a direct writer like history, project and design (a P5 item).
- **`cronjob run` as a degraded secondary** (unresponsive primary, no kill) cannot make remembrances writes. That is acceptable for a short run.
- **`cronjob run` still exits right after spawning its Mesnada task.** The semantics are unchanged: how the task outlives the process is the orchestrator's business, as before.

Links: [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/ipc_failover_p0_inplace_promotion.md]], [[pando/changes/ipc_wiring_p1_shared_wireipc.md]], [[pando/changes/mcp_server_ipc_bootstrap_p2.md]], [[pando/changes/ipc_role_aware_services_p3.md]]
