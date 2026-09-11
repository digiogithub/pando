---
created_at: 2026-09-11T20:53:45.27564435Z
updated_at: 2026-09-11T21:54:07.657955601Z
tags:
    - change
    - ipc
    - failover
    - cron
    - remembrances
---
# Change: P3 — role-aware background services (primary-only) (2026-09-11)

Implements phase **P3** of [[pando/plans/mcp_server_ipc_bootstrap.md]] (§1.4, §5.4, §7 P3, §8). Builds on [[pando/fixes/ipc_failover_p0_inplace_promotion.md]] (in-place promotion and the `startPrimaryServices` hook), [[pando/changes/ipc_wiring_p1_shared_wireipc.md]] (shared `wireIPC`) and [[pando/changes/mcp_server_ipc_bootstrap_p2.md]] (mcp-server on the bootstrap). P4–P6 and G7 are not started.

> **Update 2026-09-11:** the cron cross-process follow-up (§5 "Cross-process consistency" and the first two "Follow-ups and risks" items) is **resolved by P4**, see [[pando/changes/ipc_other_entrypoints_p4.md]].

## Problem

Every process started by `app.New` (primary or secondary) ran all the background services. Their writes were proxied correctly on secondaries, but the work was duplicated N times:
- every process reindexed every file on each fsnotify event (N× embedding and N× coordinator jobs);
- every cron job fired once per process.

## What changed

### 1. Role plumbing

**`AppOptions.IPCRole ipcruntime.Role`** (`internal/app/app.go`)
- No new type was needed. `internal/app` already imports `internal/ipc/runtime` (for `NewPrimaryBus`), and `runtime` does not import `app`, so there is no cycle.

**`resolveIPCRole(opt AppOptions)`** (`internal/app/primary_services.go`)
1. An explicit `RolePrimary`/`RoleSecondary` wins.
2. Otherwise, a `*dbproxy.DBProxy` querier with `IsRemote()` is a secondary.
3. Otherwise, the process is primary. This keeps today's behaviour for callers without IPC: tests, one-shot CLIs, `cronjob run` and `agui-serve` (until P4).
- An unknown non-empty value falls back to the same inference.

**Entrypoints, all passing `rt.Role`:**
- `cmd/root.go`: TUI and ACP `app.New(... IPCRole: rt.Role ...)`.
- `cmd/mcp_server.go` (`bootstrapMCPServer`): `IPCRole: rt.Role`.
- `cmd/serve.go`, `cmd/desktop.go`, `cmd/app.go`: they already passed `Role: string(rt.Role)` in `api.ServerConfig`. `api.NewServer` (`internal/api/server.go`) now forwards it as `IPCRole: ipcruntime.Role(cfg.Role)`. When it is empty, the querier-based inference applies.

### 2. `startPrimaryServices` (new `internal/app/primary_services.go`)

**Functions:**
- `registerPrimaryService(name, start func(ctx))`: `New` registers each primary-only service instead of spawning it.
- `applyStartupIPCRole(role, startupMode)`: runs at the end of `New`. A primary calls `startPrimaryServices("startup")`; a secondary only logs.
- `startPrimaryServices(trigger string) bool`: starts the services **at most once per App**.
  - Guarded by `primarySvcMu` with `primarySvcStarted`/`primarySvcClosed` flags.
  - A second call is a no-op: a spurious promotion of a primary, concurrent callers, or a call after Shutdown began.
  - It returns whether this call started the services.
- `closePrimaryServices()`: called by `App.Shutdown` right after `releasePrimaryRole()`.

**Lifetime context.** Services always run on `app.lifetimeCtx`, the ctx `New` received. They never run on the watcher's promote ctx or on the promotion's `primaryCtx`. That means a promotion started from the failover watcher goroutine is not tied to a context that ends early. Shutdown stops them exactly as when they start in `New`: through `watcherCancelFuncs`/`watcherWG`, and `CronService.Stop`.

