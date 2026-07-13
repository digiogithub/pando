---
created_at: 2026-07-13T11:51:45.884334268Z
updated_at: 2026-07-13T11:51:45.884334268Z
tags:
    - change
    - kb
    - wiki-links
    - backfill
    - phase4
---
# KB wiki links — backfill (Phase 4, partial)

Date: 2026-07-13. Follows `pando/changes/kb_wiki_links_phase1.md`. Plan: `pando/plans/kb_wiki_links_plan.md`.

## Motivation

Phase 1 indexes `[[wiki links]]` on every KB write path, but only for documents written *after* the upgrade. Databases upgraded from an older Pando keep their documents with zero rows in `kb_links`, and the filesystem sync will never re-index them: `SyncDirectoryWithStats` skips files whose `source_mtime_unix` has not changed (`internal/rag/kb/sync.go`). Without a backfill the graph would only ever cover new documents.

Backward-compatibility requirement from the user: a database and a KB written by older versions, with no links at all, must keep working unchanged.

## What was changed

### `internal/rag/kb/backfill.go` (new)

- `BackfillStats{Candidates, Scanned, Documents, Links}`.
- `(*KBStore).BackfillLinks(ctx)` — incremental pass: only documents with **no** link rows.
- `(*KBStore).RelinkAll(ctx)` — forced rebuild: `DELETE FROM kb_links` then re-extract everything. This is the escape hatch for a future change in the extractor (new syntax, different slug rule) that makes stored rows stale.
- Both are a no-op when `s.proxy != nil` (secondary instance): the primary owns the writer connection and runs the backfill itself.
- `linkBackfillCandidates` + `relinkBatch` (batches of 50 documents per transaction, context-cancellable).

**Key design decision — no marker column.** The usual way to tell "scanned, has no links" from "never scanned" is a `links_indexed_at` column. It is not needed here: the content is the marker. Candidates are

```sql
SELECT d.id FROM kb_documents d
WHERE d.content LIKE '%[[%'
  AND NOT EXISTS (SELECT 1 FROM kb_links l WHERE l.source_document_id = d.id)
```

A document with no `[[` needs no rows by definition; a document that has rows is done. This means **no second migration and no schema change**, which is exactly what maximizes backward compatibility. The pass is idempotent.

Two consequences handled explicitly:

1. Candidate ids are collected **up front** instead of paging over the predicate, otherwise a document containing `[[` only inside a code fence (it yields no links, so it never gets rows) would match forever and the loop would not terminate.
2. Such link-less candidates are re-read by every later pass. To keep that free, `relinkBatch` writes **nothing** when a document extracts zero links — no no-op `DELETE`. `replaceDocumentLinks` was split, extracting `insertDocumentLinks(ctx, ex, docID, filePath, links)` in `links.go` for this.

Cost: local CPU + a few writes. **No chunking and no embedding calls** — links come from the content already stored.

### `internal/app/remembrances.go` + `internal/app/app.go`

New `(*App).initKBLinkBackfill(ctx, svc)`, called from `app.go` right after `initRemembrancesKBSync`. Runs `BackfillLinks` in a background goroutine registered with `watcherCancelFuncs`/`watcherWG`, so it never delays startup and is cancelled on shutdown. It is deliberately *not* inside `initRemembrancesKBSync`, which returns early when `KBPath` is empty — the backfill must also run for KBs that live only in the database. Logs only when it actually indexed something; `context.Canceled` is silent.

### `cmd/kb.go` (new)

`pando kb relink [--force]`. Loads config, opens the DB directly, builds a bare `kb.NewKBStore(conn, nil, 0, 0)` (no embedder needed) and runs `BackfillLinks` / `RelinkAll`. Reuses `isDBLockedErr` from `cmd/db.go` for the "another instance is writing" hint. Prints "already up to date" when nothing was indexed.

## Backward compatibility summary

- **Old DB without `kb_links`**: `goose.Up` creates it at startup (`internal/db/connect.go`). An old binary on a new DB just ignores the table.
- **Documents with no `[[`**: correct state is zero rows. The wiki layer is purely additive; Phases 2/3 must degrade silently (empty lists, no errors) so a KB with no links behaves exactly as before.
- **Documents already stored *with* `[[`**: recovered by the backfill.
- **Markdown files**: never rewritten. `aliases:` frontmatter is optional, `[[concept]]` is plain text to older versions.

## Verification

- `go build ./...`, `go vet ./internal/rag/kb ./internal/app ./cmd` clean.
- `go test -race ./internal/rag/...` green, including new `internal/rag/kb/backfill_test.go`: legacy-document indexing, idempotency (second pass = zero candidates), already-indexed documents skipped, link-less candidates re-read but never written, forced rebuild dropping stale rows, and an all-legacy KB with no wiki syntax left untouched.
- Run against the real project DB: first `pando kb relink` → "Indexed 3 link(s) across 3 document(s) (14 scanned)"; second run → "already up to date"; `--force` → rebuilds the same 3 links. (14 documents contain `[[`, most of them only inside code fences of the wiki-links plan docs, which is precisely the case the no-write path covers.)

## Still pending in Phase 4

`SyncStats.LinksIndexed` (report links indexed during a filesystem sync / `kb_import_path`).
