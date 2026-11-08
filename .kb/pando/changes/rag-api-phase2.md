# Change: rag library Phase 2, contract API (bookmark `rag-api`)

**Date:** 2026-11-08
**Plan:** `pando/plans/gintrack-hub-rag-library.md` (plus addendum and decisions). Phase 1: `pando/changes/rag-module-phase1.md`.
**Workspace:** `/www/MCP/Pando/pando-rag`, bookmark `rag-api`, stacked on `rag-module`. Not pushed.

## Commits (rag-module..rag-api)
1. `feat(embeddings)`: `Model()` on every provider, through the optional interface `embeddings.ModelNamer` plus `embeddings.ModelOf(e)`. `Embedder` itself is unchanged, so fakes and wrappers keep compiling. `kb.Open` falls back to `ModelOf` when `Options.EmbeddingModel` is empty.
2. `feat(migrate)`: migration `rag/migrate/sql/20261108000001_add_kb_api_columns.sql`. It is additive only:
   - `kb_documents`: kind (default `document`), status (default `active`), author, rev, git_blob.
   - `kb_chunks`: start_line, end_line.
   - New tables: `kb_state`, `kb_doc_fields_fts` (FTS5 title/labels/aliases, rowid = doc id).
   - Index `(kind, status)`. Keyed memories are backfilled to `kind='memory'`, and trigger `kb_documents_memory_kind` tags new ones, so Pando's write paths don't change. Trigger `kb_documents_fields_fts_delete` cleans up the fields rows.
   - `internal/db/migrations_merge_test.go` lists it. Verified on a copy of a real Pando DB (`digio-ai-costs-management`): it migrated to 20261108000001, memory=1 and document=24 kinds were backfilled, and a second open reported no migrations to run.
3. `fix(kb)`: `SearchOptions.Scope` is now pushed into SQL on both legs through the shared `sqlFilter`/`legacyFilter` (prefix semantics kept). This fixes the post-fusion under-return in the old `filterByScope`. Regression test: `TestSearchScopeAvoidsUnderReturn`.
4. `feat(kb)`: `chunker.go`.
   - `parseDocument` strips front matter and takes title (or the first H1), labels and aliases.
   - `chunkLines` packs blocks (paragraphs, headings, fences) into chunks of at most ChunkSize, with 1-based source line ranges. It never splits `### <ID>.R<n>` blocks, which run to the next heading of level 3 or higher outside fences. Oversized normal blocks split on lines; a huge single line is cut with `ChunkText` and overlap.
5. `feat(kb)`: `api.go`.
   - `Doc`, `PutResult{Created, Embedded}`, `Put`, `Get` (returns `ErrNotFound`), `Delete`, `DeletePrefix` (refuses an empty prefix; LIKE-escaped), `Close` (no-op, since the db is caller-owned), `ContentRev`.
   - Empty Doc fields come from the front matter (kind/status/author/scope; tags, else labels). Meta = front-matter keys with the caller's Meta on top, stored in the existing `metadata` column. Scope is stored in `memory_scope`.
   - Re-embedding happens only when rev/blob differs, or when a stored chunk has no embedding or one from another model.
   - `deleteDocument` was split into `deleteDocTx` and `deleteChunksTx`, with no behaviour change.
