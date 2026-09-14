---
id: PANDO-EP-0005
type: epic
title: "Knowledge base: metadata fidelity and REST search surface"
status: done
priority: critical
milestone: PANDO-M-0002
labels: [rag, kb, api]
created: 2026-09-13T21:11:26Z
updated: 2026-09-13T21:11:26Z
---

## Description

Two defects make the KB unreliable as an external index. `ParseFrontMatter` fills a fixed struct (`internal/rag/kb/frontmatter.go:14-31`) and `sync.go:214-228` lifts only `tags` and `aliases`, so every other front-matter key (`id`, `type`, `status`, `project`) is silently discarded and never reaches `metadata`. The watcher (`watcher.go:163-191`) parses no front matter at all and `updateDocument` is delete-then-add with wholesale metadata replacement (`kb.go:563-568`), so the first edit of a document erases the tags the initial sync stored, permanently, because the mtime is then recorded as current. Measured on Pando's own database: 539 documents with `source_mtime_unix`, 16 with tags. `kb_add_document` mirrors to `KBPath` and, with `KBWatch=true`, the watcher re-indexes that write and clobbers the metadata the tool just stored; `WithWriteOrigin` exists but the watcher never consults it.

Separately, search is reachable only over MCP or an agent run: `internal/api/routes.go:144-152` holds seven administrative remembrances routes and none for `kb_search_documents`, `code_hybrid_search`, document upsert or a directory re-sync. The service objects are already on `App.Remembrances` and every `/api/` path inherits the `X-Pando-Token` middleware, so the REST surface is small.

## Acceptance Criteria

- [ ] Unrecognised front-matter keys are merged into the document's `metadata` on import, never overwriting the reserved `source_*` keys; a document with `status: backlog` returns `metadata.status == "backlog"` from `kb_search_documents`; a document whose front matter fails to parse is still indexed with a warning.
- [ ] The watcher builds the same metadata as the sync path and stores a front-matter-stripped body; a regression test edits a tagged document through the watcher and the tags survive; a startup repair re-indexes documents whose stored metadata lacks tags their source declares.
- [ ] The filesystem mirror records what it just wrote and the watcher skips a matching self-write (bounded TTL and map), so `kb_add_document` metadata survives `KBWatch=true`.
- [ ] `POST /api/v1/remembrances/kb/search` and `/code/search` exist, mirror the MCP tool request and response shapes (`metadata`, `tags`, `score`, `rank`, `include_docs`, `min_score`), answer 401 unauthenticated without reaching a handler, and answer empty rather than panic when `Remembrances == nil`.
- [ ] `POST`/`DELETE /api/v1/remembrances/kb/documents` upsert (metadata stored verbatim) and delete (mirror included); `POST /api/v1/remembrances/kb/reindex` runs `SyncDirectoryWithStats` against `FilesystemMirrorPath()`, returns `SyncStats` and refuses a concurrent run with 409; secondary instances proxy writes to the primary as `kb.go:190-226` already does.

## Notes

git-in-track (GIT-US-0073) does not block on the metadata stories: it runs `KBWatch=false`, drives re-sync itself and resolves hits back to its own index for authoritative fields; it does depend on the reindex route to make sync deterministic and will move from MCP to the REST search routes once they exist. Evidence in `report-search-and-fit.md` §A1-§A7.
