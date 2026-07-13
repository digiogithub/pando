---
created_at: 2026-07-13T07:01:03.730579821Z
updated_at: 2026-07-13T07:01:03.730579821Z
tags:
    - plan
    - kb
    - wiki
    - links
    - remembrances
    - architecture
---
# Implementation Plan: KB Wiki Links (document graph + navigation)

Date: 2026-07-13
Status: PLANNED (no phase implemented yet)

## Goal

Turn the Pando knowledge base into a navigable wiki. The agent will be able to write
`[[concept]]` (and `[[concept|label]]`) inside KB markdown documents; each link points to
another KB document that may or may not exist yet. The system must:

1. Extract and index those links as first-class relations (a document graph).
2. Resolve links to real documents (by path, basename slug, or alias) at query time.
3. Expose the graph through the KB tools: outgoing links, backlinks, related documents,
   and "wanted" (unresolved) concepts.
4. Teach the agent (via tool descriptions and AGENTS.md ruleset) that this syntax exists
   and that KB navigation can follow relations, not only search.

## Current-state analysis (verified in code)

- Storage: `kb_documents` (id, file_path, content, metadata JSON, created_at, updated_at,
  memory columns) + `kb_chunks` (embeddings) + `kb_fts` FTS5. Migration
  `20260311000001_add_kb.sql` (+ `20260611000001_add_kb_memory.sql`). **No relation table.**
- Front matter: `internal/rag/kb/frontmatter.go` — `FrontMatter{CreatedAt, UpdatedAt, Tags,
  Key, Scope, Outdated, ExpiresAt, Hits, Importance, Source}` with Parse/Serialize/Merge.
  User-supplied front matter is stripped by `kb_add_document` (`StripFrontMatter`); the
  store owns it.
- Write paths (BOTH must index links):
  - Direct tx: `KBStore.AddDocument` / `UpdateDocument` (= Delete+Add) / `DeleteDocument`
    in `internal/rag/kb/kb.go`.
  - IPC proxy: secondary pre-computes chunks+embeddings and sends `kbAddDocumentRequest`;
    primary handles `KBAddDocument`/`KBUpdateDocument`/`KBDeleteDocument` in
    `internal/rag/proxy/dispatcher.go` → `AddDocumentWithEmbeddings`. Link extraction can
    happen primary-side from `Content` (already in the request) → **no protocol change**.
  - Filesystem sync: `SyncDirectoryWithStats` (`sync.go`) + fs watcher (`watcher.go`) +
    `kb_import_path` tool.
- Tools: `internal/llm/tools/remembrances_kb.go` (`kb_add_document`, `kb_get_document`,
  `kb_search_documents`, `kb_delete_document`, `kb_import_path`) +
  `remembrances_hybrid.go` (`hybrid_search_remembrances`). None mention links.
- Precedent: `code_edges` (migration `20260630000001_add_code_edges.sql`, lean-ctx P4) —
  edges stored with **unresolved destination names**, resolved by cheap queries at analysis
  time (`internal/rag/code/graph.go`). Same philosophy applies here: a `[[concept]]` may
  target a document created later, or a doc may be renamed; storing unresolved slugs and
  resolving at read time keeps writes simple and the graph self-healing.
- `[[...]]` syntax already appears organically in existing KB docs (e.g.
  `pando/changes/document_conversion_kb_convert_documents_ui_toggle.md` references
  `[[feature-document-conversion-markitdown]]`), so a backfill will immediately produce a
  useful graph.

## Design decisions

- **Link syntax**: `[[target]]` and `[[target|display label]]`. Target may be a full KB
  path (`pando/features/foo.md`), a path without extension, or a bare slug
  (`feature-foo`). Links inside fenced code blocks and inline code spans are ignored.
- **Slug normalization**: lowercase, trim, spaces/underscores → `-`, strip `.md`. A
  document's implicit slugs = normalized basename + normalized full path. Optional
  explicit `aliases: []` front-matter field adds more.
- **Storage**: new `kb_links` table with `source_document_id` FK → `kb_documents(id)`
  ON DELETE CASCADE (so Delete/Update(=delete+add) cleans up automatically),
  `target_slug` (normalized, unresolved), `target_raw`, `label`, `position`. Indexes on
  source_document_id and target_slug.
- **Resolution at query time** (code_edges precedent): match `target_slug` against
  candidate docs (exact file_path, path sans .md, basename slug, aliases). Unresolved
  links are kept and surfaced as "wanted" concepts — they are a feature (they tell the
  agent what is worth documenting), not an error.
- **Aliases**: stored in front matter (`aliases:`) and mirrored into metadata JSON
  (`{"aliases": [...]}`) so resolution can read them without parsing content.
- **Config**: `RemembrancesConfig.KBWikiLinks` (default **true** — pure additive, cheap).
  Env `PANDO_KB_WIKI_LINKS`.
- **Not in scope (deferred)**: graph-boosted search ranking (link-count as RRF signal),
  visual graph in WebUI, transclusion (`![[...]]`), section anchors (`[[doc#heading]]`).

## Phases

### Phase 1 — Link model: migration + extraction + storage
- New migration `add_kb_links.sql`: `kb_links` table as designed above.
- New `internal/rag/kb/links.go`:
  - `ExtractWikiLinks(body string) []WikiLink` — regex/scanner for `[[...]]` with
    `|label` support, skipping fenced code blocks and inline code. Dedup by slug,
    keep first position.
  - `NormalizeSlug(string) string`.
- Wire storage into all write paths:
  - `AddDocument` / `AddDocumentWithEmbeddings`: extract from content, insert rows in the
    same tx (delete of stale rows unnecessary on add; Update = delete+add so cascade
    handles it).
  - Dispatcher (`KBAddDocument`/`KBUpdateDocument`): nothing extra — both funnel into the
    store methods above on the primary (verify and add extraction there if a path writes
    rows without going through them).
