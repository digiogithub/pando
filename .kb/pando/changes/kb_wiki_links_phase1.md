---
created_at: 2026-07-13T08:30:15.536342819Z
updated_at: 2026-07-13T08:30:15.536342819Z
tags:
    - change
    - kb
    - wiki
    - links
    - remembrances
---
# Change: KB Wiki Links — Phase 1 (link model: migration + extraction + storage)

Date: 2026-07-13
Plan: [[kb-wiki-links-plan]] (`pando/plans/kb_wiki_links_plan.md`) — Phase 1 of 5.

## What changed

The KB now extracts `[[wiki links]]` from document bodies and stores them as a
document graph. Nothing consumes the graph yet (that is Phase 2/3); this phase
only guarantees that every write path lands correct link rows.

### New files
- `internal/db/migrations/20260713000001_add_kb_links.sql` — `kb_links` table:
  `source_document_id` (FK → `kb_documents(id)` ON DELETE CASCADE), `source_path`,
  `target_slug` (normalized, **unresolved**), `target_raw`, `label`, `position`,
  `created_at`. Indexes on source, target_slug, source_path. Targets are never a FK
  to the destination document, so a link may point at a document that does not exist
  yet and renames self-heal at query time (same design as `code_edges`).
- `internal/rag/kb/links.go`:
  - `WikiLink{Raw, Slug, Label, Position}`.
  - `ExtractWikiLinks(body)` — matches `[[target]]` and `[[target|label]]`, skips
    fenced code blocks (``` and ~~~) and inline code spans (so docs that merely show
    the syntax don't pollute the graph), dedups by slug keeping the first occurrence.
  - `NormalizeSlug` — lowercase, strip `.md`/`.markdown`/`.mdx`, collapse
    spaces/underscores/dashes into single `-`, keep `/` separators.
  - `DocumentSlugs(filePath, aliases)` — every slug a doc can be addressed by
    (full path, basename, aliases); Phase 2 resolution matches against this.
  - `replaceDocumentLinks(ctx, execer, docID, filePath, body)` — delete + re-insert,
    works on `*sql.Tx` or `*sql.DB` via a small `sqlExecer` interface.
  - `KBStore.LinksFrom(ctx, filePath)` — read back stored (unresolved) links.
- `internal/rag/kb/links_test.go` — extraction edge cases (code fences, inline code,
  labels, dedup, unicode, empty targets), plus store lifecycle tests for all three
  write paths.

### Modified
- `internal/rag/kb/kb.go` — `AddDocument` (direct tx) and `AddDocumentWithEmbeddings`
  (the path the IPC primary uses for forwarded writes) index links inside the same
  transaction as the document insert. Because the IPC request already carries
  `Content`, links are extracted primary-side and **no protocol change was needed**.
  `DeleteDocument` also deletes link rows explicitly (defense in depth alongside the
  FK CASCADE, matching how chunks are already handled). `UpdateDocument` is
  delete+add, so link sets are naturally refreshed.
- `internal/rag/kb/memory.go` — the keyed-memory direct path writes `kb_documents`
  with a raw `INSERT ... ON CONFLICT` instead of going through `AddDocument`, so it
  refreshes links explicitly after the upsert (non-fatal: a link failure logs a
  warning and does not fail the memory write).
- `internal/rag/kb/frontmatter.go` — new `FrontMatter.Aliases []string`
  (`yaml:"aliases,omitempty"`) with merge semantics (incoming wins when non-empty);
  `ExtractAliasesFromMetadata`/`InjectAliasesIntoMetadata` (tags/aliases now share a
  generic `stringSliceFromMetadata` helper) so resolution can read aliases from
  metadata without re-parsing the body.
- `internal/rag/kb/sync.go` — filesystem sync propagates front-matter `aliases:` into
  metadata, like it already did for `tags:`.
- `internal/rag/kb/memory_upsert_test.go` — `openTestKBDB` now mirrors the full KB
  schema (chunks, FTS5, links) and enables `PRAGMA foreign_keys = ON` as production
  does, so CASCADE behaves the same in tests.

## Pre-existing bug found and fixed

`upsertMemoryByKey`'s direct-DB path bound `memory_scope` and `source` through
`nullableString(...)`, which turns `""` into SQL NULL — but both columns are
`NOT NULL DEFAULT ''` (`20260611000001_add_kb_memory.sql`). Any keyed memory upsert
without a scope therefore failed with *"NOT NULL constraint failed:
kb_documents.memory_scope"*. The `remember` tool always defaults scope to `user/`, so
it never triggered; **`kb_add_document` with tag `memory` + `key` and no scope in
metadata was broken in production**. Now binds the plain strings and `nullableString`
is gone. Surfaced by the new `TestUpsertMemoryByKeyIndexesLinks`.

## Verification

- `go build ./...`, `go vet ./internal/rag/kb` — clean.
- `go test -race ./internal/rag/kb` — green (new extraction, slug, add/update/delete
  lifecycle, IPC-primary path, keyed-memory path tests).
- `go test ./internal/rag/... ./internal/llm/tools/` — green (no regressions).
- Migration up/down smoke-tested against the real goose migration chain in a temp DB:
  `kb_links` is created on Up and dropped on Down.

## Next

Phase 2 — `internal/rag/kb/graph.go`: query-time slug resolution (path > basename >
alias), `OutgoingLinks`, `Backlinks`, `RelatedDocuments`, `WantedConcepts`.
Existing KB docs still have no link rows (backfill is Phase 4), so the graph only
covers documents written after this change until then.
