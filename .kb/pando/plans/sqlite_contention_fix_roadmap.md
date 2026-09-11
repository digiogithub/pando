---
created_at: 2026-09-11T17:36:25.595647436Z
updated_at: 2026-09-11T18:17:34.013283292Z
---
# Roadmap: SQLite contention / ACP stall fixes (2026-09-11)

Consolidates three sequential subagent analyses done for the iOS/Xcode "Pando ACP hangs" investigation.

Sources: [[pando/analysis/sqlite_locked_interrupted_errors.md]], [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/context_enricher_agent_loop_first_event.md]], [[pando/analysis/session_index_locked_residual_risk.md]], [[pando/changes/acp_logger_slog_bridge_lifecycle_events.md]], [[pando/fixes/session_indexer_debounce_retryable_busy.md]], [[pando/features/session_index_incremental_per_message.md]], [[pando/fixes/ipc_failover_p0_inplace_promotion.md]].

## Key conclusions
- **Session index "database is locked"** (152 failures, 11:19-11:26 UTC).
  - Cause: deferred read-then-write transactions (`ReplaceSessionEvents`, also `kb.deleteDocument`, `EventStore.DeleteEvent`, `relinkBatch`). SQLite does not honour busy_timeout on a read->write upgrade.
  - It happens even with ONE process (8-conn pool, per-token message writes).
  - The mcp-server IPC fix and the enricher fix do NOT remove it.
- **Context enricher (agent-loop)** never worked since c0251b29 (2026-08-08):
  - it read the first event only;
  - the model-switch check compared every agent against the coder model.

  Enrichment is synchronous on the prompt path (60s timeout), so after the fix the first prompt of each session really waits.
- **mcp-server on IPC** had to wait for the failover fixes:
  - PromoteToPrimary closed the DB still used by services;
  - zombie lock on desktop/serve/app;
  - one-shot watcher;
  - shutdown/lock-release race;
  - path-hash port mismatch.

  P0 is now done (step 4 below).

## Recommended order
1. **Quick SQLite wins (small, low risk, removes point-2 error) — IMPLEMENTED 2026-09-11, see [[pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md]].**
   - Global `_txlock=immediate` via a `file:` DSN, built with net/url and resolved to an absolute path (a relative-path DSN silently fails to open; caught during verification).
   - busy_timeout + pragmas per connection via `driver.Open(dsn, init)`: 10s primary / 200ms secondary.
     - foreign_keys is now ON on all 8 primary connections; before, it was only on ~1/8.
     - The full test suite passes; not verified against the real production DB.
   - Migration index `idx_events_session` on `events(subject, json_extract(metadata,'$.session_id'))`, confirmed picked up by `EXPLAIN QUERY PLAN`.
   - Audited all 19 `BeginTx`/`Begin` call sites. None are read-only, so the global DSN change (not per-call-site `LevelSerializable`) covers every read-then-write site with no call-site changes.
   - Related bug fixed in the same audit: `KBStore.addDocument` called the (network) embedding provider while its write transaction was still open. Under the new bounded 10s busy_timeout, that would have starved every other writer for the whole embedding call (up to 45s). The embedding now happens before `BeginTx`, mirroring the proxy path.
   - **Follow-up (2026-09-11):** a one-time `PRAGMA foreign_key_check` against the real production `.pando/data/pando.db`.
     - Why: turning `foreign_keys=ON` on all 8 primary connections could newly reject a write that used to silently succeed, if the DB had pre-existing FK-violating rows.
     - It found exactly **one** orphan row: a `design_versions` row referencing artifact `dsg_1f8559f174a5b596`. That row was deleted on 2026-09-11.
     - The real DB now has **0** FK violations, so the `foreign_keys=ON` change is confirmed safe against production data, not just the test suite.
2. **Context enricher fix — IMPLEMENTED 2026-09-11, see [[pando/fixes/context_enricher_agent_loop_first_event.md]].**
   - Shared `agent.CollectRunResult` drain helper.
   - Agent-aware model switch.
   - `ctxenrich-*`/`title-*` sessions are deleted and skipped in the indexer.
   - Timeout 60s → 25s.
   - One-off startup cleanup (28 sessions, 47 events rows).
