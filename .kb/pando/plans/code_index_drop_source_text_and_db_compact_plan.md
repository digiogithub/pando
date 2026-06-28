---
created_at: 2026-06-27T21:34:31.775373957Z
updated_at: 2026-06-27T21:34:31.775373957Z
tags:
    - plan
    - code-indexing
    - rag
    - sqlite
    - vacuum
    - architecture
---
# Plan: Drop source text from the code index + DB compaction (VACUUM / auto_vacuum) + `/db-compact` & `pando db-compact`

Created 2026-06-27. Status: PLANNED (awaiting implementation).

## Goal

Stop storing the original source code text in the code index DB. The text already exists on disk; we keep only `file_path` + `start_line/end_line/start_byte/end_byte` (+ `code_files.file_hash`) and re-read the live text on demand at result time. Bonus: search results reflect the *current* file content, not the indexed snapshot. Add on-demand DB compaction (full `VACUUM`) and enable `auto_vacuum=INCREMENTAL`, exposed via a new slash command `/db-compact` and a new CLI subcommand `pando db-compact`. Everything must be backward-compatible with already-created DBs (migrated in place on startup, no mandatory reindex).

## Baseline measurement (real DB `.pando/data/pando.db`, 77,870 symbols, ~1 GB total)

- `code_symbols.embedding`: ~228 MB (~22%)
- freelist / fragmentation (reclaimable by VACUUM): ~196 MB (~19%) — 47,932 free pages * 4096
- `events`+`messages`+`files` tables: ~320 MB (~31%)
- `code_symbols_fts` (BM25 inverted index): ~67 MB (~6%)
- **`code_symbols.source_code` (the target): ~38.6 MB (~4%)**
- name/name_path/doc_string/signature/metadata + indexes: ~6 MB

Honest ROI: removing source text saves only ~4% directly, but it ALSO reduces freelist churn on every reindex (every reindex does `DELETE ... WHERE file_id=?` + re-INSERT). VACUUM reclaims ~196 MB now; auto_vacuum keeps it from re-growing. SQLite bundled by ncruces v0.25.0 is **3.49.1** → `contentless_delete=1` and `ALTER TABLE DROP COLUMN` both supported.

## Current architecture (relevant facts)

- Migrations: `internal/db/connect.go:134` runs `goose.Up(db, "migrations")` on every startup; migrations embedded via `internal/db/embed.go` (`//go:embed migrations/*.sql`). Goose tracks applied versions in `goose_db_version` → only new migrations run. A new migration file applies automatically on first launch of the new version.
- Schema: `internal/db/migrations/20260312000001_add_code_indexing.sql`. `code_symbols.source_code TEXT`. `code_symbols_fts` is FTS5 **external-content** (`content='code_symbols'`, `content_rowid='rowid'`) → index does NOT duplicate text; it reads from base table for delete/rebuild/highlight.
- FTS triggers: `internal/db/migrations/20260320000001_add_code_fts_triggers.sql` (ai/au/ad) read `new/old.source_code`.
- Indexer: `internal/rag/code/indexer.go`. Inserts symbols (lines ~449 and ~1790, two paths), reindex deletes via `DELETE FROM code_symbols WHERE file_id=?` (lines ~417 and ~1754). `selectCodeSymbolColumns`/`scanCodeSymbolRow*` (lines ~804-900) always SELECT `source_code`. Abs path reconstruction already exists: `filepath.Join(rootPath, file_path)` (line ~629); `file_path` is relative (`filepath.Rel`, line ~346).
- Paths that consume `sym.SourceCode`: `embedSymbols` (line ~502, at index time — uses in-memory text, fine), `lexicalBoost` (lines 988/1003, ranking), `FindSymbol`/`loadChildren` `IncludeBody` (lines 1131/1162 — currently a redundant re-SELECT), `FindReferences` (`source_code LIKE`, line 1444), `SearchPattern` (regex/substring over `sym.SourceCode`, lines 1521/1535).
- IPC writer model (`internal/db/connect.go`): a **primary** owns writes + migrations; **secondaries** proxy writes via ZMQ and open RO/RW-with-fallback. WAL mode. Implication: `VACUUM` cannot be proxied and must run on the primary's writer connection with no competing writers. `auto_vacuum` mode change on an existing DB only takes effect after a full `VACUUM`.
- Slash commands: `internal/commands/registry.go` `BuiltinCommands()` + dispatch per interface (`internal/tui/components/dialog/commands.go`, `internal/mesnada/acp/slash_commands.go`, `internal/api/handlers_commands.go`).
- CLI: cobra, one file per command in `cmd/`, `var xCmd = &cobra.Command{...}` + `init()` → `rootCmd.AddCommand`.

## Design decisions

