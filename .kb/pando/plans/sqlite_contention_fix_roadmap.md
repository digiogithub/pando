---
created_at: 2026-09-11T15:41:28.298471429Z
updated_at: 2026-09-11T17:12:30.949529871Z
tags:
    - plan
    - sqlite
    - ipc
    - ios-hang
---
# Roadmap: SQLite contention / ACP stall fixes (2026-09-11)

Consolidates three sequential subagent analyses done for the iOS/Xcode "Pando ACP hangs" investigation.

Sources: [[pando/analysis/sqlite_locked_interrupted_errors.md]], [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/context_enricher_agent_loop_first_event.md]], [[pando/analysis/session_index_locked_residual_risk.md]], [[pando/changes/acp_logger_slog_bridge_lifecycle_events.md]], [[pando/fixes/session_indexer_debounce_retryable_busy.md]].

## Key conclusions
- Session index "database is locked" (152 failures 11:19-11:26 UTC) is caused by deferred read-then-write transactions (`ReplaceSessionEvents`, also `kb.deleteDocument`, `EventStore.DeleteEvent`, `relinkBatch`): SQLite does not honor busy_timeout on read->write upgrade. Happens even with ONE process (8-conn pool, per-token message writes). mcp-server IPC fix and enricher fix do NOT remove it.
- Context enricher (agent-loop) never worked since c0251b29 (2026-08-08): reads first event only + model-switch check compares every agent against coder model. Enrichment is synchronous on prompt path (60s timeout) — after the fix the first prompt of each session will really wait.
- mcp-server on IPC must wait for failover fixes: PromoteToPrimary closes the DB still used by services, zombie lock on desktop/serve/app, one-shot watcher, shutdown/lock-release race, path-hash port mismatch.

## Recommended order
1. **Quick SQLite wins (small, low risk, removes point-2 error) — IMPLEMENTED 2026-09-11, see [[pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md]].** Global `_txlock=immediate` via a `file:` DSN (net/url-built, resolved to an absolute path — a relative-path DSN silently fails to open, caught during verification); busy_timeout + pragmas per connection via `driver.Open(dsn, init)` (10s primary / 200ms secondary; foreign_keys is now ON on all 8 primary connections, was only on ~1/8 before — full test suite passes, not verified against the real production DB); migration index `idx_events_session` on `events(subject, json_extract(metadata,'$.session_id'))`, confirmed picked up by `EXPLAIN QUERY PLAN`. Audited all 19 `BeginTx`/`Begin` call sites: none are read-only, so the global DSN change (not per-call-site `LevelSerializable`) covers every read-then-write site with no call-site changes needed. Also fixed a related bug found in the same audit: `KBStore.addDocument` was calling the (network) embedding provider while its write transaction was still open, which would have starved every other writer for the whole embedding call (up to 45s) under the new bounded 10s busy_timeout — moved the embedding before `BeginTx`, mirroring the proxy path.
   - **Follow-up (2026-09-11):** a one-time `PRAGMA foreign_key_check` against the real production `.pando/data/pando.db` (per the risk noted above — turning `foreign_keys=ON` on all 8 primary connections could newly reject a write that used to silently succeed if the DB had pre-existing FK-violating rows) found exactly **one** orphan row: a `design_versions` row referencing artifact `dsg_1f8559f174a5b596`. That row was deleted on 2026-09-11, and the real DB now has **0** FK violations — the `foreign_keys=ON` change is confirmed safe against production data, not just the test suite.
2. **Context enricher fix — IMPLEMENTED 2026-09-11, see [[pando/fixes/context_enricher_agent_loop_first_event.md]].** Shared `agent.CollectRunResult` drain helper, agent-aware model-switch, delete + skip `ctxenrich-*`/`title-*` sessions in the indexer, timeout 60s → 25s, one-off startup cleanup (28 sessions, 47 events rows).
3. **Indexer load reduction (#4) + retryable lock errors (#5) — IMPLEMENTED 2026-09-11, see [[pando/fixes/session_indexer_debounce_retryable_busy.md]].** Index only finished messages (`shouldIndexOnEvent`: Created always qualifies, Updated only with a Finish part), debounce 1.2s → 5s, new 15s per-session minimum interval with a guaranteed trailing run and no overlapping runs (`sessionIndexScheduler`); new `dbproxy.ErrCodeBusy`/`IsBusyOrLockedError` (retryable) so a secondary's forwarded write and the primary's own direct write are both recognized as transient; indexer retries `ReplaceSessionEvents` up to 3x (250ms/500ms/1s backoff) reusing the already-computed embeddings, only logging the final failure at Error.
   - **Remaining:** #6 incremental per-message indexing (kills the full re-embed + full-transcript replace + 12MB forwarded payloads) — design sketched in the fix doc, not implemented.
4. **IPC P0 failover fixes**, then P1 shared `wireIPC`, P2 mcp-server on bootstrap (`ModeMCP`), P3 primary-only background services (NOT the enricher), P4 agui-serve/cronjob/kb relink, P5 remaining direct writers, P6 multi-process tests.

## Status
- Step 1: **done** (2026-09-11), including the production FK-check follow-up above.
- Step 2: **done** (2026-09-11).
- Step 3 (#4 + #5 of the residual-risk table): **done** (2026-09-11). #6 (incremental per-message indexing) not started.
- Step 4 (IPC failover): not started.
