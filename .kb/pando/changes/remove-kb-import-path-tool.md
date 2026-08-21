---
created_at: 2026-08-21T14:17:08.725739475Z
updated_at: 2026-08-21T14:17:25.691501279Z
tags:
    - change
    - kb
    - tools
---
# Remove the `kb_import_path` tool (host-only KB sync)

Date: 2026-08-21
Status: DONE
Related: [[pando/plans/kb_wiki_links_plan]], [[pando/reference/memory_tools_analysis]],
[[pando/changes/extension-system-p0-foundations]]

## Why

`kb_import_path` let a model bulk-import any directory into the knowledge base. The
document keys of an import are derived from **the directory the scan is rooted at**,
so importing the same tree from a different root indexes every file a second time
under a second set of keys, with no error and no warning.

That happened in practice: importing `.kb/pando` (instead of `.kb`) created **382
duplicate documents** whose keys lacked the `pando/` prefix, and every subsequent
`kb_search_documents` returned each document twice. Choosing the scan root is host
configuration, not a model decision, so the tool was removed rather than documented
around.

The capability is not lost: Pando already syncs the configured KB directory on
startup — `Remembrances.KBAutoImport` (default true in the generated config) calls
`KBStore.SyncDirectoryWithStats(kbPath, deleteMissing=true)` from
`internal/app/remembrances.go:102`. Closing and reopening Pando reindexes. The
filesystem watcher (`KBWatch`) covers changes while it runs.

## Changes

Removed:

- `internal/llm/tools/remembrances_kb.go` — `kbImportPathToolName`, `KBImportPathTool`,
  `NewKBImportPathTool`, and the `Info`/`Run` block.
- `internal/llm/tools/builtin_names.go` — the builtin-name entry.
- Three registration sites: `internal/llm/agent/tools.go` (agent tool set),
  `cmd/mcp_server.go` (the `pando` MCP server), and
  `internal/mesnada/server/tools.go` (subagent tool set).

Added, in place of the constant block in `remembrances_kb.go`, a comment explaining
why the tool is deliberately absent and pointing at `SyncDirectoryWithStats` for host
code — so it is not reintroduced.

`kb.KBStore.SyncDirectoryWithStats` is untouched; only the tool wrapper is gone.

## Database cleanup performed alongside

The 382 duplicates were deleted directly from the internal SQLite database
(`.pando/data/pando.db`), scoped so that nothing else could be caught:

```sql
DELETE FROM kb_documents
WHERE json_extract(metadata,'$.source_path') LIKE '/www/MCP/Pando/pando/.kb/pando/%'
  AND file_path NOT LIKE 'pando/%';
```

`kb_chunks` and `kb_links` cascade from `kb_documents` (with `PRAGMA foreign_keys=ON`),
and `kb_fts` is an **external-content FTS5 table over `kb_chunks` with no triggers**, so
it does not follow the delete on its own and was rebuilt explicitly:

```sql
INSERT INTO kb_fts(kb_fts) VALUES('rebuild');
INSERT INTO kb_fts(kb_fts) VALUES('integrity-check');
```

Worth remembering for any future direct KB surgery: memories share `kb_documents`
(`memory_key` / `memory_scope` columns), so a blanket `DELETE FROM kb_documents` would
destroy them — always scope the predicate.

## Verification

- `go build ./...` clean; `go test ./internal/llm/tools ./internal/llm/agent` pass.
- No remaining reference to `kb_import_path` in Go code, web-ui, schema or docs
  (only historical KB documents mention it).
- Post-cleanup DB state: 382 rows deleted, 367 canonical `pando/…` documents intact,
  0 orphan chunks, 0 orphan links, FTS integrity-check OK.
- `kb_search_documents` now returns one hit per document instead of two.