1. **FTS becomes contentless** (`content=''`, `contentless_delete=1`), still declaring columns `name, name_path, doc_string, source_code` so BM25 keeps matching across all four (incl. column filters like `source_code:foo`). It tokenizes at insert but stores no copy. Trade-off: `highlight()`/`snippet()` stop working — Pando does NOT use them (it computes its own boost + re-reads disk), so acceptable.
2. **Drop `code_symbols.source_code`** after repopulating the new FTS from existing text in the same migration → already-indexed symbols stay searchable with zero reindex.
3. **FTS maintenance moves partly into app code**: AFTER-INSERT trigger cannot read a dropped column, so the indexer inserts into FTS explicitly within its existing tx (it has the in-memory `SourceCode`). DELETE stays as a trigger using rowid only (contentless delete needs no column values).
4. **Hydrate on read**: new `hydrateSource` reads the file slice from disk on demand, with a per-request file-content cache and `file_hash` verification. Default extraction = byte offsets `[StartByte:EndByte]` with fallback to line range `[StartLine:EndLine]` when the stored `file_hash` differs from the live file (then mark `Stale=true`). This is the recommended staleness policy (precise when unchanged, robust when edited, never silently wrong). Make it a small config knob later if needed; no on-the-fly reindex in v1 (keep read path cheap).
5. **Ranking no longer needs disk I/O**: BM25 already indexes source tokens, so drop the `source_code` component from the Go-side `lexicalBoost`; keep boosts on name/name_path/signature/doc_string (all still in base table). Hydration is only for OUTPUT (include_body, displayed bodies) and for `SearchPattern`/`FindReferences` exact matching.
6. **DB compaction**: shared `db.Compact(ctx, conn, opts)` doing optional `PRAGMA auto_vacuum=INCREMENTAL` + full `VACUUM` (the VACUUM applies the mode change), reporting bytes before/after. `--incremental` flag → `PRAGMA incremental_vacuum` only (cheap, no full rewrite, requires auto_vacuum already incremental). Both `/db-compact` and `pando db-compact` route to the primary writer (VACUUM cannot run as secondary). New IPC RPC `db.compact` so the CLI, when another instance holds the DB, asks the running primary to compact; if no instance is running, the CLI becomes primary and compacts directly.

## Phases

### Phase 1 — Backward-compatible migration (FTS contentless + drop column)
New file `internal/db/migrations/2026XXXXXXXXXX_code_index_drop_source_code.sql`:
- Up (ORDER MATTERS): create `code_symbols_fts_new` contentless (`content=''`, `contentless_delete=1`, same 4 columns, `tokenize='porter unicode61'`); `INSERT INTO code_symbols_fts_new(rowid,name,name_path,doc_string,source_code) SELECT rowid,name,name_path,doc_string,source_code FROM code_symbols;` (repopulate BEFORE dropping); drop triggers ai/au/ad; `DROP TABLE code_symbols_fts`; `ALTER TABLE code_symbols_fts_new RENAME TO code_symbols_fts`; `ALTER TABLE code_symbols DROP COLUMN source_code`; recreate ONLY the delete trigger `code_symbols_fts_ad` as `INSERT INTO code_symbols_fts(code_symbols_fts, rowid) VALUES('delete', old.rowid);`.
- Do NOT VACUUM inside the migration (goose wraps in a tx; VACUUM forbidden in tx). Leave reclamation to `db-compact`.
- Down is lossy (text gone): recreate empty `source_code` column + external-content FTS (degraded) OR make Down log a clear warning. Document it.
- Temporary disk peak: new FTS (~67 MB) coexists with old before DROP — acceptable.

### Phase 2 — Indexer write path
- Remove `source_code` from `selectCodeSymbolColumns` and all `scanCodeSymbolRow*` (3 funcs). `sym.SourceCode` left empty until hydrated.
- Both INSERT paths: stop writing `source_code` into `code_symbols`; instead `INSERT INTO code_symbols_fts(rowid,name,name_path,doc_string,source_code) VALUES(...)` using in-memory `sym.SourceCode` within the same tx (after obtaining the symbol rowid — note FTS rowid must equal `code_symbols.rowid`; capture it).
- Reindex DELETE: the `code_symbols_fts_ad` trigger handles FTS removal by rowid automatically on `DELETE FROM code_symbols WHERE file_id=?` — verify it fires; no app change needed for delete.
- `embedSymbols` unchanged (uses in-memory text at index time).
- Remove the now-redundant `SELECT source_code` re-fetch at lines 1131/1162 (replaced by hydration in Phase 3).

### Phase 3 — Hydration on read
- New `internal/rag/code/hydrate.go`: `type sourceCache struct{...}`; `func (c *CodeIndexer) hydrateSource(ctx, sym, cache) (text string, stale bool)`. Resolve abs path via cached project root (`SELECT root_path` once per project); read file once per path into cache; verify against `code_files.file_hash`; slice by byte offsets, fallback to line range on hash mismatch / out-of-range, set stale. Bound max bytes per symbol (reuse `largeSymbolThreshold`).
- Add `Stale bool` to result/`CodeSymbol` output where surfaced (optional flag in tool output).
- Wire `IncludeBody` (FindSymbol/loadChildren) to hydrate instead of re-SELECT.