**`PromoteToPrimary`** keeps the call after pool, bus, proxy and registry are promoted: `app.startPrimaryServices("promotion")` (it previously passed `primaryCtx` to a no-op).

**Race and lock ordering** (promotion runs on the watcher goroutine while the app is live):
- `PromoteToPrimary` holds `ipcMu` and then takes `primarySvcMu`.
- `Shutdown` takes `ipcMu` in `releasePrimaryRole` (which sets `ipcHandedOver`, so any later promotion is refused), releases it, and only then calls `closePrimaryServices`. Neither path holds the two locks in the opposite order.
- `startPrimaryServices` holds `primarySvcMu` while the starters spawn their goroutines (each start only spawns). Every `watcherWG.Add` therefore happens-before Shutdown's `watcherWG.Wait`.
- Verified under `-race` by `TestStartPrimaryServicesConcurrentCallsStartOnce` and by a real promotion test.

**Log lines** (one Info line, visible in logs):
- `IPC role: primary, starting primary-only background services` with `trigger=startup|promotion` and `services=[...]`.
- `IPC role: secondary, primary-only background services skipped until promotion` with `startup_mode` and `services=[...]`.
- Also Info when a start is refused because the app is shutting down, and Debug when the services already run.

### 3. Services gated (primary-only)

| Service name | Where | Notes |
|---|---|---|
| `code-index-watcher` | `internal/app/remembrances_code.go` (`initRemembrancesProjectIndexing`) | The project id is still resolved **per process** and written to `cfg.ContextEnrichmentCodeProject`, because the context enricher built later in `New` reads it. Only the startup index plus fsnotify goroutine is gated. The `StartupMode=="cronjob"` skip and the home-directory skip are unchanged. |
| `kb-auto-import` | `internal/app/remembrances.go` (`initRemembrancesKBSync`) | `ConfigureFilesystemMirror` and `SetDocumentConverter` stay per process: they configure this process's own KB write path, not a background task. |
| `kb-watch` | same | KB directory watcher. |
| `kb-link-backfill` | `remembrances.go` (`initKBLinkBackfill`) | |
| `memory-gc` | `app.go` | |
| `cron` | `app.go` | `CronService.Start(ctx, config.Get().CronJobs)` reads the current config at start time, so a promotion uses the freshest in-memory config. |
| `enrichment-session-cleanup` | `app.go` | **Addition beyond the listed set.** It was gated by `remembrancesProxy == nil`, which is equivalent to "primary" at startup, but it never ran on a promoted primary. It now uses the hook. |

- `initRemembrancesProjectIndexing`, `initRemembrancesKBSync` and `initKBLinkBackfill` lost their `ctx` parameter; the context now comes from `startPrimaryServices`.
- These services now start at the **end** of `New` (after extensions start) rather than midway. This is a few milliseconds later and has no functional dependency. If `New` fails after registration, nothing is started (previously these goroutines leaked).

### 4. Kept per process (with justification)
- **Session indexer:** it only sees its own message broker, and its writes proxy. Unchanged per the plan.
- **MCP gateway `Initialize`:** it builds this process's own in-memory tool registry and pooled MCP clients, which this process's agent calls.
  - It is not a background DB writer.
  - Gating it would leave secondaries (an mcp-server, a TUI) without their MCP tools.
  - The gateway's favourites writes are a P5 "direct writer" item.
- **`RegisterSelfAsGlobalProject` + `ProjectManager.SeedFromGlobal`:** a one-shot, idempotent reconciliation (paths already in the DB are skipped).
  - It seeds this process's own global self-registration.
  - Gating it would delay a secondary's own project appearing in the DB until the next primary restart.
  - The duplicated cost is negligible.
  - Its writes use the direct `rawQ` (a P5 direct writer), not a shared background loop.
- Also per process (not gated): `ProjectManager.StartIdleGC` (warm instances this process started), evaluator template seeding (idempotent, proxied), LSP, telemetry, extensions.

### 5. Cron

