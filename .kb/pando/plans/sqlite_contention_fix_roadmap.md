---
created_at: 2026-09-11T12:53:37.43954421Z
updated_at: 2026-09-11T12:53:37.43954421Z
tags:
    - plan
    - sqlite
    - ipc
    - ios-hang
---
# Roadmap: SQLite contention / ACP stall fixes (2026-09-11)

Consolidates three sequential subagent analyses done for the iOS/Xcode "Pando ACP hangs" investigation. Nothing implemented yet.

Sources: [[pando/analysis/sqlite_locked_interrupted_errors.md]], [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/context_enricher_agent_loop_first_event.md]], [[pando/analysis/session_index_locked_residual_risk.md]], [[pando/changes/acp_logger_slog_bridge_lifecycle_events.md]].

## Key conclusions
- Session index "database is locked" (152 failures 11:19-11:26 UTC) is caused by deferred read-then-write transactions (`ReplaceSessionEvents`, also `kb.deleteDocument`, `EventStore.DeleteEvent`, `relinkBatch`): SQLite does not honor busy_timeout on read->write upgrade. Happens even with ONE process (8-conn pool, per-token message writes). mcp-server IPC fix and enricher fix do NOT remove it.
- Context enricher (agent-loop) never worked since c0251b29 (2026-08-08): reads first event only + model-switch check compares every agent against coder model. Enrichment is synchronous on prompt path (60s timeout) — after the fix the first prompt of each session will really wait.
- mcp-server on IPC must wait for failover fixes: PromoteToPrimary closes the DB still used by services, zombie lock on desktop/serve/app, one-shot watcher, shutdown/lock-release race, path-hash port mismatch.

## Recommended order
1. **Quick SQLite wins (small, low risk, removes point-2 error):** `_txlock=immediate` via `file:` DSN (or `LevelSerializable` at the 4 read-first sites), busy_timeout + pragmas per connection via `driver.Open(dsn, init)` (10s primary / 200ms secondary; foreign_keys currently off on 7/8 conns — run tests), migration index on `events(subject, json_extract(metadata,'$.session_id'))`.
2. **Context enricher fix:** shared `drainRun` helper, agent-aware model-switch, delete + skip `ctxenrich-*` sessions in indexer, timeout 20-30s, one-off cleanup (28 sessions, 47 events rows).
3. **Indexer load reduction:** index only finished messages, longer debounce, retryable lock errors, then incremental per-message indexing (kills full re-embed + 12 MB forwarded payloads).
4. **IPC P0 failover fixes**, then P1 shared `wireIPC`, P2 mcp-server on bootstrap (`ModeMCP`), P3 primary-only background services (NOT the enricher), P4 agui-serve/cronjob/kb relink, P5 remaining direct writers, P6 multi-process tests.
