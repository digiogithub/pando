---
created_at: 2026-09-11T22:21:51.663644766Z
updated_at: 2026-09-11T22:21:51.663644766Z
tags:
    - change
    - ipc
    - sqlite
    - failover
    - design
    - mcpgateway
    - agui
---
# Change: P5 — remaining direct writers on secondaries, and writes that survive a handover (2026-09-12)

Implements phase **P5** of [[pando/plans/mcp_server_ipc_bootstrap.md]] (§1.3 "direct writers that skip the proxy", §7 P5, §9). Builds on [[pando/fixes/ipc_failover_p0_inplace_promotion.md]] (in-place promotion, `DBProxy.Promote`/`Forward`, ordered handover), [[pando/changes/ipc_wiring_p1_shared_wireipc.md]], [[pando/changes/mcp_server_ipc_bootstrap_p2.md]], [[pando/changes/ipc_role_aware_services_p3.md]] and [[pando/changes/ipc_other_entrypoints_p4.md]] (which flagged agui-serve's thread store as a P5 item). P6 (the multi-process Python suite) and G7 (the kill policy) are not started.

The SQLite-contention context is [[pando/analysis/sqlite_locked_interrupted_errors.md]] and [[pando/plans/sqlite_contention_fix_roadmap.md]]; the analysis items the plan listed under P5 (`BEGIN IMMEDIATE`, the events session-id index, the incremental session indexer, retryable `ErrCodeBusy`) were already done in [[pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md]] and [[pando/features/session_index_incremental_per_message.md]].

## Problem

On an IPC secondary, sqlc writes go through `DBProxy.directOrProxy` and remembrances writes always forward. Everything else wrote straight to `app.rwConn`, which on a secondary is the **1-connection, 200 ms busy-timeout** pool: as soon as the primary held the write lock for longer than 200 ms, those writes failed.

Two further gaps:
- A write forwarded to the primary exactly while it hands over (drain → lock release → `instance.shutdown` → a secondary promotes) failed after 3 short retries and was never retried against the new primary (an explicit P0 risk).
- The primary's handover refusals (`writecoordinator.ErrDraining`) crossed the IPC boundary as plain text and were classified `ErrCodeInternal`, i.e. **not retryable**, even though the write had provably not been applied.

## Scope A — the direct writers, and the decision for each

Every writer was **routed through the proxy** (option 1). None was left as "direct-first plus a retry", because each is either hot (file history: one row per agent file edit), bursty (the MCP catalog: every tool of every server at each startup), or multi-statement (design).

| Writer | Decision | Notes |
|---|---|---|
| `history.NewService` (`internal/history/file.go`) | sqlc querier | Now takes `db.Querier` and is built with the app's querier. Its `WithTx` wrapped a **single** INSERT — with `_txlock=immediate` that is exactly autocommit — so the transaction was dropped rather than trying to span the proxy. The UNIQUE-constraint version retry still works: the primary's error text survives the round trip. |
| `project.NewService` (`internal/project/service.go`) | sqlc querier | Now takes `db.Querier`. `Rename` needs `UpdateProjectName`, which is hand-written on `*db.Queries` and absent from the generated `db.Querier`, so `DBProxy.UpdateProjectName` + a `dispatchWrite` case were added (the handler type-asserts the primary's querier to `ProjectNameUpdater`). |
| `mcpgateway` registry + stats (`registry.go`, `stats.go`, `gateway.go`) | registered statements | `UpsertTool`, `DeleteServer`, `RecordUsage` run through a `dbproxy.SQLWriter`. `Gateway.SetWriteProxy` wires it explicitly; a bare `NewRegistry(db)` (the WebUI fallback in `handlers_config.go`) finds the proxy through the pool binding. |
| `design` store/provider (`store.go`, `provider.go`, `service.go`) | registered statements, batched | All 8 write sites. `AddVersion` (2 statements) and `ReplaceNodes` (1 + N) are sent as one batch and run in one transaction on whichever side executes them. `Provider.SetWriteProxy` hands every session-bound service a proxy-aware store. |
| AG-UI thread map (`internal/agui/threads.go`) | registered statements | Found by P4. It permanently degrades to memory-only after the *first* persistence failure, so one lock collision on a secondary used to disable thread persistence for the life of the process. |
| `SeedFromGlobal` / `RegisterSelfAsGlobalProject` | covered | They use the project service, so they are fixed by the project change (P3 had listed them as a P5 item). |

Nothing else in `app.New` writes on the raw connection: `rawQ` is gone, and the remaining `conn` uses are the remembrances service (already proxy-aware), the gateway and the design provider (both now wired), plus reads.

### `dbproxy.SQLWriter` and registered statements (new `internal/ipc/dbproxy/statements.go`)

The three writers above hold a `*sql.DB` and run their own SQL, so they cannot use the sqlc `db.Querier` path. They now use:

- **`RegisterStatement(name, query) Statement`** — a package-level registry, populated from `var` initialisers, so both roles of the same binary hold the same map. **Only the name and the arguments cross IPC**; the primary looks the SQL up in its own registry, so a peer can never make the primary execute arbitrary SQL. This was the reason for rejecting a generic "exec this SQL" RPC.
- **`SQLWriter.Exec` / `ExecBatch`** — direct on the local pool first; on SQLITE_BUSY/LOCKED the batch is forwarded as the `ExecStatements` write method. A batch is one transaction on either side (prepared once per repeated statement), so design's multi-statement writes stay atomic; the plan's "a transaction cannot span the proxy" constraint is respected by sending the whole batch, not by splitting it.
- **Typed argument encoding** (`{"t":"int|str|bytes|bool|float|time|null","v":…}`) so the primary binds exactly what the secondary would have bound: `time.Time` stays a `time.Time` (the gateway's `called_at` is compared against `time.Time` cutoffs, and the ncruces driver formats it), an int stays an int, and `driver.Valuer` (e.g. `sql.NullInt64`) is resolved before encoding.
- **`RegisterStatementExecutor(*sql.DB)`** — the primary's pool, registered next to the remembrances dispatcher (`App.registerRemembrancesDispatcher`), so a promoted secondary registers it before starting its bus.
- **Version skew / unwired primary:** an unknown statement (or no executor) answers `unknown write method`, which the secondary maps to `ErrCodeMethodNotFound` and handles by falling back to a **bounded direct retry** on its own pool (5 attempts, 100 ms doubling) — the pre-P5 behaviour plus patience.
- **`BindPool(db, proxy)` / `ProxyForPool(db)`** — `app.New` binds the pool it was given to its `DBProxy`, so writers built from the shared `*sql.DB` alone (the AG-UI adapter, whose `Deps` deliberately never sees `*app.App`; the WebUI's MCP catalog fallback) follow the topology without threading a proxy through the entrypoints. This avoided editing `cmd/agui_serve.go`, which a parallel task was changing.

## Scope B — forwarded writes during a handover

**`DBProxy.forwardWithHandoverRetry`** (`internal/ipc/dbproxy/proxy.go`) now wraps every forwarded write (`directOrProxy`, `directOrProxyVoid`, `ProxyWriteWithResult`, `WriteWithRetry`, and therefore `Forward`/`ForwardWithResult`, the remembrances path). Per error class:

- **`ErrNotRemote`** — this instance was promoted meanwhile: returned at once, and every caller then writes locally (P0's `Promote()` passthrough).
- **Unreachable** (never delivered) and the new **`ErrCodeUnavailable`** (the primary refused it while draining) — nothing was applied, so the write is re-sent until the **handover wait** expires (`DefaultHandoverWait`, 20 s; `SetHandoverWait` per proxy). Backoff is jittered 50 ms → 1 s, and `refreshPrimaryAddr` re-reads the endpoint before each retry.
- **Busy** — re-sent, capped at 3 attempts as before (the primary's own 10 s busy timeout already expired).
- **Timeout / lost response** (outcome unknown) — re-sent only for **void** writes (idempotent replaces/deletes/updates), capped at 3, matching the legacy loop. Typed writes (mostly creates) are not re-sent: a re-sent create that had in fact been applied would fail with a conflict after succeeding.
- Anything else returns immediately. When the wait is exhausted the last error is returned wrapped in a message that says so, and `errors.As` still finds the `*WriteError`.

Supporting changes:
- **`ErrCodeUnavailable`** (retryable) in `internal/ipc/dbproxy/errors.go`, mapped from the coordinator's texts: `draining for primary handover` and `coordinator is shut down` (both refused before queueing). `coordinator shut down while waiting for result` is ambiguous, so it maps to `ErrCodeTimeout`. `ClassifyError` was exported so `internal/ipc/writecoordinator` can pin those texts in its own test — the two packages cannot import each other's constants (cycle).
- **`internal/ipc/errors.go` / `client.go`:** a send failure is now tagged `ErrConnectionFailed` and a failed receive the new `ErrResponseLost` (an ambiguous outcome), via a wrapper that keeps the original message so existing text matching is unaffected. Before, both mapped to `ErrCodeInternal` and were never retried — the most likely error shape during a handover.
- **`Client.ForgetEndpoint`**: after an `Unavailable` answer the cached DEALER is dropped, because the ROUTER that answered is about to close and the replacement on the same port is a different socket.
- **Endpoint re-resolution:** `DBProxy.rpcAddr` is now atomic, with `SetPrimaryResolver`. `App.SetIPCSecondaryContext` installs `lockFilePrimaryResolver(workdir, instanceID)`, which reads the IPC lock file and ignores an empty file or one naming this instance. After P0's canonical workdir the ports are normally identical, so this is mostly retry-and-re-dial; it also covers a primary that bound elsewhere.

## Files and symbols

- **New:** `internal/ipc/dbproxy/statements.go` (`Statement`, `StmtCall`, `RegisterStatement`, `SQLWriter`, `RegisterStatementExecutor`, `dispatchExecStatements`, `execStatementsLocal`, `execStatementsWithBusyRetry`, `BindPool`, `ProxyForPool`, the wire types), `internal/ipc/dbproxy/proxytest/proxytest.go` (test-only topology helper).
- `internal/ipc/dbproxy/proxy.go`: atomic `rpcAddr`, `PrimaryResolver`/`SetPrimaryResolver`, `SetHandoverWait`, `refreshPrimaryAddr`, `forwardWithHandoverRetry`, `proxyWriteRetry`, `jitterDuration`, `UpdateProjectName`, `ProjectNameUpdater`.
- `internal/ipc/dbproxy/errors.go`: `ErrCodeUnavailable`, the coordinator text constants, `ClassifyError`, new mappings.
- `internal/ipc/dbproxy/handlers.go`: `UpdateProjectName` and `ExecStatements` dispatch cases.
- `internal/ipc/errors.go`, `internal/ipc/client.go`: `ErrResponseLost`, `classify`, `ForgetEndpoint`.
- `internal/app/app.go`: `New` (single querier, `writeProxy`, `BindPool`, `history`/`project` on the querier, `gw.SetWriteProxy`, `designProvider.SetWriteProxy`), `registerRemembrancesDispatcher` (also registers the statement executor), `SetIPCSecondaryContext` + `lockFilePrimaryResolver`.
- `internal/history/file.go`, `internal/project/service.go`, `internal/mcpgateway/{gateway,registry,stats}.go`, `internal/design/{store,provider,service}.go`, `internal/agui/threads.go`.
- Tests: `internal/ipc/dbproxy/{statements_test.go,handover_test.go}`, `internal/ipc/writecoordinator/handover_text_test.go`, `internal/history/proxy_writes_test.go`, `internal/project/service_proxy_test.go`, `internal/mcpgateway/proxy_writes_test.go`, `internal/design/proxy_writes_test.go`, `internal/agui/threads_proxy_test.go`; `internal/app/promote_test.go` and `cmd/ipc_wiring_test.go` also reset the new global.

## Tests

`proxytest.New(t)` builds a real topology — migrated DB file, primary pool + write coordinator + statement executor on a real ZMQ bus, secondary 1-connection pool with a `DBProxy` — plus `HoldWriteLock(d)` (BEGIN IMMEDIATE on the primary) and `StartPrimary`/`DrainPrimary`/`StopPrimary`. Each writer's test performs its writes while the lock is held for 0.4–0.7 s (well past the secondary's 200 ms timeout) and asserts both success and that the call waited, which a direct write could not have done.

- **dbproxy:** argument round trip for every supported type (including `time.Time` identity in both instant and formatted text, and `driver.Valuer`), duplicate-name panic, direct/atomic-batch behaviour with no proxy, unknown statement and missing executor → method-not-found (also after a text round trip), pool binding, handover text classification.
- **Handover (real bus):** a typed and a void forwarded write across a graceful handover (drain → stop → new primary on the same ports 500/400 ms later) succeed and are applied by the new primary; the bound is enforced when no primary returns (error still carries the retryable `*WriteError`); a promotion during the wait switches the caller to a local write; the resolver re-points the proxy to a primary on a different endpoint; `SQLWriter` forwards under contention, falls back to a direct retry when the primary has no executor, and writes directly after `Promote`.
- **writecoordinator:** `Submit` while draining and after `Shutdown` classify as `ErrCodeUnavailable`, pinning the texts across the package boundary.
- **Per writer:** history (create/version/update/delete + the unchanged primary versioning path), project (create/rename/status/delete through the proxy), gateway (upsert/usage/favourites, and a pool-bound registry), design (create, `AddVersion`, `ReplaceNodes` with styles, rows-affected → `ErrNotFound` semantics preserved across the proxy, plus the direct path), AG-UI threads (persisted through the proxy without degrading, re-read by a fresh store).

## Verification

- `go build ./...` clean; `go vet` clean on `./internal/ipc/... ./internal/history ./internal/project ./internal/mcpgateway ./internal/design ./internal/agui ./internal/app`; `gofmt -l` clean on touched packages (`cmd/test_ollama_main/main.go` is pre-existing).
- `go test -race -count=1 ./internal/ipc/... ./internal/history/... ./internal/project/... ./internal/mcpgateway/... ./internal/design/... ./internal/agui/... ./internal/app/... ./cmd/... ./internal/rag/... ./internal/db/...`: all ok, no races.
- `go test -count=1 ./internal/api/... ./internal/mesnada/...`: all ok.
- The 4 known pre-existing `internal/llm/agent` failures were not re-run; unrelated.

### Isolated smoke test (scratch binary; the real `.pando/` and `/tmp/pando-instances` were never touched)

`smoke5/run_p5_smoke.sh` + `p5_smoke.py` in the session scratchpad, inside `unshare -Urmn` (private user/mount/net namespace, loopback only) with a tmpfs over `/tmp/pando-instances`, a throwaway HOME and project, and `[Data] Directory` set explicitly in the scratch `.pando.toml`.

Processes: A `pando acp` (primary), B `pando acp` (secondary, a promotion candidate), C `pando mcp-server --no-http` (secondary), D `pando serve` (secondary, REST).

1. **Contention.** The script held the SQLite write lock for 1.5 s. While held: D created a project (201) and renamed it (200) through its REST API — `project.Service` writes, including the new forwarded `UpdateProjectName` — and C deleted a seeded memory row (`forget`, a remembrances forward). All succeeded; the project create returned after 1.50 s, i.e. it waited for the primary instead of failing on the 200 ms timeout. Both rows were verified in the database.
2. **Handover.** A was stopped with stdin EOF while C issued a remembrances write. A exited 0, **B** won the promotion, and C's write — issued into the handover window — succeeded and was applied by the new primary. (An earlier run in which C itself was promoted also passed, exercising the `ErrNotRemote` → local-write branch.)
3. Teardown: D, C and B all exited 0, no stray processes.

All 17 checks PASS. The first run failed only on the harness: `remember` embeds its content and this namespace has no route to an embedding provider, so the seeded-row + `forget` pattern from the P2 smoke is used instead. No code changed because of it.

## Deviations from the plan sketch

- The plan allowed "keep direct-first plus a bounded retry, documented as safe" for the small writers. Nothing was left that way: the named-statement writer made option 1 cheap enough for all of them, and the AG-UI store's permanent degradation made a retry-only fix unattractive.
- Scope B was implemented as pure retry-with-backoff plus lock-file re-resolution. Reacting to `instance.promoted` was not needed: the promoted instance writes the lock file before binding, the ports are normally identical, and the failover watcher already consumes that event.
- `dbproxy.ClassifyError` was exported, and two `internal/ipc` error sentinels added, which the sketch did not anticipate; both were needed for handover errors to be classified at all.

## Follow-ups and risks

- **Ambiguous timeouts remain ambiguous.** A void write that timed out may be applied twice (idempotent by construction); a typed write is never re-sent, so a create whose response was lost surfaces as a failure even though the row exists. Unchanged from before P5, now documented.
- **A secondary with no primary at all** (unresponsive primary, `AllowKillStalePrimary=false`) now fails forwarded writes after ~20 s rather than ~1 s. Bounded and deliberate, but an interactive MCP tool call can block that long in that state.
- **`DefaultWriteTimeouts.Default` is 5 s** while the primary's busy timeout is 10 s: a forwarded write that queues behind a long primary write can time out before the primary gives up. Pre-existing; the `Long` (30 s) timeout is used for statement batches.
- **Head-of-line blocking** in the single write coordinator now also carries design/gateway/AG-UI batches. `ReplaceNodes` on a large render is the biggest of them.
- **File history on a secondary is not covered by the smoke test**: no surface writes file versions without a real agent run. It is covered by unit tests.
- The statement registry is global and keyed by name; two packages registering the same name with different SQL panic at init. Intentional (it is a programming error), but it means statement names must stay unique across packages — the current ones are prefixed with their package.

Links: [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/ipc_failover_p0_inplace_promotion.md]], [[pando/changes/ipc_wiring_p1_shared_wireipc.md]], [[pando/changes/mcp_server_ipc_bootstrap_p2.md]], [[pando/changes/ipc_role_aware_services_p3.md]], [[pando/changes/ipc_other_entrypoints_p4.md]], [[pando/analysis/sqlite_locked_interrupted_errors.md]], [[pando/plans/sqlite_contention_fix_roadmap.md]]