**Exactly one start.**
- ACP's second `CronService.Start` (and its `defer CronService.Stop()`) was removed from `runACPServerWithOptions` (`cmd/root.go`).
- Cron now starts only in `app.New` through the `cron` primary service, only on the primary or on promotion, and is stopped by `App.Shutdown`.
- Nuance: ACP used to start cron only when `cfg.CronJobs.Enabled`. `app.New` always called `Start`, and still does: `scheduleLocked` schedules nothing while disabled, and the `config.Bus` reload picks up a later enable. The app's semantics are unchanged.

**`internal/cronjob/service.go` changes:**
- **Live config on an unstarted service.** A new `configJobs` source (default `config.Get().CronJobs`). `ListJobs` and `RunNow` read it while the scheduler is not started. On a secondary, the TUI cron dialog, the WebUI list and "run now" (explicit user actions, allowed on a secondary) therefore keep working. `NextRun` is empty there, because this process schedules nothing.
- **`Reload` on an unstarted service** only validates.
  - Previously it added entries to the unstarted cron, and a later `Start` scheduled them again.
  - That would have made every job fire **twice** after a promotion whenever the WebUI had hot-reloaded the secondary's service (`handlers_cronjobs.go` calls `Reload` after each edit).
- **`Stop` removes the entries,** so a restart does not double-schedule.
- **Pre-existing data race fixed.** `watchConfigChanges` read the `s.reloadCh` field without the lock while `Stop` cleared it under `s.mu`; production hits this too, via `App.Shutdown` → `CronService.Stop`. The channel is now passed to the goroutine as a parameter. The new `-race` test found it.

**Cross-process consistency (task item 4). Decision: document as a follow-up, not implemented.** **RESOLVED by P4 (2026-09-11):** the proposed `cronjob.reload` RPC is implemented, see [[pando/changes/ipc_other_entrypoints_p4.md]] §4. All three cron REST handlers now forward the saved configuration from a secondary to the primary. The primary applies it with `config.SetCronJobsInMemory` (no file write, no `config.Reload()`) plus `CronService.Reload`. The smoke test showed an ACP primary scheduling a job added through a serve secondary 0.018 s after the save. The original analysis is kept below for the record.
- Cron jobs live in the **config file** (`config.UpdateCronJobs` → `updateCfgFile`). There are no cron DB tables, so there are no changepub events to react to.
- The primary reloads cron today through:
  - the in-process `config.Bus` (`watchConfigChanges`), which covers edits made on the primary itself;
  - a process that runs the config-file watcher (`config.WatchConfigFile`). **Only the TUI runs one** (`cmd/root.go`), so a TUI primary also picks up edits from other processes.
- **Gap.** A cron edit made through a *secondary's* WebUI (serve/desktop/app; the only cron edit surface) is persisted to the file. A *non-TUI* primary (ACP, mcp-server, serve...) does not schedule it until it restarts or another process is promoted.
  - Before P3 that edit was scheduled only by the process that made it, while every other process kept firing the old set. It was inconsistent already; P3 trades N× duplicates for this staleness window.