3. **Indexer load reduction (#4) + retryable lock errors (#5) — IMPLEMENTED 2026-09-11, see [[pando/fixes/session_indexer_debounce_retryable_busy.md]].**
   - Index only finished messages (`shouldIndexOnEvent`: Created always qualifies, Updated only with a Finish part).
   - Debounce 1.2s → 5s.
   - New 15s per-session minimum interval, with a guaranteed trailing run and no overlapping runs (`sessionIndexScheduler`).
   - New `dbproxy.ErrCodeBusy`/`IsBusyOrLockedError` (retryable), so both a secondary's forwarded write and the primary's own direct write are recognised as transient.
   - The indexer retries `ReplaceSessionEvents` up to 3x (250ms/500ms/1s backoff) reusing the already-computed embeddings, and only logs the final failure at Error.
4. **Incremental per-message session indexing (#6) — IMPLEMENTED 2026-09-11, see [[pando/features/session_index_incremental_per_message.md]].**
   - Chunk per message instead of per whole transcript: a content-hash marker per message, plus a synthetic `"__session_header__"` row for the title.
   - New `EventStore.ReplaceMessageEvents`/`DeleteMessageEvents` scoped by `message_id`, with a new `idx_events_message` expression index mirroring `idx_events_session`.
   - Lazy migration of legacy whole-session rows on a session's first post-upgrade run (no backfill script).
   - Two new IPC write ops, with a version-skew fallback to the old full-transcript replace (`dbproxy.IsMethodNotSupportedError`).
   - Removes the O(n) re-embed + O(n) write-tx + ~12MB IPC payload per index run for large sessions. Unchanged messages are never re-embedded or rewritten, so an index run now costs roughly O(changed messages) instead of O(whole transcript).
   - Audited every consumer of session-scoped events search: no code changes needed. The only visible change is that a search hit's content is now typically one message instead of an arbitrary transcript window.
5. **IPC failover P0 (G1–G6 of [[pando/plans/mcp_server_ipc_bootstrap.md]]) — IMPLEMENTED 2026-09-11, see [[pando/fixes/ipc_failover_p0_inplace_promotion.md]].**
   - **In-place promotion.**
     - `db.PromoteToPrimaryPool` reconfigures every pooled connection to the 10 s busy_timeout and primary pragmas (atomic per-pool init state + checking out `MaxOpenConnections` connections), then 8 conns + migrations.
     - `DBProxy.Promote`/`IsRemote`/`Forward`; stores switched from `proxy != nil`.
     - Services keep their references.
   - **Promotion wires the primary exactly like a Bootstrap primary**, re-announces `IsPrimary=true`, keeps lock/bus/coordinator for shutdown, and has a P3 `startPrimaryServices` hook.
   - **Watcher.**
     - Never takes the lock without a promotion callback (G3).
     - Loops with jittered backoff (G4).
     - Retries the lock for 5 s after `instance.shutdown` (G5).
   - **Ordered primary handover:** drain coordinator → release lock → announce → close bus → rest of shutdown. Idempotent `rt.Cleanup`/`ReleaseLock`.
   - **Canonical workdir** (G6); the watcher is bound to the lock-file ports.
   - **Extra fixes:**
     - the lock file is truncated instead of unlinked (removes a two-primaries unlink race);
     - bind retry during promotion (found by the isolated two-process smoke test);
     - dispatcher registered before bus start;
     - `IPCIsPrimary` data race.
   - Race-clean tests in every touched package, plus an isolated two-process smoke test: graceful handover promoted in ~0.11 s, SIGTERM in ~8 ms, with a DB write after promotion in both.

## Status
- Step 1: **done** (2026-09-11), including the production FK-check follow-up above.
- Step 2: **done** (2026-09-11).
- Step 3 (#4 + #5 of the residual-risk table): **done** (2026-09-11).
- Step 3b (#6, incremental per-message indexing): **done** (2026-09-11); see [[pando/features/session_index_incremental_per_message.md]].
  - One known gap: the version-skew fallback branch inside the indexer has no live multi-process IPC round-trip test. Its two inputs (the error classification and the fallback function) are each tested independently.
- Step 4 (IPC failover, then mcp-server on IPC per [[pando/plans/mcp_server_ipc_bootstrap.md]]):
  - **P0 done (2026-09-11)**; see [[pando/fixes/ipc_failover_p0_inplace_promotion.md]].
  - **Next up — P1: shared `wireIPC`.** This also gives serve/desktop/app a secondary branch, a promote callback, the `SetIPCPrimaryHandover` wiring and `SetupIPC`.
  - Then:
    - P2: mcp-server on the bootstrap (`ModeMCP`), plus signal/EOF handling; ACP also lacks a SIGTERM handler.
    - P3: primary-only background services (fill `startPrimaryServices`; NOT the enricher).
    - P4: agui-serve / cronjob / kb relink.
    - P5: remaining direct writers.
    - P6: multi-process tests.
  - G7 (kill policy) is still open.