- `FrontMatter.Aliases []string` (`yaml:"aliases,omitempty"`) + merge semantics in
  `MergeFrontMatter` (incoming wins when non-empty) + inject into metadata like tags
  (`InjectAliasesIntoMetadata` / `ExtractAliasesFromMetadata`).
- Tests: `links_test.go` (extraction edge cases: code blocks, labels, nested brackets,
  unicode, dedup) + store tests asserting rows written/cascaded on add/update/delete.

### Phase 2 — Graph queries: resolution, backlinks, related
- New `internal/rag/kb/graph.go` on `KBStore`:
  - `resolveSlugs(ctx, slugs []string) map[slug]*ResolvedDoc` — one pass over
    `kb_documents` metadata (paginated `listDocumentMetadata` already exists) matching
    path / basename / aliases.
  - `OutgoingLinks(ctx, filePath) ([]LinkInfo, error)` — links from the doc, each with
    `target_slug`, `label`, `resolved_path` ("" when unresolved).
  - `Backlinks(ctx, filePath) ([]BacklinkInfo, error)` — docs whose links resolve to this
    doc: match `target_slug IN (docSlugs(filePath))` (its path/basename/alias slugs).
  - `RelatedDocuments(ctx, filePath, limit) ([]RelatedDoc, error)` — weighted union:
    outgoing resolved (1.0) + backlinks (0.9) + optional shared-tags neighbors (0.3,
    capped) — mirrors `code.RelatedFiles` weighting approach.
  - `WantedConcepts(ctx, limit)` — top unresolved slugs by reference count (GROUP BY
    target_slug HAVING unresolved), i.e. concepts the agent mentioned but never wrote.
- Tests: `graph_test.go` — resolution precedence (exact path > basename > alias),
  unresolved handling, backlinks after rename (old slug dangles, new resolves), -race.

### Phase 3 — Tool surface (the agent-facing part)
- `kb_get_document`: response gains `links` (outgoing, with resolved paths or
  `unresolved: true`) and `backlinks` (paths + labels). Description updated to say the
  document may contain `[[wiki links]]` and that these fields enable navigation.
- `kb_add_document`: description documents the wiki syntax explicitly ("you can and
  should link key concepts as `[[slug-or-path]]`; unresolved links are fine — they mark
  concepts worth documenting later"). Response reports `links_indexed` and lists
  unresolved slugs so the agent gets immediate feedback.
- `kb_search_documents`: each result gains a compact `links`/`backlinks` count and
  optional `related` hint (top 2-3 related paths for the best hit) so the agent can hop.
  Keep it token-cheap: counts always, paths only for rank 1.
- New tool `kb_related_documents(file_path, limit?, include_wanted?)` — exposes
  RelatedDocuments + optionally WantedConcepts; description explains the wiki graph and
  when to navigate vs search.
- Registration: `builtin_names.go`, MCP server (`cmd/mcp_server.go`), coder toolsets in
  `internal/llm/agent/tools.go` (NOT TaskAgent — precedent from code graph/pando_stats
  tools). Respect `KBWikiLinks` config: when off, new tool hidden and outputs unchanged.
- Tests: tool-level tests for output shape; builtin_names test update.

### Phase 4 — Filesystem sync, backfill, watcher
- `SyncDirectoryWithStats`: extraction already happens via store add/update — verify the
  proxy sync path also lands links; add `LinksIndexed` to `SyncStats` and to
  `kb_import_path` output.
- Backfill: existing docs have no `kb_links` rows. Add lazy backfill: on first
  `graph.go` query (or at KB startup, async) detect docs with content containing `[[`
  but zero link rows and re-extract (content is already in `kb_documents`; no
  re-embedding needed — pure link insert). Also expose `pando kb relink` CLI subcommand
  for explicit full rebuild.
- Watcher-driven edits on `.kb/` flow through the same store methods — covered; add an
  integration test (edit file on disk → links updated).

### Phase 5 — Config, guidance, docs
- Config: `KBWikiLinks bool` in `RemembrancesConfig` + env + accessor (default true);
  surface as toggle in the existing KB settings section (TUI + WebUI + API) with
  7-locale i18n keys (follow KBConvertDocuments toggle precedent).
- Agent guidance: update `internal/agentsmd/template.md` (the `/improve-agents-md`
  canonical ruleset) documentation-section to instruct linking key concepts with
  `[[concept]]` when writing KB docs and using `kb_related_documents`/backlinks for
  context recovery.
- Docs: README section + KB feature doc `pando/features/kb_wiki_links.md`; update
  `pando/reference/memory_tools_analysis.md` tool reference.
- Final verification: `go build ./...`, `go vet`, targeted `go test -race ./internal/rag/kb
  ./internal/llm/tools`, manual smoke via MCP (`kb_add_document` with links →
  `kb_get_document` shows backlinks → `kb_related_documents` navigates).

## Phase dependency order

P1 → P2 → P3 (agent value delivered here) → P4 (completeness) → P5 (polish). P4 backfill
can be pulled earlier if we want the existing organic `[[...]]` references to light up for
testing P2/P3 against real data.

## Risks / notes

- KB scale is small (hundreds–thousands of docs), so query-time resolution over metadata
  is cheap; no caching needed initially.
- `UpdateDocument` being delete+add means link rows are naturally refreshed; FK CASCADE is
  the only cleanup mechanism needed.
- Memory docs (tag `memory`) also flow through the same store — links work there for free,
  which matches the existing organic `[[name]]` convention used in memory files.
- Must not break byte-identical mirror round-trip: links live in the body, front matter
  only gains optional `aliases`.
