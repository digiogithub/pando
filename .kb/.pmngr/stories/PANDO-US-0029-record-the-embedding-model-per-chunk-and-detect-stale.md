---
id: PANDO-US-0029
type: story
title: Record the embedding model per chunk and detect stale embeddings
status: done
priority: medium
parent: PANDO-EP-0007
milestone: PANDO-M-0002
author: claude
labels: [rag, performance]
estimate: 5
created: 2026-09-13T21:16:10Z
updated: 2026-09-13T21:16:21Z
---

## Description

As an operator who changed the document embedding model, I want Pando to tell me that the existing chunks no longer match, so that recall does not silently drop to zero.

The only place a dimension is ever compared is inside `searchVector` (`kb.go:891-894`): `if len(vec) != len(queryEmb) { continue }` — per chunk, at query time, silently. Nothing validates on write: `addDocument` (`kb.go:274-300`) checks only that the number of vectors equals the number of chunks, and `kb_chunks` has no `model` or `dims` column. There is exactly one document embedder per Pando instance, global to every corpus and namespace (`internal/rag/service.go:20-25`, built at `:47-80` from `DocumentEmbeddingProvider`/`DocumentEmbeddingModel`, `internal/config/config.go:481-491`), so changing that setting blinds every consumer at once with no error, no log line and no way to notice.

Implementation:

- Migration adding `embedding_model TEXT` and `embedding_dims INTEGER` to `kb_chunks`, following the pattern of `20260611000001_add_kb_memory.sql`, which added seven columns the same way. Backfill `embedding_dims` from `length(embedding)/4` (little-endian float32, `internal/rag/store.go:11-18`); leave `embedding_model` empty for pre-existing rows and treat empty as "unknown", not as a mismatch.
- Write both columns in `addDocument` (`kb.go:274-300`) from the embedder's model id and `Dimensions()`.
- A startup check, run where the KB is wired up (`internal/app/remembrances.go:75-160`), that counts chunks whose recorded dimension differs from the configured embedder's and logs a single loud warning naming both models and the count.
- Expose that count as a `stale_chunks` field on the existing `GET /api/v1/remembrances/enrichment` (`internal/api/handlers_remembrances.go`, route at `routes.go:151`), and report the active document embedding model there too, so a host can pin the model in its own config and refuse to start when Pando reports a different one.
- Have `kb_search_documents` include a warning in its response when the query skipped chunks for a dimension mismatch, counting the `continue` at `kb.go:891-894`.

Do NOT add a per-corpus or per-collection embedder in this story — that is a separate, larger change to `RemembrancesService`. Do NOT re-embed automatically on mismatch: surface it, let the operator trigger a reindex (`POST /api/v1/remembrances/kb/reindex`, PANDO-US-0001).

## Acceptance Criteria

- [ ] A migration adds `embedding_model` and `embedding_dims` to `kb_chunks` and backfills the dimension from the stored blob length; it is idempotent and reversible.
- [ ] Newly written chunks record the model id and dimension of the embedder that produced them.
- [ ] A startup check logs one warning naming the configured model, the recorded model and the count of mismatched chunks; it logs nothing on a consistent corpus.
- [ ] `GET /api/v1/remembrances/enrichment` reports `stale_chunks` and the active document embedding model.
- [ ] A `kb_search_documents` response that skipped chunks for a dimension mismatch carries a warning with the skipped count; a clean query carries none.
- [ ] A test switches the configured dimension and asserts the warning path, the count and that search still returns the matching-dimension chunks.

## Notes

Size M — one migration plus roughly 60 lines. Independent of PANDO-US-0027 and PANDO-US-0028. The remediation path is PANDO-US-0001's reindex route, so schedule this after it if the operator experience matters; the detection half stands alone. Evidence in `report-search-and-fit.md` §A12 and §A14.