### Phase 4 — Adapt dependent search paths
- `lexicalBoost`: drop the `SourceCode` field + `sourceMatches` term; BM25 covers source matches.
- `FindReferences`: replace `source_code LIKE ?` with an FTS MATCH on the symbol name (union with existing name/name_path/doc/signature/file_path LIKE on base columns), then hydrate only for display.
- `SearchPattern`: FTS pre-filter (MATCH on tokens of the pattern) to get candidate rowids, then hydrate each candidate from disk and run the regex/substring against live text (this is the "fresh content" path). For pure regex with no usable tokens, fall back to iterating candidates by other filters then hydrate (bounded by limit). Keep matching on name/name_path/signature/doc_string from base columns without disk reads.
- Tool layer `internal/llm/tools/remembrances_code.go`: ensure output paths that printed `source_code` now trigger hydration; keep token-optimization trimming behavior.

### Phase 5 — DB compaction core + CLI `pando db-compact`
- New `internal/db/compact.go`: `func Compact(ctx, conn *sql.DB, opts CompactOptions) (CompactResult, error)` where opts = {Incremental bool, EnableAutoVacuum bool}. Full path: optional `PRAGMA auto_vacuum=INCREMENTAL;` then `VACUUM;`. Incremental path: `PRAGMA incremental_vacuum;`. Report sizeBefore/sizeAfter/freedBytes (via page_count*page_size or os.Stat). Must run outside any tx; ensure single writer.
- New `cmd/db.go`: `var dbCmd` (`Use:"db"`) + subcommand `dbCompactCmd` (`Use:"compact"`, flags `--incremental`, `--auto-vacuum`). RunE: detect running primary via existing IPC ping; if a primary holds the DB → send IPC `db.compact` RPC and print its result; else open RW (become primary), call `db.Compact`, print freed bytes. `init()` registers `rootCmd.AddCommand(dbCmd); dbCmd.AddCommand(dbCompactCmd)`. (Also accept top-level alias `pando db-compact` if desired via a hidden alias.)

### Phase 6 — Slash command `/db-compact` + IPC routing
- Register in `internal/commands/registry.go` `BuiltinCommands()`: `{Name:"db-compact", Description:"Compact the database (VACUUM) and reclaim free space", AcceptsArgs:false}`.
- New IPC RPC `db.compact` in `internal/ipc/protocol` + handler on the primary that calls `db.Compact` on its writer connection and returns `{freedBytes,sizeBefore,sizeAfter}`. This is the single funnel used by the slash command (runs in-app → routes to primary) and the CLI-when-primary-running.
- Dispatch handlers: TUI (`internal/tui/components/dialog/commands.go` — show a confirm + run + toast with freed bytes), ACP (`internal/mesnada/acp/slash_commands.go` — emit a text result), WebUI/API (`internal/api/handlers_commands.go` — POST handler returning the result). Mirror the existing `/compact` handling pattern.
- i18n: add strings in all 7 locales (TUI + WebUI) consistent with project convention.

### Phase 7 — Tests, docs, KB
- Tests (Go): migration up applies and keeps existing symbols searchable (fixture DB with old schema → run goose → assert FTS MATCH still returns rows, source_code column gone); indexer insert/reindex keeps FTS in sync (no source column); `hydrateSource` correctness (byte slice, line fallback on hash mismatch, stale flag, missing file); `SearchPattern`/`FindReferences` parity vs current behavior on unchanged files; `db.Compact` frees freelist (insert+delete churn → compact → page_count drops). Verified command: `go test ./internal/rag/... ./internal/db/... ./internal/api`.
- Docs: README section on DB compaction + `/db-compact` + `pando db-compact`; note auto_vacuum=INCREMENTAL.
- KB: `kb_add_document` summary on completion (`pando/changes/...`). Update MEMORY.md index line.

## Risks & mitigations
- **VACUUM under IPC** — must run on primary, no competing writers; route via IPC RPC. CLI refuses/forwards if a primary is busy. Mitigation: clear error + forward path.
- **Stale offsets after edits** — hash-verify + line fallback + `stale` flag; never return wrong slice silently.
- **Lossy Down migration** — documented; Down recreates empty column only.
- **FTS rowid alignment** — app-side FTS insert must use the exact `code_symbols.rowid`; capture `LastInsertId`/rowid. Keep delete via trigger to avoid drift.
- **Disk peak during migration** — transient new+old FTS coexistence (~67 MB), acceptable.

## Out of scope (future, bigger wins noted for context)
- Embedding quantization float32→int8 (~170 MB potential) — separate plan.
- Retention/pruning of `events`/`messages`/`files` tables (~320 MB) — separate plan.
- On-the-fly reindex on hash mismatch during read.
