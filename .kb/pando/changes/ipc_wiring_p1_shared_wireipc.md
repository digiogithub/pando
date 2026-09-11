---
created_at: 2026-09-11T19:06:22.256052648Z
updated_at: 2026-09-11T19:06:22.256052648Z
---
# Change: P1 — shared `wireIPC` helper, `BootstrapWithOptions`, `ModeMCP`, ACP signal handling (2026-09-11)

Implements phase **P1** of [[pando/plans/mcp_server_ipc_bootstrap.md]] (§5.1, §7). Builds directly on [[pando/fixes/ipc_failover_p0_inplace_promotion.md]] (P0's in-place promotion, ordered handover, looping watcher, canonical workdir). P2 (mcp-server on the bootstrap) and P3 (role-aware background services) are not started; `cmd/mcp_server.go` is untouched.

## What changed

### 1. `cmd/ipc_wiring.go` (new)

- `wireOptions{AcceptDelegations *bool}`: nil keeps `config.Get().Mesnada.Delegation.AcceptDelegations` (every entrypoint migrated here); a non-nil override is for P2's ephemeral mcp-server, which will pass `&false` to refuse peer delegations unconditionally.
- `wireIPC(ctx, rt *ipcruntime.BootstrapResult, pandoApp *app.App, instanceID, cwd string, mode instanceregistry.Mode, opts wireOptions) (cleanup func())`: the post-`app.New` half of the bootstrap, now written once and shared by all five entrypoints.
  - Announces the instance (`instanceregistry.Announce`); returns a `cleanup` that calls `instanceregistry.Revoke`.
  - Primary (`wirePrimary`): wires the shared bus handlers, calls `pandoApp.SetupIPC(bus)`, registers the ordered handover (`pandoApp.SetIPCPrimaryHandover(coord, rt.ReleaseLock)`), starts the bus, and — only if that succeeds — starts the primary failover watcher (`rt.Watcher.Start(ctx)`). The handover is registered even if the bus fails to start, matching Bootstrap's own "continue without IPC" fallback: a shutdown must still drain and release the lock.
  - Secondary (`wireSecondary`): calls `pandoApp.SetIPCSecondaryContext(...)` with a `busSetupFunc` and arms the promotion callback (`rt.Watcher.SetPromoteCallback(pandoApp.PromoteToPrimary)`). It does **not** call `rt.Watcher.Start(ctx)` again — `ipcruntime.Bootstrap` already starts the secondary watcher unconditionally so it can heartbeat-monitor even before a promote callback exists (P0); calling `Start` twice would spawn a second monitoring goroutine and double-close the watcher's `done` channel.
- `primaryBusSetupFunc(instanceID, cwd string, pandoApp *app.App, opts wireOptions) app.IPCBusSetupFunc`: the one implementation of "primary wiring" (write coordinator, changepub publisher, `dbproxy.RegisterHandlersWithCoordinator`, `registerBridgeHandlers`, bridge heartbeats). It is used in exactly two places so they can never drift apart: directly by `wirePrimary` against the bus Bootstrap created, and as the `busSetupFunc` a promoted secondary's `App.PromoteToPrimary` calls against a freshly created bus. It must not start/bind the bus — the caller does that (`bus.Start` / the runtime's `startPrimaryBus` retry).

### 2. Migrated entrypoints

`cmd/root.go` (TUI `RunE` and `runACPServerWithOptions`), `cmd/serve.go`, `cmd/desktop.go` (`runDesktopMode`), `cmd/app.go` (`runAppMode`) all replaced their copy-pasted primary/secondary block with one `wireIPC(...)` call. Net: root.go −144 lines, serve.go/desktop.go/app.go each −~30 lines, offset by the new `cmd/ipc_wiring.go` (+171) and its tests (+260).

