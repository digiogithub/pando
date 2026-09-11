---
created_at: 2026-09-11T12:03:28.739559473Z
updated_at: 2026-09-11T12:03:28.739559473Z
tags:
    - analysis
    - sqlite
    - ios-hang
---
# SQLite "database is locked" / "interrupted" errors — root cause analysis (2026-09-11)

Context: telemetry from `pando acp` (pid 206790, Zed stdio, IPC primary per `.pando/ipc.lock`) plus 2 mesnada `pando-cli` subagents (`pando --yolo -p ...`, IPC secondaries, telemetry mode "tui"), `pando mcp-server --no-http` (11:29, 11:46) and Pando desktop (secondary, 11:43:37), all on `/www/MCP/Pando/pando/.pando/data/pando.db` (1.97 GB, WAL 37 MB, journal_mode=wal). Related: [[pando/analysis/remembrances-single-writer-proxy-gap-2026-05-27.md]], [[pando/plans/remembrances_ipc_proxy_implementation_plan.md]], [[pando/plans/inter_instance_ipc_plan.md]], [[pando/analysis/telemetry_acp_hang_analysis.md]].

## Driver and connection settings
- Driver `github.com/ncruces/go-sqlite3` v0.25.0 (WASM). Plain path DSN, no `_pragma`, no `_txlock` (`internal/db/connect.go:99`).
- The driver sets `BusyTimeout(time.Minute)` on EVERY new connection when the DSN has no `_pragma` (driver.go:247-251). So primary / mcp-server pools effectively have busy_timeout = 60 s on all conns.
- `Connect()` (primary, mcp-server, cronjob, agui, db/kb/project CLIs): MaxOpenConns 8, MaxIdle 4; PRAGMAs (`foreign_keys`, `cache_size`, `synchronous`) run via `db.Exec` → applied to ONE pooled connection only (connect.go:120-134). journal_mode=WAL is persistent, so OK; foreign_keys/synchronous are not per-pool.
- `ConnectRWSecondary()` (all IPC secondaries: TUI/-p subagents, desktop, serve, app, secondary ACP): MaxOpenConns 1 and `PRAGMA busy_timeout = 200` on that single conn (connect.go:73-85). A re-opened conn would revert to the driver default 60 s.
- `_txlock` default = deferred: `BEGIN ` (driver.go:357). Every `db.BeginTx(ctx,nil)` in rag (events/kb/code/chunk) is DEFERRED.
- Remembrances (KB, events, code index) live in the SAME `pando.db` and share the SAME `*sql.DB` pool as sessions/messages (`app.go:346` `NewRemembrancesServiceWithProxy(conn, ...)`).
- ctx → `SetInterrupt(ctx)` on every Exec/Query/Begin (driver.go:244,365,396,422,486,765). Cancelling the ctx makes the progress handler interrupt the statement ("sqlite3: interrupted") and also aborts busy-waits (`timeoutCallback` returns 0 when `c.interrupt.Err()!=nil`, conn.go:375-392). `sqlite3.Error.Is` does NOT match `context.Canceled` (error.go:53).

## Cause 1 — "sqlite3: interrupted" (11:16:19): context-enrichment loop reads only the FIRST event of the run channel
- `agentLoopEnricher.runLoop` (`internal/app/context_enricher_agent.go:166-196`) does `result = <-done` once and treats it as the final result. But `agent.runInternal` streams many events into the same channel before the final one: ContentDelta/ThinkingDelta/ToolCall (`agent.go:1859,1868,1877`), ToolResult (`1675+`), TokenUsage (`2044`), SystemMessage (`871`), steering (`729,741`), final result only at `agent.go:978`.
- The first intermediate event has `Error==nil` and empty `Message` → warn `enrichment loop produced no assistant message` (line 191) → `runLoop` returns → `defer cancel()` cancels `runCtx` → the enrichment agent's `genCtx` is cancelled while it is inside `a.messages.Create(ctx, ...)` in `streamAndHandleEvents` (`agent.go:1594-1598`) → ncruces interrupts the INSERT → `failed to create assistant message: sqlite3: interrupted`, wrapped at `agent.go:1257` and logged via `logging.ErrorPersist` (`agent.go:956`) because the sqlite error is not `context.Canceled`.
- Not lock contention per se; it is a logic bug (every enrichment run is effectively aborted after its first streamed event; the enriched context is always lost → fallback path). Contention can make the Create slower and widen the window.

