---
created_at: 2026-09-11T19:44:09.492843163Z
updated_at: 2026-09-11T19:45:33.447087006Z
---
# Change: P2 — `pando mcp-server` on the IPC primary/secondary bootstrap (2026-09-11)

Implements phase **P2** of [[pando/plans/mcp_server_ipc_bootstrap.md]] (§5.3, §7). Builds on [[pando/changes/ipc_wiring_p1_shared_wireipc.md]] (P1's shared `wireIPC`, `BootstrapWithOptions`, `ModeMCP`) and [[pando/fixes/ipc_failover_p0_inplace_promotion.md]] (P0's in-place promotion, ordered handover, canonical workdir). `cmd/mcp_server.go` was completely untouched by P0/P1; this change is the first to touch it. P3 (role-aware background services), P4-P6 and G7 are not started.

## What changed

### 1. `cmd/mcp_server.go` — bootstrap flow

`runMCPServerMode` no longer calls `db.Connect()` directly. The sequence is now:

1. `config.Load(cwd, debug, "")` plus the existing `enableMCPServerFeatures()`/`applyMCPServerFlagOverrides(cmd)` flag handling (unchanged).
2. **Absolute `cwd`**: `--cwd` now does `os.Chdir(cwdFlag)` then *always* re-derives `cwd` via `os.Getwd()` (previously it used the possibly-relative `cwdFlag` string directly when `--cwd` was given). `ipcruntime.BootstrapWithOptions` canonicalises again internally (`filepath.Abs` + `EvalSymlinks`, P0's G6 fix), but `config.Load` and the registry entry now already see the real path too.
3. **`bootstrapMCPServer(ctx, cwd) (*ipcruntime.BootstrapResult, *app.App, func(), error)`** (new): joins the shared bootstrap.
   - `instanceID := uuid.New().String()`.
   - `ipcruntime.BootstrapWithOptions(ctx, cwd, instanceID, ipcruntime.Options{ProbeTimeout: mcpServerProbeTimeout /* 3s */, AllowKillStalePrimary: false})`. mcp-server never kills an unresponsive primary — it is an ephemeral, low-trust process spawned by an editor/agent (Claude Code, Copilot, Cursor...) and must not SIGKILL a user's long-running TUI/desktop/serve instance just because that instance is slow, under a debugger, or SIGSTOPped to answer one liveness probe. On a Bootstrap error (only reachable today when opening the primary DB itself fails — every other internal failure mode already degrades gracefully, per the P1 doc) it logs via `logging.Error` and returns the wrapped error, matching what every other entrypoint (serve/desktop/ACP) already does on a bootstrap failure — there is no DB to serve MCP tools from either way, so no other fallback is meaningful.
   - `app.New(ctx, rt.SQLDB, app.AppOptions{SkipLSP: true, SkipMesnadaServer: true, StartupMode: "mcp", DBQuerier: rt.Querier})` — the only change from before is passing `DBQuerier: rt.Querier`, exactly like ACP's `app.AppOptions{SkipLSP: true, DBQuerier: rt.Querier, StartupMode: "acp"}`. `AppOptions.IPCRole` does not exist yet (P3, plan §5.4 `startPrimaryServices` is still a no-op hook per the P0 doc), so mcp-server passes only what every other migrated entrypoint already passes today.
   - `wireIPC(ctx, rt, pandoApp, instanceID, cwd, instanceregistry.ModeMCP, wireOptions{AcceptDelegations: &acceptDelegations})` with `acceptDelegations := false` — mcp-server is ephemeral (it dies with the client that spawned it) and must never accept a peer delegation regardless of the user's persisted `Mesnada.Delegation.AcceptDelegations` setting.
   - On an `app.New` error, `rt.Cleanup()` runs before returning, so a failed app build never leaks the just-acquired lock/bus/DB.
4. `buildMCPServerTools` is byte-for-byte unchanged — see "Tool behaviour on a secondary" below for why no changes were needed there.

### 2. `cmd/mcp_server.go` — ordered shutdown

- **`shutdownMCPServerOrdered(stopTransports, shutdownApp, unwireIPC, cleanupRuntime func())`** (new): a small function that calls the four closures in order — `stopTransports()` → `shutdownApp()` (`pandoApp.Shutdown`, which starts with `releasePrimaryRole()`: drain the write coordinator, release the IPC lock, announce `instance.shutdown`, close the bus, only *then* run the slower extensions/agent-vcs/LSP shutdown) → `unwireIPC()` (registry revoke) → `cleanupRuntime()` (`rt.Cleanup`, idempotent). `runMCPServerMode` registers exactly one `defer` wrapping this call, so the order is a property of the function body, not of `defer`'s LIFO stacking across several separate `defer` statements — the latter is how ACP/serve/desktop/app already declare their defers today (`rt.Cleanup`, then `pandoApp.Shutdown`, then `wireIPC`'s cleanup — LIFO execution is *cleanup → Shutdown → rt.Cleanup*, i.e. the registry entry is dropped *before* the lock is actually released and `instance.shutdown` is announced). mcp-server's explicit ordering was chosen deliberately per the task: Shutdown's real handover completes first, so the registry entry is only removed once this instance is definitely done acting as primary, closing a small window the other entrypoints still have. This is a one-line, low-risk behavioural difference from ACP/serve/desktop/app, noted here in case someone later wants to unify all five onto the same explicit ordering.
- **`stopMCPTransports(httpSrv, stdioSrv *mesnadaServer.Server)`** (new): calls `Shutdown(ctx)` (10s timeout) on whichever of the two `*mesnadaServer.Server` values is non-nil. For the stdio transport this only releases per-session resources (`cleanupSessions` → `llmtools.UnregisterSessionCache`/`CloseBrowserSession`) — `internal/mesnada/server/server.go`'s `runStdio()` blocks on `bufio.Scanner.Scan()` over `os.Stdin` with no cancellation hook, so a signal-triggered shutdown cannot interrupt an in-flight blocking read; that goroutine is simply abandoned when the process exits after `runMCPServerMode` returns (harmless: the OS reclaims everything on exit, and this is exactly the same limitation the previous "both transports" branch already had for the HTTP-alongside-stdio case).
- **`waitForMCPServerShutdown(sigCtx context.Context, stdioDoneCh, httpErrCh <-chan error) error`** (new): the single `select` that used to be duplicated three ways (`--no-http` had *no* select at all — see below; `--no-stdio` and "both transports" each had their own copy). It reacts to whichever of three triggers fires first:
  - `sigCtx.Done()` (SIGINT/SIGTERM via `signal.NotifyContext`) → logs, returns `nil` (graceful).
  - `stdioDoneCh` receiving `nil` (the client closed stdin — EOF) → logs, returns `nil` (graceful, same as a signal).
  - `stdioDoneCh` receiving a non-nil error (a genuine read/encode failure) → logs, returns that error.
  - `httpErrCh` receiving an error → logs, returns that error.

  A disabled transport's channel is simply never written to, so passing it into the same `select` is safe: that case never fires.