6. `feat(kb)`: `search_api.go`.
   - `Search(text, Query) ([]Hit, Stats, error)`. Every filter is applied in SQL: PathPrefixes, ExcludePrefixes, Scopes (exact or nested `/`), Kinds, Statuses (default active; `AnyStatus` disables it), Tags (json_each, case-insensitive), Authors, ExcludeOutdated.
   - Mode is Hybrid, FTS or Vector. RRF runs over three lists: vector, chunk FTS, and the fields FTS (which points at the doc's first chunk).
   - Chunks from another model are skipped and counted.
   - `Hit` carries Project (set by the caller), Path, Chunk, lines, Score, Rank, Kind, Status, Scope, Author, Rev, GitBlob and Meta. `Fuse(limit, lists...)` merges by rank across projects.
   - The legacy `SearchDocuments*` path is untouched.
7. `feat(kb)`: `apply.go`.
   - `Change{Op add|modify|delete|rename, Path, OldPath, Blob, Content}`.
   - `ApplyChanges(ctx, fsys, changes, watermark)` plans against stored state overlaid with the batch, embeds before the transaction, then applies everything plus the watermark (`kb_state`) in one tx. A recorded watermark makes it a no-op. A same-blob rename moves the doc (id, chunks and embeddings kept; kb_links.source_path updated). Only `.md` paths are indexed.
   - `Watermark()`.
   - `SyncTree(ctx, fsys, SyncOptions{Prefix, Include, Exclude, DeleteMissing})` uses Put (unchanged files are not re-embedded), skips hidden entries, respects ctx, and DeleteMissing never removes kind=memory docs.
   - `SyncStats.Renamed` (`json:"renamed,omitempty"`).
8. `feat(extensions)`: `SearchMiddleware(mgr, projectID func() string)` fills `RemembranceQuery.ProjectID`. The app passes `config.WorkingDirectory`, the same value the write-side attribution uses.

## Deviations from the contract (to confirm with Pips/gintrack)
- Meta lives in the existing `metadata` column, and Scope in `memory_scope` (no new `meta` column, no compat view).
- `Model()` is an optional interface (`embeddings.ModelNamer`), not an `Embedder` method.
- Stats is a new `kb.Stats` (Vector/FTS candidates plus dimension and model skips). The old `SearchStats` is unchanged.
- `Hit.Project` was added so Fuse can tell the same path in different projects apart.
- `UpsertMemory`/`GetMemoryByKey`/`GetMemoriesForInjection` remain as Pando's memory facade. Memory = kind `memory` via backfill and trigger.
- ApplyChanges and SyncTree (without Include) index Markdown only. The converter is not used with fs.FS.

## Verification
Commands, run with `GOROOT=$HOME/.go`:
- Root module (`/www/MCP/Pando/pando-rag`): `go build ./...` and `go vet ./...`.
- Root module: `go test ./internal/db ./internal/rag/... ./internal/llm/tools ./internal/api ./internal/extensions`.
- `rag/` module: `go vet ./...`, `go test ./...` and `go test -race ./kb ./migrate`.

All of these passed. The new tests use the offline `apiEmbedder` and a temp-file DB migrated with `migrate.Up`:
- `api_test.go`, `search_api_test.go`, `apply_test.go`, `chunker_test.go`.
- `migrate_test.go`: `TestMemoryRowsGetMemoryKind`.
- `memory_search_test.go`: `TestSearchMiddlewarePassesProjectID`.

## Follow-up after Pips's review (#70)
Phase 2 was accepted, with deviations 1–4 and 6 approved. (5), Markdown only, is OK for the PoC and is recorded as debt in the plan.

Fix for gintrack memories (ADR-044):
- `normalizeDoc` derives kind from `kind`, else `type`, so `type: memory` gives kind=memory.
- `parseDocument` now reads the front matter as a plain map through `splitFrontMatter`. That helper was extracted from `ParseFrontMatterWithRaw` as a pure refactor, so Pando's behaviour is unchanged.
- Before this, a host key whose shape differs from Pando's typed `FrontMatter` voided the whole front matter. ADR-044's `source: {kind, ref}` is such a key: it cannot decode into `FrontMatter.Source string`.
- New test `TestApplyChangesIndexesGintrackMemories` uses the ADR-044 example file. It checks that `Search(Kinds: [memory], Statuses: [active])` finds the memory in all three modes, that the proposed memory is found only with `Statuses: [proposed]`, that front matter is stripped from chunks, and that line ranges are correct.

Next: on hold. José reviews `rag-module` and `rag-api`; no `rag/vX` tag without his OK.