- Why no cheap fix was safe:
  - Calling `config.Reload()` on the primary would wipe in-process runtime overrides (for example mcp-server's `enableMCPServerFeatures`/flag overrides).
  - A merge-correct, cron-only re-read of the global plus project-local files (the local file may lack a cron section) needs new config plumbing.
- **Proposed follow-up (~100 lines):** a `cronjob.reload` IPC RPC, sent by the editing secondary after `UpdateCronJobs`, carrying the `CronJobsConfig`. The primary applies it in memory (`cfg.CronJobs` plus `CronService.Reload`, without rewriting the file), following the `CompactDatabase` forward pattern. It touches `internal/ipc/protocol`, the bridge handler registration in `cmd`, `App`, and `internal/api/handlers_cronjobs.go`.

### 6. Secondary UX (explicit actions still work)
Only the automatic starts are gated. The following all run on a secondary as before, writing through the proxy:
- `code_index_project`, `code_reindex_file`;
- KB tools (`kb_add_document`, `kb_delete_document`...), `remember`/`forget`;
- slash commands;
- cron list and "run now".

## Files and symbols
- `internal/app/primary_services.go` (new): `primaryService`, `resolveIPCRole`, `registerPrimaryService`, `applyStartupIPCRole`, `startPrimaryServices`, `closePrimaryServices`, `primaryServiceNamesLocked`.
- `internal/app/app.go`: `AppOptions.IPCRole`; App fields `lifetimeCtx`, `primarySvcMu`, `primarySvcs`, `primarySvcStarted`, `primarySvcClosed`; `New` (registrations for memory GC, cron and enrichment cleanup, plus `applyStartupIPCRole` at the end); `PromoteToPrimary` (`startPrimaryServices("promotion")`); `Shutdown` (`closePrimaryServices`); the old no-op hook was removed.
- `internal/app/remembrances.go`: `initKBLinkBackfill`, `initRemembrancesKBSync`.
- `internal/app/remembrances_code.go`: `initRemembrancesProjectIndexing`.
- `internal/cronjob/service.go`: `configJobs`/`liveConfigJobs`, `jobsLocked`, `ListJobs`, `jobByNameLocked`, `reloadLocked`, `Stop`, `watchConfigChanges(ctx, reloadCh)`.
- `internal/api/server.go`: `NewServer` forwards `ServerConfig.Role` as `AppOptions.IPCRole`.
- `cmd/root.go`: TUI and ACP pass `IPCRole: rt.Role`; the ACP cron start was removed.
- `cmd/mcp_server.go`: `IPCRole: rt.Role`.
- Tests:
  - `internal/app/primary_services_test.go` (new)
  - `internal/cronjob/service_test.go` (new; the package had no tests)
  - `cmd/ipc_role_test.go` (new)

## Tests added
- `internal/app/primary_services_test.go`:
  - `TestResolveIPCRole`: no IPC, direct querier, remote proxy, promoted proxy, explicit roles winning, unknown value.
  - `TestSecondaryRoleStartsNoPrimaryServices`
  - `TestPrimaryRoleStartsPrimaryServicesOnce`: a second call is a no-op.
  - `TestPromoteToPrimaryStartsPrimaryServicesExactlyOnce`:
    - a **real in-place promotion** using P0's `newSecondaryFixture`;
    - the services start once;
    - cancelling the promote ctx does not end their ctx, while cancelling the lifetime ctx does;
    - a second call is a no-op.
  - `TestPrimaryServicesNotStartedOnceShutdownBegan`
  - `TestStartPrimaryServicesConcurrentCallsStartOnce`: 16 goroutines, under `-race`.
  - `TestKBWatchIsPrimaryOnly`:
    - a real `initRemembrancesKBSync` registration against a real KB store and DB;
    - a secondary spawns 0 goroutines, and a promotion spawns 1;
    - it is stopped through `watcherCancelFuncs`/`watcherWG` exactly like Shutdown.
- `internal/cronjob/service_test.go`:
  - `TestUnstartedServiceReadsLiveConfigAndSchedulesNothing`
  - `TestStartAfterReloadSchedulesEachJobOnce`: also checks that Stop removes entries and is idempotent.
- `cmd/ipc_role_test.go` `TestEntrypointsPassIPCRoleToApp` (source-shape):
  - `root.go` has `IPCRole: rt.Role` ×2 and no `CronService.Start(`;
  - `mcp_server.go` passes `IPCRole`;
  - serve/desktop/app pass `string(rt.Role)`;
  - `api.NewServer` forwards the role.
- No existing `cmd` test asserted the ACP cron start (checked), so no source-shape test needed updating.

## Verification
- `go build ./...`: clean.
- `go vet ./internal/app ./cmd ./internal/cronjob ./internal/api`: clean. `gofmt -l` on the touched packages: clean (`cmd/test_ollama_main/main.go` is pre-existing and untouched).
- `go test -race -count=1 ./internal/app/... ./internal/ipc/... ./cmd/... ./internal/cronjob/...`: all ok, no races. `go test -race -count=10 ./internal/cronjob`: ok after the race fix.
- `go test -count=1 ./internal/rag/... ./internal/db/... ./internal/mesnada/... ./internal/api/...`: all ok.
- `./internal/llm/agent`: only the 4 known pre-existing failures (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged`).

### Isolated three-process smoke test
**Setup:**
- A scratch binary and `smoke3/run_p3_smoke.sh` + `p3_smoke.py` in the session scratchpad.
- `unshare -Urmn` (private user, mount and net namespace, loopback only), with a tmpfs over `/tmp/pando-instances` and a throwaway HOME and project.
- The project `.pando.toml` enables Remembrances (ollama providers, unreachable in the netns), Mesnada, and one cron job (`p3smoke`, `0 3 * * *`).
- It also sets a top-level `LogFile`, because **mcp-server has no `--log-file` flag**; the ACP processes override it with theirs.
- The real `.pando/` and the real `/tmp/pando-instances` were never touched.

**Flow:**
1. A = `pando acp` (primary).
2. B = `pando acp` and C = `pando mcp-server --no-http` (both secondaries).
3. Close A's stdin (graceful handover).

**Results:**
- A logged `IPC role: primary, starting primary-only background services trigger=startup services="[code-index-watcher enrichment-session-cleanup memory-gc cron]"`, plus exactly one `cronjob_event=scheduled` and one `remembrances code: startup indexing scheduled`.
- B (`startup_mode=acp`) and C (`startup_mode=mcp`) logged `IPC role: secondary, primary-only background services skipped until promotion`. Neither scheduled cron nor the code index or watcher.
- A exited with 0. A secondary promoted **0.215 s** later in both runs:
  - run 1: winner **B** (acp);
  - run 2: winner **C** (mcp-server).
- The winner logged `... trigger=promotion` exactly once, plus exactly one `cronjob_event=scheduled` and one `startup indexing scheduled`. The loser still had none: exactly one scheduler and indexer per project.
- All processes exited 0, the lock file was 0 bytes afterwards, and no stray processes were left.
- A first attempt failed only because the harness passed `--log-file` to mcp-server (the unknown flag made C exit 1). The harness was fixed; the code was not changed.

## Follow-ups and risks
- **Cron cross-process edits** reach a non-TUI primary only after a restart or promotion (see §5). The proposed fix is the `cronjob.reload` RPC. **RESOLVED by P4** ([[pando/changes/ipc_other_entrypoints_p4.md]]).
- **P4 entrypoints still have no IPC** (`agui-serve`, `cronjob run`, `kb relink`). They fall back to "primary" and therefore still run the primary services, as before P3. `cronjob run`'s code-indexer skip is unchanged, and its cron scheduler starts as before. The plan's P4 "oneshot" role should skip them. **RESOLVED by P4:** agui-serve is on `wireIPC`; `cronjob run` is a one-shot process (`AppOptions.OneShot`), which starts no primary services even as primary and is never armed for promotion; `kb relink` forwards over `kb.relink`.
- A primary whose bus fails to start ("continue without IPC") still reports `RolePrimary` and runs the services, the same as before.
- A degraded mcp-server secondary (unresponsive primary, `AllowKillStalePrimary=false`) runs none of the services. If the primary is stuck but alive, nobody indexes until it recovers or dies. This is acceptable: the kill policy is G7.
- The service parameters (KB path, GC interval, code project id) are snapshotted at `New`. A config change between startup and a promotion is not reflected, matching pre-P3 behaviour, where they started at `New`. Cron reads `config.Get()` at start time.
- A promotion's `startPrimaryServices` reads `config.Get()` on the watcher goroutine. This is the same unsynchronised global-config access pattern used throughout the app.

Links: [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/ipc_failover_p0_inplace_promotion.md]], [[pando/changes/ipc_wiring_p1_shared_wireipc.md]], [[pando/changes/mcp_server_ipc_bootstrap_p2.md]], [[pando/changes/ipc_other_entrypoints_p4.md]]
