---
created_at: 2026-10-07T13:58:08.867607572Z
updated_at: 2026-10-07T13:58:08.867607572Z
tags:
    - fix
    - code-index
    - sessions
    - embeddings
    - ollama
    - performance
---
# Fix: one project id per directory, and incremental session indexing (2026-10-07)

Follow-up to [[pando/analysis/ollama-cpu-session-reindex-darwin-2026-10-07.md]] and [[pando/fixes/ollama-cpu-kb-sync-loop-one-shot-and-breaker.md]]. Two commits.

## 1. Code index: a directory is never indexed under two ids (commit `fix(code-index): never index one directory under two project ids`)
Problem: each caller derived ids its own way. `code_index_project` used the directory or project name (`app_ios_ivooxnew`), startup used the sanitized full path or the configured `ContextEnrichmentCodeProject`, and TUI and API had their own helpers. So one path got indexed twice. Also, `IndexProject` upserts on `project_id`, so a configured id was moved onto another directory and back (the telethon/iVoox ping-pong), re-embedding the whole tree each time.

- `internal/rag/code/project_resolve.go`: `CodeIndexer.ResolveProjectID(ctx, requestedID, rootPath)` compares canonical paths (abs + clean + EvalSymlinks):
  1. If the path is already registered, it returns the existing id (most recently indexed first; logs a warning when duplicates already exist).
  2. If the requested id is free, or its directory no longer exists (moved project), it returns the requested id.
  3. If the requested id belongs to another existing directory, it returns `requestedID_<8 hex sha256(path)>`.
- `IndexProject` resolves internally. Callers resolve first so they report the real id: `internal/llm/tools/remembrances_code.go` (code_index_project), `internal/api/handlers_remembrances.go`, `internal/tui/page/settings.go`, and startup in `internal/app/remembrances_code.go`. At startup, a configured id that belongs to another directory is replaced in memory (`cfg.ContextEnrichmentCodeProject`) by the resolved one.
- `HasProject` compares canonical paths (`projectRootMatches`).
- Existing duplicates are not deleted. They are reused (most recent) and logged.
- Test: `internal/rag/code/project_resolve_test.go`.

## 2. Session indexing (commit `fix(remembrances): incremental, debounced session indexing`)
`internal/app/remembrances_indexer.go`, `internal/rag/events/session_chunks.go`:
- **Incremental**: `EventStore.SessionChunks` reads the stored chunks and their vectors. Chunking is prefix-stable, so only new or changed chunks are embedded (`embedSessionChunks`). Stored vectors of a different size are re-embedded. An unchanged session skips both the embedding and the write (`sameSessionChunks`), so there is no FTS delete and no lock.
- **Debounce**: `sessionIndexScheduler`. The idle delay went from 1.2s to 30s (`sessionIndexIdleDelay`). There is at most one pass per session at a time; events during a pass mark the session dirty and reschedule it.
- **Content**: reasoning is no longer indexed. Tool inputs are capped at 500 bytes and tool results at 2000 bytes (UTF-8 safe, `truncateForIndex`). Tool result metadata is dropped.
- **Safety**: skipped in one-shot runs (`AppOptions.SkipStartupIndexing`). Each pass uses the app context with a 5 min timeout instead of `context.Background()`. On `embeddings.IsBackendUnavailable` a global 2 min cooldown applies and the session is retried afterwards.
- Tests in `internal/app/remembrances_indexer_test.go` (now with an in-memory event store): reuse (unchanged = 0 embeds, grown = only the tail), reasoning and tool cap, scheduler coalescing and cooldown.

## Verification
`go build ./...`, `go vet`. `go test ./internal/app ./internal/rag/... ./internal/llm/agent ./internal/api ./internal/llm/tools ./internal/tui/page` pass. The new scheduler and indexer tests also pass with `-race`.
