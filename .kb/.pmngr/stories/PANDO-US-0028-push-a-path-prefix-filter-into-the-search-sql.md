---
id: PANDO-US-0028
type: story
title: Push a path-prefix filter into the search SQL
status: done
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

As a host sharing one knowledge base between several corpora, I want to restrict a query to a path prefix in SQL, so that a query for one project neither under-returns nor pays for scanning the others.

`kb_documents.file_path` is globally unique and there is no collection or namespace column (migration `20260311000001_add_kb.sql`). Path prefixes therefore already give uniqueness — `<corpus>/<project>/items/<ID>.md` — and `file_path` comes back in every result, so a host can resolve a hit to its project today. What does not exist is a *filter*: `kb_search_documents` has no path parameter, and the filters it does have are post-fusion Go passes over a candidate pool of `limit*5` (`kb.go:684-687`, `filterByTags` at `:728`, `filterOutdated` at `:733`, `filterByScope` at `:738`). With three projects in one corpus, a query whose top candidates all belong to another project silently returns nothing.

`scope` cannot be repurposed: `filterByScope` (`kb.go:806-815`) matches `memory_scope`, which only `UpsertMemory` (`internal/rag/kb/memory.go:166-260`) ever writes, and which drags the document into the memory subsystem, its TTL and its GC (`memory_gc.go:11`). Tags cannot either: `matchesTags` (`kb.go:820-829`) is substring containment in both directions.

Implementation: add `PathPrefix string` to `kb.SearchOptions` (`internal/rag/kb/types.go:29-34`); when non-empty, append `AND d.file_path LIKE ? || '%'` to the vector scan (`kb.go:840-849`) and the FTS query (`kb.go:945-964`), passing the prefix as a bound parameter — never string concatenation, and escape `%` and `_` in the prefix with an explicit `ESCAPE` clause. Expose it as an optional `path_prefix` parameter on `kb_search_documents` (`internal/llm/tools/remembrances_kb.go:227-259`) and on the REST search route from PANDO-US-0005 when that exists. This also shrinks the unbounded vector scan by the selectivity of the prefix, which is the second-cheapest performance win after dropping the body.

Do NOT add a `collection` column: that is a migration plus `Document.Collection` plus roughly fifteen call sites (`searchVector`, `searchFTS`, `GetDocument` `:335`, `ListDocuments` `:1024`, `listDocumentMetadata` `:394`, `getDocumentMetadata` `:366`, the memory queries and an `AddDocument` signature change) — a separate, larger decision. Do NOT filter by prefix in Go after fusion: that reproduces the under-return this story exists to fix.

## Acceptance Criteria

- [ ] `kb.SearchOptions` carries `PathPrefix` and an empty value changes nothing about current behaviour.
- [ ] Both search legs apply `AND d.file_path LIKE ? || '%'` with a bound parameter and an `ESCAPE` clause; a prefix containing `%` or `_` matches literally (test).
- [ ] `kb_search_documents` accepts `path_prefix`, and the REST search route exposes it once that route exists.
- [ ] A test with documents under two prefixes asserts a prefixed query returns only matching documents and that the candidate count scanned equals the number of matching chunks (counter or row-count assertion).
- [ ] A prefixed query returns a full `limit` of results where an unprefixed one plus post-filtering would have under-returned.

## Notes

Size S, about 25 lines, no migration. Independent of PANDO-US-0027 but naturally paired with it; exposing it on REST depends on PANDO-US-0005. The 2026-09-13 decision is one Pando instance per repository, which makes this an optimisation rather than a blocker — it becomes a requirement the moment two corpora share an instance. Evidence in `report-search-and-fit.md` §A2-§A3.
