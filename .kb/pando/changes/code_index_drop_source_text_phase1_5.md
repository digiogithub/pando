---
created_at: 2026-06-27T21:57:53.071870953Z
updated_at: 2026-06-27T21:57:53.071870953Z
tags:
    - change
    - code-indexing
    - rag
    - sqlite
    - vacuum
---
# Change: Drop source text from code index (Phase 1 bundle) + DB compaction (Phase 5)

Date 2026-06-27. Implements Phases 1 and 5 of `pando/plans/code_index_drop_source_text_and_db_compact_plan.md`.

## What changed

### Phase 5 — DB compaction core + CLI (independent)
- `internal/db/compact.go` (new): `CompactOptions{Incremental, EnableAutoVacuum}`, `CompactResult{Mode, SizeBefore, SizeAfter, Freed}`, `Compact(ctx, conn, opts)` and `DBPath()`. Full mode runs optional `PRAGMA auto_vacuum=INCREMENTAL` then `VACUUM`; incremental mode runs `PRAGMA incremental_vacuum`. Always finishes with `PRAGMA wal_checkpoint(TRUNCATE)`. Size measured via `PRAGMA page_count*page_size` (no config dependency).
- `cmd/db.go` (new): `pando db compact` (+ hidden alias `pando db-compact`) with flags `--incremental`, `--no-auto-vacuum`. Loads config, `db.Connect()`, calls `db.Compact`, prints freed bytes. On SQLite locked/busy error it tells the user to stop other instances or use the (future) `/db-compact` slash command. NOTE: the slash command + IPC `db.compact` RPC forwarding is Phase 6 (not yet implemented); the CLI currently relies on SQLite's busy semantics (safe — no corruption, just fails if another writer holds the DB).
- Test `internal/db/compact_test.go`: churns insert+delete, asserts VACUUM shrinks size, sets `auto_vacuum=2`, and incremental follow-up succeeds.

### Phase 1 bundle — stop storing source_code (pulls in Phases 2-4, inseparable)
Dropping the column alone would break the running app, so the write/read/search code was adapted in the same change.
- Migration `internal/db/migrations/20260627000001_code_index_drop_source_code.sql`: converts `code_symbols_fts` from external-content to **contentless** (`content=''`, `contentless_delete=1`), repopulating the inverted index from the still-present `source_code` BEFORE `ALTER TABLE code_symbols DROP COLUMN source_code`. Drops the old ai/au/ad triggers; recreates only an AFTER DELETE trigger that does a plain `DELETE FROM code_symbols_fts WHERE rowid=old.rowid` (contentless_delete tables use ordinary DELETE, NOT the legacy `'delete'` command — that was the one bug found in testing). Applies automatically on startup via `goose.Up` in `db.Connect()`; backward-compatible, no mandatory reindex. Down is lossy (restores empty column + external-content FTS shape).
- `internal/rag/treesitter/types.go`: `CodeSymbol.Stale bool` (set when hydrated text no longer matches the indexed hash).
- `internal/rag/code/indexer.go`: removed `source_code` from `selectCodeSymbolColumns`, the dead `codeSymbolSelectColumns` const, and all 3 `scanCodeSymbolRow*`. Both INSERT paths (IndexFile + IndexFileDirect) no longer write `source_code`; they capture the rowid and call new `insertSymbolFTS` to populate the contentless FTS from the in-memory body. `lexicalBoost` dropped its source component (BM25 covers source now). `FindReferences` replaced `source_code LIKE` with an FTS MATCH (call sites live in other symbols' source) + base-column LIKE, then hydrates. `SearchPattern` rewritten: pass 1 matches base columns without disk, pass 2 pre-filters source via FTS then hydrates+confirms (arbitrary regex/substring over source may be missed → use Grep). `FindSymbol`/`loadChildren` no longer re-SELECT source; hydrate the tree when IncludeBody. `GetSymbolsOverview` and `HybridSearch` hydrate their bounded result sets.
- `internal/rag/code/hydrate.go` (new): `insertSymbolFTS`, `sourceCache` (per-call memoization of project roots, file contents+sha256, indexed hashes), `hydrateSource` (byte-slice `[StartByte:EndByte]` when file hash matches; line-range fallback + `Stale=true` on mismatch/missing), `hydrateTree`, `hydrateSymbols`, `extractLines`.
- Tests `internal/rag/code/hydrate_test.go`: `TestSourceCodeColumnDropped` (real goose migrations → column gone), `TestHydrationAndFTSRoundTrip` (IndexFileDirect → FTS find → hydrated body equals disk; edit file → Stale=true), `TestReindexDeleteTriggerRemovesFTS` (delete trigger purges contentless FTS rows). Updated existing `TestHybridSearchBoostsSourceMatchesWhenStructureIsSparse` to assert FTS (not in-memory boost) surfaces source matches.

## Verification
- `go build ./...` clean; `go vet` clean on changed packages.
- `go test ./internal/rag/... ./internal/db/ ./internal/llm/agent ./internal/api` all pass; `go test -race ./internal/rag/code/ ./internal/db/` pass.
- Built binary: `pando db compact --help` and `pando db-compact` wired.

## Notes / follow-ups
- Phase 6 (slash command `/db-compact` + IPC `db.compact` RPC routing to primary) NOT done — only CLI requested in this round.
- ROI reminder: source text was ~38.6 MB of a ~1 GB real DB; the bigger wins (embeddings ~228 MB, freelist ~196 MB via VACUUM, events/messages/files ~320 MB) are separate future plans.
- SQLite bundled by ncruces v0.25.0 is 3.49.1 (contentless_delete + DROP COLUMN supported).
