---
created_at: 2026-09-14T15:59:30.258147303Z
updated_at: 2026-09-14T15:59:30.258147303Z
tags:
    - rag
    - kb
    - fix
    - watcher
    - frontmatter
---
## PANDO-US-0003 — Parse front matter in the KB watcher and repair the damage at startup

### What changed

- `internal/rag/kb/sync.go`
  - Added `loadResult` struct (`content`, `format`, `converted`) and
    `buildDocumentMetadata(absPath, docPath string, mtimeUnix int64, res loadResult) (map[string]interface{}, string)`,
    factored out of the old inline block in `SyncDirectoryWithStats`. This is now the single
    place that turns loaded file content into metadata + indexable body: converted documents
    are flagged `"converted": true` with the body indexed verbatim; markdown documents are
    parsed via `ParseFrontMatterWithRaw`, tags/aliases injected via
    `InjectTagsIntoMetadata`/`InjectAliasesIntoMetadata`, and every other front-matter key
    merged in via `MergeUnknownFrontMatterKeys` (all from PANDO-US-0002).
  - `SyncDirectoryWithStats` now just calls `buildDocumentMetadata(...)` instead of duplicating
    the front-matter parsing logic inline.
- `internal/rag/kb/watcher.go`
  - `handleWatchEvent` now calls the same `buildDocumentMetadata` helper instead of building a
    bare `{source_path, source_mtime_unix, source_format[, converted]}` map. This was the root
    cause: the watcher never parsed front matter at all, and `UpdateDocument`/`AddDocument` is
    delete-then-add (`kb.go` `updateDocument`), so the first edit made through the watcher
    permanently wiped tags and any other front-matter key an initial sync had stored (the mtime
    written by the watcher then made the sync path think the file was unchanged, so it never
    repaired it either).
  - The body now indexed by the watcher is front-matter-stripped, matching the sync path (it
    used to index the raw file content, front matter included).
- `internal/rag/kb/repair.go` (new)
  - `RepairFrontMatterMetadata(ctx, dirPath) (RepairStats, error)`: a bounded, one-shot startup
    repair. It walks `listDocumentMetadata` for every document with a `source_path` (skipping
    converted and synthetic/memory documents), re-parses each source file's current front
    matter via `buildDocumentMetadata` (ignoring the mtime-skip the regular sync/watcher use —
    that field is exactly what the bug left in a false "unchanged" state), and calls
    `UpdateDocument` only when the freshly-parsed metadata declares a key
    (`frontMatterMetadataMissing`) the stored metadata does not have.
  - One-shot via a synthetic marker document at `__pando_kb__/frontmatter_repair_marker`
    (written under `WithoutWriteObserver` so it never surfaces as a user-visible write event),
    recording `dir_path`/`scanned`/`repaired`/`ran_at`. A later call for the same `dirPath`
    short-circuits on the marker lookup without scanning.
- `internal/rag/kb/types.go` — added `RepairStats{Scanned, Repaired int}`.
- `internal/app/remembrances.go` — `initRemembrancesKBSync`'s existing background goroutine (the
  one gated by `cfg.KBAutoImport`) now calls `svc.KB.RepairFrontMatterMetadata(importCtx, kbPath)`
  right after the initial `SyncDirectoryWithStats` summary log, logging a scanned/repaired count
  (`WarnPersist` only when something was actually repaired).

### Why

Watcher edits under `KBWatch=true` were silently destroying document metadata: `handleWatchEvent`
built metadata from only `source_path`/`source_mtime_unix`/`source_format`, and since
`updateDocument` is delete-then-add, the very first edit through the watcher discarded whatever
tags/front-matter keys the initial filesystem sync had stored — permanently, because the watcher
recorded a fresh `source_mtime_unix`, so the sync path's mtime-skip check never saw the file as
"changed" again and could never repair it on its own. Measured on Pando's own KB before the fix:
539 documents with `source_mtime_unix`, only 16 still with tags.

### Verification

- `go build ./...` — green.
- `go test ./internal/rag/...` — green (`internal/rag/kb` included).
- `go test ./internal/app/...` — green (covers `remembrances.go` wiring).
- New tests in `internal/rag/kb`:
  - `watcher_test.go`: `TestWatcherAndSyncBuildIdenticalMetadata` (watcher and sync build
    byte-identical metadata + body for the same file), `TestWatcherEditPreservesTagsAndCustomFrontMatterKey`
    (the regression: index via watcher, edit, tags + custom front-matter key survive),
    `TestWatcherAddDocumentAlsoParsesFrontMatter` (the AddDocument branch, not just Update).
  - `repair_test.go`: `TestRepairFrontMatterMetadataFixesWatcherDamage`,
    `TestRepairFrontMatterMetadataIsOneShot` (second call for the same dir scans nothing),
    `TestRepairFrontMatterMetadataNoOpOnHealthyCorpus` (scans but writes nothing on a corpus
    already indexed by the fixed code — zero write-observer events), `TestRepairFrontMatterMetadataSkipsNonFilesystemDocuments`.
- Pre-existing, unrelated failures in `internal/llm/agent` (caveman session tests +
  `TestApplyToolDiscoveryWithoutManagerIsUnchanged`) were left untouched, as instructed.

Related: [[pando/fixes/mirror_watcher_self_write_suppression_pando_us_0004.md]],
[[.pmngr/epics/PANDO-EP-0005-knowledge-base-metadata-fidelity-and-rest-search-surface.md]].