**Behavioural differences preserved / deliberately unified** (per-entrypoint diff before this change):
- **ACP-specific bits kept in root.go, not moved into wireIPC**: `signal.Ignore(SIGPIPE)`, the `--debug` requires `--log-file` check, the ACP `*log.Logger`→slog bridge, forcing `Permissions.SetGlobalAutoApprove(true)`, the CronService start (still duplicated with `app.New`'s own cron start — that duplication is P3's to fix, not touched here), and `pandoAgent.StartNotificationBroadcast`.
- **`AcceptDelegations`**: every migrated entrypoint passes `wireOptions{}` (nil override) — identical behaviour to today (config default, false out of the box).
- **Deliberate unifications (previously-missing pieces, now uniform across all five)**:
  - `serve`/`desktop`/`app` previously had **no secondary branch at all** (G3 in the P0 doc's gap list: their secondary watcher never promoted). They now get the exact same secondary wiring as TUI/ACP, including the promotion callback.
  - `serve`/`desktop`/`app` previously never called `pandoApp.SetupIPC(bus)` on their primary bus, so the remembrances IPC write dispatcher was never registered there — a pre-existing gap noted in the P0 doc's "What remains". Now registered uniformly.
  - `serve`/`desktop`/`app` previously never called `SetIPCPrimaryHandover`; their lock was released only by `rt.Cleanup` at the very end of process shutdown (no drain, no early release). They now get the same ordered handover as TUI/ACP.
  - `app.go`'s primary block was missing `changepub.NewBusPublisher`/`coord.SetPublisher` entirely (silently dropping write-change events instance-to-instance). `serve.go`/`desktop.go` had it; `wireIPC` now includes it for all three.
  - `serve`/`desktop`/`app` previously started the primary failover watcher **never** (not even conditionally); they now call `rt.Watcher.Start(ctx)` once the bus is confirmed up, exactly like TUI/ACP.
  - The bridge-heartbeat goroutine (`bridge.New(...).Start(ctx)`) now always starts as soon as handlers are registered (inside `primaryBusSetupFunc`), before `bus.Start`/`startPrimaryBus` runs — matching how the promotion path already worked (P0's `busSetupFunc` contract) rather than the TUI/ACP-only "start bridge only if `bus.Start` succeeded" ordering. If `bus.Start` then fails, the bridge goroutines just fail to publish (logged) until cancelled; this is a negligible, documented simplification, not a functional regression.
  - Per-entrypoint bus-start-failure log messages (`"IPC: serve mode failed to start bus"`, `"IPC: desktop mode failed to start bus"`, `"IPC: app mode failed to start bus"`, the ACP `logger.Printf` variant) are now one message from `wirePrimary`: `logging.Warn("IPC: failed to start bus, continuing without IPC", "error", busErr)`. Still never touches stdout for ACP (slog only).

### 3. `instanceregistry.ModeMCP` (`internal/instanceregistry/entry.go`)

Added `ModeMCP Mode = "mcp"`. Unused until P2. The one non-exhaustive `switch e.Mode` display helper (`internal/tui/components/instances/view.go:modeStr`) got a `case ModeMCP: return "MCP"` arm so the Instances panel doesn't fall through to the `"TUI"` default once P2 lands.

### 4. `registerBridgeHandlers` `AcceptDelegations` override (`cmd/bridge_delegation.go`)

New signature: `registerBridgeHandlers(bus *ipc.Bus, instanceID string, pandoApp *app.App, acceptOverride *bool)`. The decision itself is factored into `resolveAcceptDelegations(acceptOverride *bool) bool` (nil → `config.Get().Mesnada.Delegation.AcceptDelegations`; non-nil → the override wins), so it is unit-testable without a running instance. All current call sites (now only inside `primaryBusSetupFunc`) pass `opts.AcceptDelegations`, which is nil for every entrypoint migrated in P1.

### 5. `ipcruntime.BootstrapWithOptions` (`internal/ipc/runtime/runtime.go`)

- `Options{ProbeTimeout time.Duration, AllowKillStalePrimary bool}` and `DefaultOptions()` (10s probe, kill allowed — today's `Bootstrap` behaviour, which now just calls `BootstrapWithOptions(ctx, workdir, id, DefaultOptions())`).
- `BootstrapWithOptions(ctx, workdir, instanceID string, opts Options) (*BootstrapResult, error)`: when the primary is unresponsive,
  - `AllowKillStalePrimary=true` (unchanged): `killStalePrimary` + `reacquireAfterKill`, same as before.
  - `AllowKillStalePrimary=false` (new, for P2): the primary is **never** killed. `BootstrapWithOptions` logs one `logging.Warn` ("continuing as a degraded secondary instead of killing it") and falls through to the normal secondary bootstrap path unchanged. No new "degraded mode" plumbing was needed: `DBProxy` already tries direct-first sqlc writes before proxying (§1.3 of the plan), and remembrances writes (which always proxy) already fail loudly once their forward times out against an address nobody answers — that is the existing, correct "fail loudly" behaviour, just now reachable without ever touching the unresponsive process.
- `killStalePrimary`/`primaryResponds` now take an explicit `timeout time.Duration` parameter instead of the hardcoded `stalePrimaryProbeTimeout` constant, so a custom `ProbeTimeout` is actually honoured. `internal/ipc/runtime/runtime_kill_test.go`'s three call sites were updated to pass `stalePrimaryProbeTimeout` explicitly (no behaviour change for those tests).

### 6. Signal handling for the ordered handover

- **serve/desktop/app**: already had `signal.NotifyContext(..., SIGINT, SIGTERM)` driving `server.Shutdown(ctx)`, which calls `s.app.Shutdown()` (`internal/api/server.go`). Since `App.Shutdown` now starts with `releasePrimaryRole()` (P0) and `wireIPC` now registers the handover resources for these three (item 2 above), they get the ordered handover on SIGINT/SIGTERM automatically — no new signal code needed there, just the missing `SetIPCPrimaryHandover` registration.
- **ACP** (`runACPServerWithOptions`, `cmd/root.go`): previously had **no** SIGINT/SIGTERM handling at all (only `signal.Ignore(SIGPIPE)`) — the P0 doc flagged this as an open risk. Fixed: the function's root `ctx` is now `ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)`. `transport.Run(ctx)` (`internal/mesnada/acp/transport_stdio.go`) already returns as soon as `ctx.Done()` fires, which then runs every deferred cleanup in order (`unwireIPC` no-op, `pandoApp.Shutdown()` → the ordered handover, `rt.Cleanup()`, `CronService.Stop()`). The final `return` treats a `context.Canceled` from this signal-driven cancellation as a graceful, non-error exit — the same way serve/desktop/app already treat `http.ErrServerClosed` after their own signal-triggered `Shutdown`. A one-line `logging.Info("acp: shutdown signal received")` on the signal path uses slog only; stdout still carries only the ACP JSON-RPC stream.

## Files and symbols touched

- `cmd/ipc_wiring.go` (new): `wireOptions`, `wireIPC`, `wirePrimary`, `wireSecondary`, `primaryBusSetupFunc`.
- `cmd/root.go`: TUI `RunE` and `runACPServerWithOptions` migrated to `wireIPC`; ACP gained `signal.NotifyContext`-driven shutdown; removed now-unused imports (`ipc`, `ipc/bridge`, `ipc/changepub`, `ipc/dbproxy`, `ipc/writecoordinator`), added `errors`.
- `cmd/serve.go`, `cmd/desktop.go`, `cmd/app.go`: migrated to `wireIPC`; same import cleanup.
- `cmd/bridge_delegation.go`: `resolveAcceptDelegations`, `registerBridgeHandlers` gained the `acceptOverride *bool` parameter.
- `internal/instanceregistry/entry.go`: `ModeMCP`.
- `internal/tui/components/instances/view.go`: `modeStr` gained a `ModeMCP` case.
- `internal/ipc/runtime/runtime.go`: `Options`, `DefaultOptions`, `BootstrapWithOptions`; `Bootstrap` now delegates to it; `killStalePrimary`/`primaryResponds` gained a `timeout` parameter.
- Tests (new/updated): `cmd/ipc_wiring_test.go`, `cmd/bridge_delegation_test.go`, `cmd/root_test.go` (`TestEntrypointsUseSharedIPCWiring` replaces `TestRunACPServerWithOptions_ConfiguresSecondaryIPCFailoverPath`), `internal/ipc/runtime/runtime_bootstrap_options_test.go`, `internal/ipc/runtime/runtime_kill_test.go` (signature update), `internal/instanceregistry/registry_test.go` (`TestModeMCPRoundTrips`).

## Test design notes

- `cmd/ipc_wiring_test.go`'s `TestWireIPCPrimaryBranch`/`TestWireIPCSecondaryBranch` build a real `*ipcruntime.BootstrapResult` (isolated `$HOME`/project via `config.Load`) and a **bare** `*app.App{}` populated with just enough real services — `session.NewService`, `message.NewService` and a minimal hand-written `fakeAgentService` (embeds `*pubsub.Broker[agent.AgentEvent]` to satisfy `Subscribe`) — because `bridge.New(...).Start(ctx)` spawns goroutines that call `.Subscribe(ctx)` on `Sessions`/`CoderAgent` immediately and panic on a nil interface. `app.New` itself is not used anywhere in the repo's tests (too heavy: LLM providers, LSP, MCP gateway) and was not started here either.
  - The secondary-branch test drives a **real promotion** (`secApp.PromoteToPrimary`) after tearing the original primary down with `rtPrimary.Cleanup()` (idempotent) and re-acquiring the now-free lock — deterministic, not timing-dependent (`rtSecondary.Watcher.Shutdown(ctx)` stops its background monitoring first so it cannot race the manual promotion). A real `ipc.ping` RPC round-trip against the original primary's ports proves the promoted bus, wired through the exact same `busSetupFunc` `wirePrimary` uses, is actually up and serving.
  - `instanceregistry.Announce`/`Revoke` write to the real, hardcoded `/tmp/pando-instances` (the `instancesDir` override is unexported and only usable from tests inside the `instanceregistry` package itself). Every test uses distinctive, non-UUID instance IDs and revokes them via `t.Cleanup`; verified afterwards that no `wireipc-test-*` entries remained.
- `internal/ipc/runtime/runtime_bootstrap_options_test.go`'s fake unresponsive primary is a real, killable, alive process holding a real flock — `flock --exclusive --no-fork <lockfile> sleep 30` (the `--no-fork` flag is required: without it `flock(1)` forks a child to run the target command and the *parent* PID we could see and kill is not the one actually holding the lock, so killing it never releases it). Its LockInfo JSON (naming that PID) is written by the test process afterwards via a plain, unlocked open — `flock()` is advisory and never blocks ordinary read/write syscalls, only other `flock()` attempts. `TestBootstrapWithOptionsKillsStalePrimaryWhenAllowed` (the "kill is still allowed" control case) had to explicitly reap the killed child (`cmd.Process.Wait()`) before checking liveness: `killStalePrimary` kills it internally, but until *this* test process (the real OS parent) reaps it, the PID is a zombie that `processAlive`'s `kill(pid, 0)` probe still reports as "alive".

## Verification

- `go build ./...`: clean.
- `gofmt -l` on every touched file: clean.
- `go vet ./...` (whole repo): clean.
- `go test -race ./internal/ipc/... ./internal/app ./internal/instanceregistry/...`: all pass, no data races.
- `go test ./cmd ./internal/mesnada/... ./internal/rag/... ./internal/db/...`: all pass.
- `go test ./...` (whole repo): only the 4 known pre-existing failures in `internal/llm/agent` (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged`) — unrelated to this change, pre-existing per the P0 doc.
- Repo-wide `gofmt -l` shows pre-existing formatting issues in several files this change never touched (including the two named in the P0 doc, `internal/rag/kb/types.go` and `internal/rag/code/graph.go`); none of the touched files appear in that list.

### Manual smoke test (isolated)

A scratch binary (`go build -o <scratchpad>/smoke/pando .`) was driven inside `unshare -Urmn` (private user+mount+net namespace) with a tmpfs over `/tmp/pando-instances` and a throwaway `$HOME`/project under the scratchpad — the real `.pando/` and the host's `/tmp/pando-instances` were never touched (verified before/after: unchanged timestamps, no `wireipc-test-*`/smoke entries leaked into the real registry).

Scenario, specifically chosen to validate the previously-missing **serve** secondary/handover wiring: `pando serve --host 127.0.0.1 --port 18765` as primary, `pando acp --cwd <proj> --debug --log-file acp.log` (stdin via a FIFO) as secondary, then SIGTERM the serve process.

- The ACP secondary's log correctly showed `role=secondary` on startup, bound against serve's ports from the lock file.
- SIGTERM to serve → serve exited with status 0 (its existing `http.ErrServerClosed` handling), and the ACP secondary logged `PromoteToPrimary complete` **0.115s** later.
- The lock file after promotion named the ACP instance's PID and **the same ports** serve had bound (proving the promoted bus rebinds the original primary's ports so no other secondary's `DBProxy` needs to change its target).
- The instance registry (namespace-private tmpfs) showed the ACP instance re-announced with `is_primary: true`.
- A `session/new` JSON-RPC call sent to the promoted ACP instance over its FIFO got a well-formed result, and `sqlite3` against `.pando/pando.db` showed the new row (`select count(*) from sessions` = 1) — the promoted instance can actually write, which is exactly what P0 fixed (G1) and P1 now makes reachable through `serve`, not just TUI/ACP.
- No stray `pando` processes were left running after the script exited.

## What remains

- **P2** — `pando mcp-server` on the bootstrap: absolute `cwd`, `BootstrapWithOptions(ctx, workdir, id, Options{ProbeTimeout: 3s, AllowKillStalePrimary: false})`, `wireIPC(..., instanceregistry.ModeMCP, wireOptions{AcceptDelegations: &falseVal})`, signal/stdin-EOF handling on the stdio path, a stdout-clean regression test. `cmd/mcp_server.go` is completely untouched by P1.
- **P3** — role-aware background services: `AppOptions.IPCRole`, `App.startPrimaryServices` (currently a no-op hook per P0), gating the startup code indexer/KB mirror/backfill/memory GC/cron behind it, and removing the ACP-vs-`app.New` duplicate `CronService.Start` call (noted, deliberately not touched here per the task scope).
- **P4–P6** per the plan: other direct-DB entry points (`agui-serve`, `cronjob run`, `kb relink`, `db.ConnectCLI` for project/design), the remaining direct writers on secondaries, and the Phase-7 multi-process Python test suite under `tests/`.
- **G7** (killing a suspended primary) is still an open policy question, untouched by P1.
- The bridge-heartbeat start-before-bus-bind unification (see item 2 above) is a minor, deliberate behavioural change from TUI/ACP's pre-P1 ordering; flagged here in case it ever needs revisiting.

Links: [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/ipc_failover_p0_inplace_promotion.md]]