- **Previous bug fixed**: `--no-http` (the scenario this whole plan document and the task's smoke tests are built around) used to `return stdioSrv.Start()` directly — a blocking call with **no signal handling at all**. Neither SIGINT nor SIGTERM had a handler installed, so the OS default action terminated the process immediately: no deferred `pandoApp.Shutdown()`, no lock release via the ordered handover, no registry revoke. (The kernel still freed the flock on process death, so failover eventually recovered via the heartbeat-timeout path, but the ordered handover — draining the coordinator, announcing `instance.shutdown` promptly — never ran.) The rewrite now always launches the stdio transport (when enabled) in a goroutine reporting to `stdioDoneCh`, mirroring how the "both transports enabled" branch already worked, and adds the same signal handling `--no-http` was missing. This also incidentally fixes a smaller pre-existing gap in the "both transports" branch: it only forwarded a *non-nil* stdio error to its select channel, so a graceful stdin EOF while HTTP was also enabled did not trigger any shutdown at all (the process would just wait forever on the now-idle select) — `waitForMCPServerShutdown` treats stdio EOF as a first-class graceful trigger in every transport combination.
- **No `os.Exit` before cleanup**: every return path from `runMCPServerMode` is a plain `return`, so the single deferred `shutdownMCPServerOrdered` call always runs.

### 3. Tests (`cmd/mcp_server_ipc_test.go`, new)

- **`TestMCPServerUsesSharedIPCBootstrap`** — a source-shape test in the same style as `TestEntrypointsUseSharedIPCWiring` (`cmd/root_test.go`), which explicitly excluded `mcp_server.go` pending this phase. Asserts the source contains `ipcruntime.BootstrapWithOptions(`, `AllowKillStalePrimary: false`, `wireIPC(`, `instanceregistry.ModeMCP`, `acceptDelegations := false` and `AcceptDelegations: &acceptDelegations`, and that it no longer calls `db.Connect()` directly.
- **`TestMCPServerBootstrapAndWiringStdoutIsClean`** — the required stdout-cleanliness regression test. It redirects `os.Stdout` to a pipe and drives `ipcruntime.BootstrapWithOptions` (with mcp-server's exact options) plus `wireIPC` (with mcp-server's exact `ModeMCP`/`AcceptDelegations` arguments) against a bare test `*app.App` — mirroring `cmd/ipc_wiring_test.go`'s `bareAppForIPCTest` pattern rather than calling `bootstrapMCPServer` (and therefore real `app.New`) directly, because that file's own doc comment already establishes that `app.New` (LLM providers, LSP, MCP gateway) is deliberately never exercised by this package's tests — it is too heavy, and per the P1 smoke tests can itself reach the network in a config that has embedding/LLM providers configured. Asserts zero bytes reach stdout after a settling sleep.
- **`TestShutdownMCPServerOrdered`** — calls `shutdownMCPServerOrdered` with four order-recording closures and asserts the exact sequence `stop-transports, shutdown-app, unwire-ipc, cleanup-runtime`.
- **`TestWaitForMCPServerShutdown_SignalIsGraceful` / `_StdioEOFIsGraceful` / `_StdioErrorPropagates` / `_HTTPErrorPropagates`** — unit tests for the extracted select logic: a cancelled `sigCtx` and a `nil` off `stdioDoneCh` both return `nil`; a non-nil error off either `stdioDoneCh` or `httpErrCh` is returned unchanged. None of these need cobra flags, a real IPC bootstrap, or a real MCP transport.

All new/updated files: `cmd/mcp_server.go`, `cmd/mcp_server_ipc_test.go`.

## Stdout guarantees

Everything the bootstrap and wiring stack can write goes through `logging.*` (slog, routed to the in-memory writer or `--log-file`, never stdout) or `log.Printf` inside `internal/ipc/bus.go`/`internal/ipc/bridge/bridge.go` (both go to Go's default `log` output, which is stderr unless redirected — allowed for a stdio MCP server). The only intentional stdout writer remains `internal/mesnada/server.Server.runStdio`'s `json.NewEncoder(os.Stdout)`. `TestMCPServerBootstrapAndWiringStdoutIsClean` covers the bootstrap+wiring half; the smoke test below covers the full process end-to-end, including `buildMCPServerTools` and the actual JSON-RPC exchange.

## Tool behaviour on a secondary

No changes were needed in `buildMCPServerTools` or in any tool implementation. This was already correct as of P0:

- **Writes** (`kb_add_document`, `remember`/`recall`/`forget`, `save_event`, the code index tools, `kb_delete_document`): the KB/Events/Code stores all call `Forward`/`IsRemote` (P0's fix for G1) instead of `proxy != nil`, so on a secondary they transparently forward to the primary over the write coordinator, and after a promotion they transparently switch to direct writes without any code on mcp-server's side needing to know which role it is in.
  - One nuance surfaced and verified by the smoke test below: `remember`/`forget`'s **keyed** upsert path (`internal/rag/kb/memory.go`) has two branches. The *direct* branch (this instance is not remote — i.e. it is the primary, or was promoted) does a raw `INSERT ... ON CONFLICT` and **never calls the embedder**. The *proxied* branch (`s.proxy.IsRemote()` true) delegates to `AddDocument`/`UpdateDocument`, which **always** computes chunk embeddings locally before forwarding the write (by design, per `pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md`: embedding must happen before opening a write transaction so it cannot hold the busy_timeout hostage). So a `remember`-with-key call on a secondary will fail if no embedding provider is reachable, independent of whether the actual primary is reachable — this is expected, not a regression, and is exactly the behaviour the smoke test's scenario (c) uses as a promotion probe (see below). `forget`/`kb_delete_document`'s delete path never touches the embedder in either role, which is why it was used as the primary "does a write really land" check in scenario (b).
- **Reads** (`kb_search_documents`, `code_hybrid_search`, etc.): unchanged — they already read from the local connection (RW-but-small-pool on a secondary, full RW pool on the primary) regardless of role.
- **Pool-exhaustion concern** ([[pando/fixes/sqlite-connection-pool-exhaustion-mcp-server.md]]): does not apply differently under P2. A secondary's pool is `db.ConnectRWSecondary()` (1 connection), which is what mcp-server now gets automatically via `rt.SQLDB` when it is not primary; there is no migration-running or multi-connection behaviour change for mcp-server specifically. Migrations are still only run by whoever ends up primary (`ipcruntime.BootstrapWithOptions`'s primary branch, or `db.PromoteToPrimaryPool` on a promotion) — mcp-server never runs migrations on its own again now that it goes through the shared bootstrap instead of its own `db.Connect()` call.
- No direct-writer-only tool (`history`/`project`/`mcpgateway`/`design` — the P5 "remaining direct writers" list) is reachable through `buildMCPServerTools` today except the opt-in `--file-tools-write` group (`write`/`edit`/`patch`, off by default), which uses `appSvc.History` (`history.NewService(rawQ, conn)`, a direct writer via `WithTx`). This was not exercised by the smoke test (it requires an explicit opt-in flag) and is left as a known P5 gap, unchanged by this phase.

## Latency

Both scenarios measured *process start of `pando mcp-server --no-http` to receiving the `initialize` JSON-RPC response*, inside the isolated smoke test below (debug build, cold config, no LLM providers configured):

- **(a) mcp-server as primary** (fresh lock, no probe needed): **0.524s**. (Also independently reproduced at 0.544s on a re-run.)
- **(b) mcp-server as secondary** (existing ACP primary already up, one `ipc.ping` probe plus opening the 1-conn secondary DB): **0.485s**.

Both numbers are dominated by `app.New` (LLM/tool subsystem init, project manager, theme/icon setup, etc.) exactly as the plan predicted — the secondary case is not meaningfully slower than the primary case in this measurement, consistent with the design note that a secondary skips the startup indexer/KB-import/backfill work a primary does (P3 will make this an explicit, enforced skip rather than an implicit one, since P3 has not moved those services behind a role gate yet — see "What remains"). Answering MCP `initialize` before `app.New` finishes was **not** implemented, per the plan's explicit "optional, not required" framing; it remains a follow-up (see below).

## Smoke test (isolated, scratch binary — never touched the real `.pando/` or `/tmp/pando-instances`)

Driven by a Python harness (`mcp_smoke.py`) inside `unshare -Urmn` (private user+mount+net namespace, loopback brought up manually) with a tmpfs mounted over `/tmp/pando-instances` and a throwaway `HOME`/project dir per scenario under the scratchpad. A pre-seeded project-local `.pando.toml` turns on `Remembrances.Enabled`/`MemoryEnabled` (both default `false` on a fresh install — [[pando/fixes/no-hardcoded-provider-and-agent-model-defaults.md]] — so `remember`/`forget` are not even registered without it) with `DocumentEmbeddingProvider = 'ollama'`/`CodeEmbeddingProvider = 'ollama'` purely so `NewRemembrancesServiceWithProxy` constructs successfully; no tool call in the test ever actually reaches an embedding endpoint (the namespace has no route to the host's Ollama, verified in passing since `ps` showed the group is present on this machine).

- **(a) `pando mcp-server --no-http` first (primary) → `pando acp` (secondary) → close mcp-server's stdin (EOF)**:
  - `initialize` answered in 0.524s.
  - `tools/list` returned 28 tools including `remember`.
  - `remember` (key `smoke.a`) succeeded; verified via a read-only `sqlite3` query against `.pando/pando.db` that the row landed.
  - ACP secondary bootstrapped and answered its own `initialize`.
  - mcp-server's stdin closed → **exited with code 0 in 0.114s**, confirming the ordered shutdown actually drains/releases/announces/closes instead of hanging or being killed.
  - ACP secondary logged a promotion **0.114s** after the EOF; `session/new` against the now-primary ACP wrote a real session row (`sessions` count 0→1).
  - Final lock file: 0 bytes (released).
- **(b) `pando acp` first (primary), `pando mcp-server --no-http` (secondary)**:
  - A memory row (`smoke.b`) was first seeded by briefly running mcp-server as primary (direct, embedding-free keyed upsert) before it exited cleanly, then ACP took over as the real primary for this scenario.
  - mcp-server joined as secondary; `initialize` answered in 0.485s.
  - `pando ipc status --path <proj>` confirmed 2 known instances, the ACP one marked primary.
  - `forget` (key `smoke.b`) on the **secondary** mcp-server succeeded ("Memory deleted: ..."), and a follow-up `sqlite3` read confirmed **0** rows remained for that key in the (single, shared) `pando.db` — the delete was forwarded through the write coordinator to the ACP primary and applied there.
  - mcp-server's entire captured raw stdout (2 lines: the `initialize` and `forget` responses) parsed as valid `{"jsonrpc":"2.0", ...}` JSON-RPC with **zero** non-JSON-RPC bytes.
  - mcp-server exited 0 after its own stdin EOF; ACP exited cleanly after its own EOF.
- **(c) `pando acp` (primary) + `pando mcp-server --no-http` (secondary), then SIGTERM the primary**:
  - Before the SIGTERM, a keyed `remember` call on the still-secondary mcp-server correctly **failed** (no reachable embedding provider — see "Tool behaviour on a secondary" above), confirming the pre-promotion state behaves as expected rather than silently succeeding for the wrong reason.
  - `SIGTERM` to the ACP primary → **exited 0 in 0.114s** (its P1 signal handler drives the same ordered handover).
  - Polling `remember` with a fresh key every 0.5s, mcp-server's write **succeeded 0.134s after the SIGTERM** — the moment it flips from erroring (still-remote, embedder unreachable) to succeeding (promoted, direct path, no embedder needed) is itself the promotion signal.
  - `pando ipc status --path <proj>` confirmed the lock/registry now show mcp-server's own PID as primary with `Mode: mcp` (the `ModeMCP` display string added in P1's `internal/tui/components/instances/view.go` change, now actually exercised end-to-end for the first time).
  - A `sqlite3` read confirmed the post-promotion row landed. The promoted mcp-server then exited 0 after its own stdin EOF.
- **Extra check (not in the task's three named scenarios, added for completeness): mcp-server as primary, SIGTERM'd directly.** `remember` succeeded, then `SIGTERM` → exited 0 in 0.164s, lock file 0 bytes, stdout still pure JSON-RPC (2 lines), and the row written before the signal was still present afterward — confirming P2's own signal handling (not just EOF) works when mcp-server itself is the primary being torn down.
- No stray processes were left running after any scenario (`pgrep` against the scratch binary path came back empty each time), and the real `/www/MCP/Pando/pando/.pando/ipc.lock` and the real `/tmp/pando-instances` registry entries were confirmed unchanged before/after the whole run.

## Verification
- `go build ./...`: clean.
- `gofmt -l` on `cmd/mcp_server.go`, `cmd/mcp_server_ipc_test.go`, `cmd/ipc_wiring.go`, `cmd/ipc_wiring_test.go`: clean.
- `go vet ./cmd/... ./internal/ipc/... ./internal/app/...`: clean.
- `go test -race ./internal/ipc/... ./internal/app ./cmd`: all pass, no data races.
- `go test ./internal/mesnada/... ./internal/rag/... ./internal/db/...`: all pass.
- `go test ./...` (whole repo): only the 4 known pre-existing failures in `internal/llm/agent` (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged`) — unrelated to this change.
- Repo-wide `gofmt -l` still only flags the same pre-existing files noted in the P0/P1 docs (`internal/rag/kb/types.go`, `internal/rag/code/graph.go`, plus several others never touched by IPC work); none of the files this change touched appear in that list.

## What remains
- **P3** — role-aware background services: `AppOptions.IPCRole`, `App.startPrimaryServices` (currently a no-op hook per P0), gating the startup code indexer/KB mirror/backfill/memory GC/cron behind it. Until P3 lands, mcp-server (like every other entrypoint) runs the full set of background services regardless of role — nothing broke because of that in this phase, so it was left alone per the task's explicit scope.
- **P4-P6** per the plan: other direct-DB entry points (`agui-serve`, `cronjob run`, `kb relink`, `db.ConnectCLI` for project/design), the remaining direct writers on secondaries (`history`, `project`, `mcpgateway`, `design` — see "Tool behaviour on a secondary" above for the one mcp-server-reachable instance, `--file-tools-write`), and the Phase-6/7 multi-process Python test suite under `tests/`.
- **G7** (killing a suspended primary) is still an open policy question, untouched here — moot for mcp-server specifically since it always passes `AllowKillStalePrimary: false`.
- **Optional latency follow-up** (plan §5.3, explicitly not required): answering MCP `initialize` before `app.New` finishes. Not implemented; `app.New` remains the dominant cost in both measured scenarios above.
- Minor, unrelated doc-drift noticed while writing the smoke test: `buildMCPServerTools`'s comment claims `MemoryEnabled` "is turned on in server mode" by `enableMCPServerFeatures`, but that function does not actually set it — `Remembrances.MemoryEnabled` still defaults to `false` like `Remembrances.Enabled` (`internal/config/config.go`'s `applyDefaultValues`). Not fixed here (out of scope for P2; flagged for whoever next touches `enableMCPServerFeatures` or that comment).

Links: [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/changes/ipc_wiring_p1_shared_wireipc.md]], [[pando/fixes/ipc_failover_p0_inplace_promotion.md]], [[pando/plans/sqlite_contention_fix_roadmap.md]]
