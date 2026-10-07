---
created_at: 2026-10-07T13:48:17.544455666Z
updated_at: 2026-10-07T13:48:17.544455666Z
tags:
    - analysis
    - ollama
    - embeddings
    - sessions
    - telemetry
    - darwin
---
# Ollama 100% CPU, second user: full session re-embedding per turn (2026-10-07)

debug_id 7402-2673-1745-4679, macOS v1.2.11, mode `app`, project ~/Documents/Xcode/iVoox/redesign/one/app_ios_ivooxnew (agentvcs tracks 8050 files). Info level only.

## Telemetry
- Startup at 2026-10-07 13:47:29 is clean: KB `147 scanned, 147 unchanged` (no embeddings), code index already present (`startup incremental indexing ready`), watcher on. So startup does not load Ollama, unlike [[pando/analysis/ollama-cpu-kb-sync-loop-darwin-2026-10-07.md]].
- Errors: `remembrances session index failed: replace session events: events: fts delete: sqlite3: database is locked` (13:21, 13:31).

## Root-cause candidate (code)
`initRemembrancesSessionIndexing` (internal/app/remembrances_indexer.go) subscribes to every message Created/Updated event. With a 1.2s per-session debounce, `indexSessionConversation` rebuilds the whole conversation text (text + thinking + every tool input + every tool result + metadata), chunks it at 800 chars and calls `EmbedDocuments` on all chunks, then `ReplaceSessionEvents` (delete + re-insert + FTS). Any pause of 1.2s or more (tool execution, LLM latency) triggers it, so a long agent session re-embeds hundreds of chunks after almost every step. Cost grows quadratically with session length, uses `context.Background()` with no timeout, and runs for every session (subagents too). That matches "Ollama 100% while working" with a clean startup, and the `database is locked` on the FTS delete.

## Fix options
- Incremental: hash chunks and only embed new or changed ones (reuse stored vectors); append-only for new messages.
- Index at turn end or after a long idle (for example 30s), not on every message update.
- Leave thinking and large tool results out of the indexed text, or cap their size.
- Skip in one-shot children (SkipStartupIndexing), bound the context, and back off on `embeddings.IsBackendUnavailable`.

Related fix: [[pando/fixes/ollama-cpu-kb-sync-loop-one-shot-and-breaker.md]].
