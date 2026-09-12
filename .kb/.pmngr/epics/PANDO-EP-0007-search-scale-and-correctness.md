---
id: PANDO-EP-0007
type: epic
title: Search scale and correctness
status: backlog
priority: medium
milestone: PANDO-M-0002
labels: [rag, performance]
created: 2026-09-13T21:11:26Z
updated: 2026-09-13T21:11:26Z
---

## Description

KB vector search is a full scan of every embedded chunk in Go with no ANN index, which is acceptable for tens of thousands of chunks, but `searchVector` and `searchFTS` select `d.content` once per chunk and no caller reads it (`internal/rag/kb/kb.go:840-848`, `:945-961`). Measured against the live database with the real driver: 6 861 chunks materialise 136 MB, allocate 238 MB and cost 561 ms cold / 79 ms warm per query; the body is about 85 percent of the bytes. There is no collection or namespace: `file_path` is globally unique and every filter (`tags`, `scope`, `exclude_outdated`) is a post-fusion Go filter over `limit*5` candidates, so a path prefix gives uniqueness but not a cheaper query. Nothing records which embedding model produced a chunk; a dimension mismatch is a silent `continue` at query time (`kb.go:891-894`), so switching models degrades recall without any signal.

## Acceptance Criteria

- [ ] `d.content` is no longer selected in either search leg; `SearchResult.Document.Content` is documented as unpopulated by search or backfilled only for the returned top-k; a benchmark records bytes scanned and allocations before and after.
- [ ] `kb.SearchOptions.PathPrefix` is pushed into SQL on both legs (`AND d.file_path LIKE ? || '%'`), exposed by `kb_search_documents` and the REST route, and a test asserts a prefixed query scans only matching documents.
- [ ] The embedding dimension or model id is recorded per chunk; a startup check compares it with the configured embedder and logs loudly on mismatch; the stale-chunk count is reported on an existing remembrances route and `kb_search_documents` warns when it skipped chunks.

## Notes

Practical ceiling today is roughly 25 000 to 35 000 chunks per instance; a 5 000-item backlog plus its KB is about 10 500. One Pando per repository (decision of 2026-09-13) keeps every instance well inside that. Evidence in `report-search-and-fit.md` §A11-§A14.