## Cause 2 — `replace session events: events: fts delete: database is locked` (11:19:37–11:23:30, ~15x)
- Subagents are secondaries (`cmd/root.go:217-245`, `DBQuerier: rt.Querier` = DBProxy) → their `remembrancesProxy` is set → `ReplaceSessionEvents` is forwarded via IPC `WriteWithRetry` (`events.go:156-163`) → primary's writecoordinator goroutine → `RemembrancesWriteDispatcher` (`dispatcher.go:159-167`) → primary's EventStore with proxy==nil → direct path.
- Direct path is a DEFERRED tx that first READS then WRITES: `deleteSessionEventsTx` SELECTs all rows `WHERE subject='session' AND json_extract(metadata,'$.session_id')=?` (events.go:229-256; index only on subject, 46,027/46,064 rows are subject=session → scans all + json_extract + reads content), then `INSERT INTO events_fts ... 'delete'` (events.go:259-265).
- SQLite rule: upgrading a read txn to a write txn does NOT invoke the busy handler (btree only retries when `inTransaction==TRANS_NONE`); in WAL a stale snapshot gives SQLITE_BUSY_SNAPSHOT immediately. busy_timeout (60 s) is therefore irrelevant: any commit by another connection during the multi-second scan → instant "database is locked" on the first write (the fts delete).
- Concurrent committers at that time: the subagents themselves write sessions/messages DIRECTLY first (`DBProxy.directOrProxy` tries the local RW conn before proxying, proxy.go:234-275) — one `messages.Update` per streamed delta (`agent.go` ~1862-1881); the ACP primary's own agent writes; other queued coordinator jobs.
- Write amplification: session indexer re-embeds and replaces the WHOLE session transcript every 1.2 s debounce after each message event (`remembrances_indexer.go:35,65-72,150`).
- Error is `ErrCodeInternal` → not retryable (`errors.go:38-72`), so it surfaces immediately in the subagent logs.

## Cause 3 — `code: delete old symbols: database is locked` for `.pando.toml` (11:44:40)
- Text is the NON-proxied `IndexFile` path (`indexer.go:434-446`; the dispatcher path would say "delete old symbols direct", indexer.go:1905). So it ran in a process whose code indexer has no proxy: the ACP primary's own watcher, or a `pando mcp-server`.
- `pando mcp-server` bypasses IPC entirely: `db.Connect()` (full RW, runs goose migrations) + `app.New` without DBQuerier (`cmd/mcp_server.go:107-119`), so it runs its own code watcher, session indexer, KB sync, KB link backfill, all writing directly. Same for `cronjob`, `agui_serve`, `kb`, `project`, `design`, `db` CLIs.
- Every process with remembrances watches the project (`remembrances_code.go:16-117`); a `.pando.toml` write (config save, e.g. desktop startup/settings) fires fsnotify in all of them → concurrent reindex of the same file.
- The first statement in the tx is a write, so the busy handler applies (60 s). The failure 63 s after desktop start (11:43:37 → 11:44:40) matches "a write lock held > 60 s" (or a writer on a 200 ms secondary conn). Candidate long holders: `relinkBatch` KB link backfill (single deferred tx read→write over a batch, `backfill.go:157-200`) and KB auto-import in mcp-server/primary processes; coordinator-executed `CodeIndexFile` bursts from the new desktop instance; `DELETE FROM kb_links` full clears.
- Note `embedSymbols` does network embedding calls between per-symbol autocommit UPDATEs (`indexer.go:~560-575`); when executed via the dispatcher inside the single writecoordinator goroutine, embedding latency blocks ALL queued IPC writes (incl. subagent message writes falling back to proxy).

## Cause 4 — `agentvcs scanner: hash failed` — unrelated
`.antigravitycli/3c650646-...json` is a dangling symlink to `~/.gemini/config/projects/...` (target missing). `filepath.Walk` uses Lstat, then `hashFile` `os.Open` follows the link → ENOENT (`scanner.go:117-120,170-175`). Logged at Error every scan; no DB involvement. Fix: skip symlinks / non-regular files or log at Debug.

## Can this hang ACP (iOS/Xcode report)?
- Yes, plausibly. The ACP primary's prompt path does autocommit writes (`messages.Create`, `messages.Update` per streamed delta) on the 60 s busy_timeout pool. While any other process (mcp-server, a direct-writing secondary, KB backfill/import) holds the write lock, each such write blocks up to 60 s, sequentially in the agent loop → the editor sees no output for minutes. There is no deadline shorter than the prompt ctx.
- The primary also serialises all secondaries' writes in one writecoordinator goroutine (`writecoordinator/coordinator.go:69-81`), executing remembrances writes (full-session ReplaceSessionEvents scans, CodeIndexFile + embedding HTTP calls) inline → secondaries' writes queue behind them (30 s Long timeouts).
- The context enrichment bug means every first prompt of a session pays an enrichment run that is then aborted; with `hiddenInChat=false` it also creates child sessions, i.e. extra writes on the prompt path.
- No evidence of a SQLite tx held across an LLM call in the main agent; the risk is lock waits (busy_timeout 60 s) on per-delta writes, plus coordinator head-of-line blocking.

## Candidate fix directions (not implemented)
1. Fix `runLoop` to drain `done` until the final event (the one sent at `agent.go:978`/channel close), ignoring intermediate types.
2. Use `_txlock=immediate` for write transactions (or `BEGIN IMMEDIATE` in rag write paths) so the busy handler applies; move the SELECT in `deleteSessionEventsTx` inside an IMMEDIATE tx; add an index/generated column on `json_extract(metadata,'$.session_id')`.
3. Put busy_timeout (and foreign_keys/synchronous) in the DSN via `_pragma=` so every pooled conn gets it; choose an explicit value (e.g. 5 s) for prompt-path writes instead of the implicit 60 s.
4. Route `mcp-server` (and cronjob/agui/CLI writers) through `ipcruntime.Bootstrap` so they become secondaries, or disable their watchers/indexers when a primary exists.
5. Make the session indexer incremental (append new chunks) and throttle it; run embeddings before submitting to the coordinator; retry lock errors in `WriteError.IsRetryable`.
6. agentvcs: skip symlinks/non-regular files.
