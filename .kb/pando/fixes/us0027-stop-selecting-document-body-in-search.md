---
created_at: 2026-09-14T16:30:45.613149518Z
updated_at: 2026-09-14T16:30:45.613149518Z
tags:
    - fix
    - rag
    - performance
    - search
---
# PANDO-US-0027 — Stop selecting the document body in vector and FTS search

Status: COMPLETE (2026-09-14). Part of [[PANDO-EP-0007]] search scale and correctness, story 1 of 3 (see also PANDO-US-0028, PANDO-US-0029).

## What changed

- `internal/rag/kb/kb.go`:
  - `searchVector` — dropped `d.content` from the `SELECT` (kb_chunks JOIN kb_documents) and from the corresponding `rows.Scan(...)` list. The candidate loop no longer materialises the full document body once per scanned chunk.
  - `searchFTS` — same fix: `d.content` removed from `SELECT` and `Scan`.
  - Added `documentContentByID(ctx, ids []int64) (map[int64]string, error)`: a keyed `SELECT id, content FROM kb_documents WHERE id IN (...)` backfill helper for the rare caller that genuinely needs the full body for its own top-k results, instead of re-adding the column to the scan.
- `internal/rag/kb/types.go` — documented on `Document.Content` and `SearchResult` that search never populates `Content`; only `GetDocument`/direct row queries do. Callers must read `ChunkContent` or backfill explicitly.
- `internal/rag/kb/memory.go` (`GetMemoriesForInjection`) — the memory-recall path (`recall` tool, `<memories>` prompt injection) renders `Document.Content` directly, not a chunk excerpt, so it would have silently gone blank after the fix. Fixed by collecting the distinct document IDs from the search hits and calling `documentContentByID` once per call, then copying the backfilled content onto each `MemoryResult.Document` before merging with the pinned-scope results.
- Left `internal/rag/enricher.go:238` (`chunk = strings.TrimSpace(r.Document.Content)`) as-is: it is a defensive fallback only reached when `ChunkContent` is empty, which does not happen for a real match (FTS/vector chunks always carry non-empty content), so no backfill was warranted there.

## Why

`searchVector`/`searchFTS` were materialising `d.content` — the entire document body — once per scanned chunk, over the *whole* unbounded candidate pool (no `SQL LIMIT`), even though nothing read `SearchResult.Document.Content` from a search result. Measured live: 540 documents / 6,861 chunks, ~136 MB materialised (~85% of row bytes was the body), 238 MB allocated, 561 ms cold / 79 ms warm per query.

## Verification

- New tests in `internal/rag/kb/search_test.go`:
  - `TestSearchDocumentsDoesNotPopulateDocumentContent` — asserts `Document.Content == ""` on search results while `ChunkContent`, `FilePath`, `ID`, `Tags` stay correct.
  - `TestGetMemoriesForInjectionBackfillsContent` — regression test proving memory recall still returns the real content after the fix.
  - `TestDocumentContentByID` — unit test for the new backfill helper (hit, miss, empty input).
- Benchmark `internal/rag/kb/search_bench_test.go` (`BenchmarkSearchVectorScan_WithBody` vs `_NoBody`, plus `BenchmarkSearchDocumentsWithOptions`), results recorded in the committed testdata note `internal/rag/kb/testdata/us0027-search-body-benchmark.md`:
  - bytes scanned: 822,820 → 122,540 per op (**-85.1%**)
  - bytes allocated: 1,064,476 → 333,617 B/op (**-68.7%**)
  - allocations: 12,732 → 11,680 allocs/op (**-8.3%**)
  - row count scanned unchanged (350.0), isolating the effect to the column list as intended.
- `go build ./internal/rag/... ./internal/llm/tools/... ./internal/api/... ./internal/app/... ./cmd/... ./internal/mesnada/...` — clean.
- `go test ./internal/rag/... ./internal/llm/tools/... ./internal/api/...` — all green.
- Did NOT run `go build ./...` / full repo tests: `internal/agui` currently fails to compile on the parent commit due to unrelated, concurrently in-progress work by another agent (`handleListThreads`/`handleThreadMessages`/`handleDeleteThread` undefined) — out of scope per this task's instructions, not caused by this change.

## Not done (out of scope per the story)

- No ANN index (sqlite-vec/HNSW/IVF) — explicitly excluded by the story below ~25k chunks.
- No fix to the stale `internal/rag/types.go:1-14` comment claiming sqlite-vec/vec0 is in use — not part of this story's acceptance criteria; flagged here for a future doc cleanup.
