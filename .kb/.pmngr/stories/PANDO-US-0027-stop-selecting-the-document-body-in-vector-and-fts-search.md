---
id: PANDO-US-0027
type: story
title: Stop selecting the document body in vector and FTS search
status: backlog
priority: medium
parent: PANDO-EP-0007
milestone: PANDO-M-0002
author: claude
labels: [rag, performance]
estimate: 3
created: 2026-09-13T21:16:10Z
updated: 2026-09-13T21:16:21Z
---

## Description

As an operator of a growing knowledge base, I want a search query to stop materialising every document body, so that latency and allocation track the number of chunks rather than the size of the corpus.

`searchVector` (`internal/rag/kb/kb.go:840-849`) issues a `SELECT c.id, c.document_id, c.content, c.embedding, d.file_path, d.content, d.metadata, ...` over `kb_chunks JOIN kb_documents` with no `LIMIT` — every embedded chunk on every query — and `d.content` is the entire document body, returned once per chunk of that document. `searchFTS` (`kb.go:945-964`) selects `d.content` the same way. Nothing reads it: `SearchResult.Document.Content` is never consumed by any caller, and the MCP tool returns the chunk, not the document (`internal/llm/tools/remembrances_kb.go:296-316`).

Measured against the live database with the real driver (`github.com/ncruces/go-sqlite3`, WASM, `internal/db/connect.go`): 540 documents, 6 861 embedded chunks, 768-dimension vectors — 136 MB materialised (about 19.9 KB per row), 238 MB allocated, 561 ms cold and 79 ms warm per query. The body is roughly 85 percent of those bytes.

Implementation: drop `d.content` from both SQL strings and from the corresponding `Scan` lists (`kb.go:869-877` and `kb.go:980-989`), leaving `Document.Content` zero-valued. Document on the `SearchResult` type (`internal/rag/kb/types.go:36-41`) that `Document.Content` is not populated by search; if any caller turns out to need it, backfill it with one keyed query over the returned top-k only, never over the scan.

Do NOT introduce an ANN index in this story — sqlite-vec, HNSW or IVF over a quantised copy is a large piece of work and unnecessary below roughly 25 000 chunks. Do NOT trust the comment at `internal/rag/types.go:1-14` claiming sqlite-vec/`vec0` are in use: embeddings are little-endian float32 BLOBs with cosine computed in Go (`internal/rag/store.go:11-18`, `kb.go:895`, `cosine` at `:1180`).

## Acceptance Criteria

- [ ] `d.content` appears in neither `searchVector` nor `searchFTS`, and both `Scan` lists match their new column lists.
- [ ] `SearchResult.Document.Content` is documented as unpopulated by search, or backfilled only for the returned top-k.
- [ ] A Go benchmark in `internal/rag/kb` reports bytes scanned and bytes allocated per query, run before and after, with the improvement recorded in the story or a committed testdata note.
- [ ] Search results (ids, ordering, scores, chunk content, metadata, tags) are unchanged; an existing search test still passes.
- [ ] No caller fails to compile because it relied on `Document.Content` from a search result.

## Notes

Size S — two SQL strings and two `Scan` lists. Independent of every other story in this milestone and the cheapest win available; do it first in this epic. Together with PANDO-US-0028's path prefix it moves the practical ceiling roughly fourfold. Evidence in `report-search-and-fit.md` §A13.
