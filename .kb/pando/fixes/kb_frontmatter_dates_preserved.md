---
created_at: 2026-11-11T12:30:34.981976029Z
updated_at: 2026-11-11T12:30:34.981976029Z
tags:
    - fix
    - rag
    - kb
---
# Fix: KB documents keep the created_at/updated_at their front matter records

## Problem

Pando wrote `created_at`/`updated_at` into each KB file's front matter but never read them
back. `buildDocumentMetadata` parsed them and dropped them (both keys are reserved), and
`AddDocumentWithEmbeddings` always stored `now, now`. `updateDocument` is a delete plus a
re-add, so every update also reset `created_at`. A lost index, a reindex, a `touch` or a
git checkout/pull (any mtime change) therefore re-dated documents to the moment they were
indexed. `kb_add_document` then wrote that wrong `created_at` back into the file. Its update
path built the front matter from the index only, so it also dropped `aliases` and
host-defined keys, and it stored no `source_path`/`source_mtime_unix`, so the next startup
re-imported the file. Phases 2 and 6 of [[pando/plans/kb_yaml_frontmatter_plan.md]] required
the opposite.

## What changed

- `internal/rag/kb/kb.go`:
  - New `DocumentTimes`, plus `AddDocumentWithTimes` and `UpdateDocumentWithTimes`.
    `AddDocument`/`UpdateDocument` delegate to them with zero times.
  - `resolveDocumentTimes`: a missing value becomes now, both are stored in UTC, and
    `updated_at` is raised to `created_at` when it is earlier.
  - `updateDocument` reads the stored `created_at` before its delete + re-add, so any update
    without a date keeps it (API upsert, keyless memory upsert, tools).
- `internal/rag/kb/sync.go`: `buildDocumentMetadata` also returns the front-matter dates. The
  startup sync, the watcher (`watcher.go`) and the front-matter repair (`repair.go`) index
  with them.
- `internal/rag/kb/frontmatter.go`: `ExtraFrontMatterKeys`, and
  `SerializeFrontMatterWithExtra`, which writes host-defined keys after the typed fields.
- `internal/rag/kb/filesystem.go`:
  - `ReadMirroredFrontMatter` reads the front matter of a document's mirrored file.
  - `MirrorSourceMetadata` returns the `source_*` stamp of that file. Markdown paths only:
    the sync never visits other paths, so a `source_path` on them would make its
    delete-missing pass remove the document.
- `internal/llm/tools/remembrances_kb.go` (`kb_add_document`, non-memory path):
  - Merges from the file's own front matter, falling back to the index (`created_at`, tags,
    aliases). This also applies when the document is not indexed but its file exists (lost
    index).
  - Writes the file first, keeping host-defined keys. Then indexes it with the same dates,
    the merged tags and aliases, the host keys and the file's `source_*` stamp.

Decision: a file with `created_at` but no `updated_at` gets `updated_at` = import time.

## Verification

- `internal/rag/kb/document_dates_test.go`:
  - sync stores the 2020 dates;
  - a watcher edit and a `touch` keep them, and so do a reindex after an mtime change and a
    fresh index;
  - an update without front-matter dates (and a plain `UpdateDocument`) keeps `created_at`;
  - offset and nanosecond timestamps are stored in UTC;
  - created-only and backwards dates follow the rules above;
  - the `source_*` stamp is only for markdown;
  - extra keys round-trip through the serializer.
- `internal/llm/tools/remembrances_kb_add_dates_test.go`:
  - `kb_add_document` over an indexed file and over an unindexed file keeps `created_at`,
    aliases and host keys in both the index and the file;
  - the next sync leaves the file unchanged;
  - new documents and tag replacement work;
  - a non-markdown path is not deleted by the sync.
- `go test ./internal/rag/... ./internal/llm/tools ./internal/api` passes.

## Follow-ups (not fixed here)

- An unparseable front-matter date makes the whole front matter fail to parse, tags included,
  and the document is indexed with the front matter as body text.
- `MarkOutdated` (`memory.go`) rewrites the file without `created_at` and tags.
- `kb_delete_document` also deletes the file on disk (`filesystem.go`), but its description
  does not say so.
