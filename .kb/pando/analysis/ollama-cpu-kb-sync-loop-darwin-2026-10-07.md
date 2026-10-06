---
created_at: 2026-10-07T13:35:43.894441725Z
updated_at: 2026-10-07T13:35:43.894441725Z
tags:
    - analysis
    - ollama
    - embeddings
    - kb
    - telemetry
    - darwin
---
# Ollama 100% CPU on macOS: KB sync never converges (telemetry analysis, 2026-10-07)

Source: Better Stack source `pando` (2751484), darwin v1.2.9 sessions 2026-10-05..07 (debug_id 5197-1123-6710-9997, project ~/GIT/telethon_downloader).

## Findings
1. **Every pando process re-runs full KB sync at startup**, including mesnada subagents (`pando-cli` non-interactive, logged as mode `tui`). ~17 startups in 3 days, sometimes 3 at once (2026-10-06 08:58:37).
2. **Every doc fails embedding, nothing is persisted, so next startup redoes all 21 docs** (`mode=update` each time). Docs spaced exactly 45s = `kbEmbeddingsTimeout` (internal/rag/kb/kb.go:21). Final error: `kb: update memory/project/...local_deploy_env_file.md (65 bytes): ollama: text 0: ... /api/embeddings: context deadline exceeded`. Sync skips only on matching `source_mtime_unix`, so failed docs retry forever.
3. **Request amplification** in `OllamaEmbedder.EmbedDocuments` (internal/rag/embeddings/ollama.go): batch `/api/embed` fails -> per-text `/api/embed` -> per-text legacy `/api/embeddings`, no backoff/circuit breaker. Ollama keeps computing abandoned requests after client timeout, so the queue never drains.
4. Even successful embeds are slow: 17 chunks took 45.0s on desktop (contention with concurrent processes).
5. `ContextEnrichmentCodeProject = app_ios_ivooxnew` (global config) makes the telethon_downloader tree index under the iVoox project id: two different roots share one code project, risking ping-pong reindex. Also `sqlite3: database is locked` on code reindex/KB fts delete, and `dbproxy METHOD_NOT_FOUND ReplaceSessionEvents` (369 errors) for secondaries.

## Suggested fixes
- Skip startup KB auto-import + code startup indexing in non-interactive/mesnada child processes (like `cronjob`).
- Per-doc failure memo / backoff; circuit breaker on Ollama timeouts in sync.
- Remove fallback fan-out on timeout errors (only fall back on 404/unsupported); don't call legacy endpoint after ctx deadline.
- Single-flight embedding across instances (primary does embedding) or serialize via IPC.
- Don't apply global `ContextEnrichmentCodeProject` to a different working dir; implement `ReplaceSessionEvents` in dbproxy primary.

Related: [[fix_sqlite_immediate_tx_conn_pragmas]], [[default-code-embedding-model-repo-gone]].
