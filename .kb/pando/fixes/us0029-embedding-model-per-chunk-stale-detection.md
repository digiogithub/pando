---
created_at: 2026-09-14T16:54:17.726377944Z
updated_at: 2026-09-14T16:54:17.726377944Z
tags:
    - fix
    - rag
    - performance
    - search
---
# PANDO-US-0029 — Record the embedding model per chunk and detect stale embeddings

Status: COMPLETE (2026-09-14). Part of [[PANDO-EP-0007]] search scale and correctness, story 3 of 3, closing the epic. Builds on [[pando/fixes/us0027-stop-selecting-document-body-in-search.md]] and [[pando/fixes/us0028-path-prefix-filter-in-search-sql.md]] (same searchVector/searchFTS plumbing).

## What changed

- **Migration** `internal/db/migrations/20260914000001_add_kb_chunk_embedding_meta.sql` — adds `kb_chunks.embedding_model TEXT NOT NULL DEFAULT ''` and `kb_chunks.embedding_dims INTEGER NOT NULL DEFAULT 0`, backfills `embedding_dims` from `length(embedding)/4` for existing rows, adds `idx_kb_chunks_embedding_dims`. Idempotent and reversible (Down drops only the index; columns stay, per this repo's established SQLite convention — see `20260611000001_add_kb_memory.sql`). Verified by `internal/db/kb_chunk_embedding_meta_migration_test.go`.
- `internal/rag/kb/kb.go`:
  - `KBStore` gained an `embeddingModel string` field + `SetEmbeddingModel`/`EmbeddingModel` accessors.
  - `addDocument` and `AddDocumentWithEmbeddings` now write `embedding_model`/`embedding_dims` per chunk (dims = `len(vector)`, model = the writer's configured model; both stay `""`/`0` for a chunk with no embedding).
  - `kbAddDocumentRequest` (IPC forward payload) gained `EmbeddingModel`; dims travel implicitly via each vector's own length.
  - New `documentContentByID`-adjacent helper `CountStaleEmbeddings(ctx, configuredDims) (StaleEmbeddingStats, error)` — counts chunks whose recorded `embedding_dims` differs from `configuredDims`, plus the distinct non-empty `embedding_model` values among them.
  - `searchVector` now returns a third value, `skipped int` — the count of chunks it excluded for `len(vec) != len(queryEmb)` (previously a silent `continue`).
  - New `SearchStats` type (`SkippedForDimensionMismatch`) and `SearchDocumentsWithOptionsAndStats` — a new public method alongside the unchanged `SearchDocumentsWithOptions`/`SearchDocuments`, so every existing caller is unaffected; only callers that need the warning use the new method.
- `internal/rag/proxy/dispatcher.go` — `kbAddDocRequest` mirrors the new `EmbeddingModel` field; both `KBAddDocument`/`KBUpdateDocument` cases forward it into `AddDocumentWithEmbeddings`.
- `internal/rag/service.go` — `NewRemembrancesServiceWithProxy` calls `kbStore.SetEmbeddingModel(cfg.DocumentEmbeddingModel)`; `RemembrancesService` gained an exported `SetDocumentEmbedder` (test-only override hook, since `docEmbedder` is otherwise unexported and only set by the constructor).
- `internal/app/remembrances.go` — new `initKBEmbeddingStalenessCheck(ctx, svc, cfg)`: one indexed `COUNT` query (synchronous, not backgrounded) comparing every chunk's recorded dimension against `svc.DocumentEmbedder().Dimension()`; logs one `logging.WarnPersist` naming the configured model, the recorded model(s) (or "unknown"), and the count — nothing on a consistent corpus. Wired into `internal/app/app.go` alongside the other remembrances startup steps.
- `internal/llm/tools/remembrances_kb.go` (`KBSearchDocumentsTool`) — calls `SearchDocumentsWithOptionsAndStats`; when `SkippedForDimensionMismatch > 0`, adds a `"warning"` field to the structured response (and to the "no documents found" text when everything was skipped). New `staleEmbeddingWarning`/exported `StaleEmbeddingWarning` helper shared with REST.
- `internal/api/handlers_remembrances_search.go` (`handleKBSearch`) — same `SearchDocumentsWithOptionsAndStats` + `warning` field, using `tools.StaleEmbeddingWarning` so both surfaces phrase it identically.
- `internal/api/handlers_remembrances.go` (`GET /api/v1/remembrances/enrichment`) — `enrichmentStatusResponse` gained `stale_chunks` (int64) and `document_embedding_model`; computed via new `(*Server).kbEmbeddingStaleness`.

Three duplicated in-memory test schemas (`internal/rag/kb/memory_upsert_test.go`, `internal/api/handlers_remembrances_kb_test.go`, `internal/llm/tools/remembrances_kb_mark_outdated_test.go`) got the two new `kb_chunks` columns so existing tests keep passing against the new `INSERT` column list.

## Why

Before this, the only place embedding dimension was ever checked was a silent per-chunk `continue` inside `searchVector` at query time — nothing validated on write, `kb_chunks` had no model/dims column, and there is exactly one document embedder per Pando instance (global to every corpus). Changing `Remembrances.DocumentEmbeddingModel` blinded every consumer at once with no error, no log line, and no way to notice — recall could silently drop toward zero.

## Verification

- `internal/db/kb_chunk_embedding_meta_migration_test.go` — runs the real goose chain against a pre-populated `kb_chunks` row (simulating an upgrade), asserts the backfilled dims (16-byte blob → 4), empty model on the pre-existing row, idempotent re-run, and reversible Down (index removed, no error).
- `internal/rag/kb/embedding_staleness_test.go` — `TestAddDocumentRecordsEmbeddingModelAndDims`, `TestSearchDocumentsWithOptionsAndStats_SkipsStaleEmbeddingDimension`, `TestCountStaleEmbeddings` (zero on consistent corpus; correct count + recorded-model list after an embedder swap).
- `internal/app/remembrances_embedding_staleness_test.go` — `TestInitKBEmbeddingStalenessCheck_WarnsOnMismatch` (captures `slog` output, asserts both model names + count + WARN level present), `_SilentOnConsistentCorpus` (zero log bytes), `_NilInputsDoNotPanic`.
- `internal/api/handlers_remembrances_enrichment_test.go` — `stale_chunks`/`document_embedding_model` reported correctly; zero on a consistent corpus; nil-remembrances does not panic.
- `internal/llm/tools/remembrances_kb_search_test.go` — `kb_search_documents` carries no warning on a clean query, and carries `"stale embedding"` + `"skipped 1 chunk"` + the matching-dimension document's path after a simulated embedder swap (response is TOON/TOML/JSON via `FormatStructuredData`, so the test checks rendered text rather than assuming JSON).
- `go build ./...` — clean (the whole repo, not just the task's packages — the previously-noted unrelated `internal/agui` compile failure from another concurrent agent's WIP has since been fixed upstream and is gone).
- `go test ./internal/rag/... ./internal/llm/tools/... ./internal/api/... ./internal/app/... ./internal/db/...` — all green.

## Not done (out of scope per the story)

- No per-corpus/per-collection embedder — explicitly excluded; `RemembrancesService` remains one embedder per instance.
- No automatic re-embed on mismatch — the remediation path is the existing `POST /api/v1/remembrances/kb/reindex` (PANDO-US-0001); this story only surfaces the problem.
