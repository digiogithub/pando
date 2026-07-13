---
created_at: 2026-07-13T14:11:24.921878106Z
updated_at: 2026-07-13T14:11:24.921878106Z
tags:
    - change
    - kb
    - wiki
    - links
    - tools
    - mcp
---
# KB Wiki Links — Phase 3: tool surface

Date: 2026-07-13
Status: DONE
Plan: [[kb_wiki_links_plan]] · Previous: [[kb_wiki_links_phase1]], [[kb_wiki_links_phase2_graph]], [[kb_wiki_links_phase4_backfill]]

## What was changed

Phase 3 exposes the link graph built in Phase 2 to the model. The four existing KB tools now
report links when there are any, and a new tool navigates the graph.

### New tool: `kb_related_documents`

`internal/llm/tools/remembrances_kb_links.go` (new file).

- With `file_path`: returns the document's outgoing `links` (each resolved to a document or
  flagged unresolved), its `backlinks`, and the scored `related` neighbours
  (`KBStore.RelatedDocuments`, weights 1.0 outgoing / 0.9 backlink / 0.3 shared tags).
  Optional `limit` (default 10).
- **Without `file_path`**: returns the *wanted concepts* — link targets no document defines yet,
  most-linked first (`KBStore.WantedConcepts`). This is the only surface for wanted concepts;
  folding it into the same tool avoided a sixth KB tool.
- When a document is connected to nothing at all, the tool answers with a plain text message
  that also teaches the syntax, rather than an empty JSON object.

Registered in the three required places: `internal/llm/tools/builtin_names.go`
(`kbRelatedDocumentsToolName` in the builtin set, so it is never catalogued by the MCP gateway),
the coder toolset in `internal/llm/agent/tools.go`, and the MCP server in `cmd/mcp_server.go`.
Deliberately **not** in `TaskAgentTools`.

### Enriched tools (`internal/llm/tools/remembrances_kb.go`)

- **`kb_add_document`** — description now teaches `[[concept]]` / `[[concept|label]]` and asks the
  model to link a new document to the plans/features/fixes it builds on. Its confirmation message
  gains a link summary when the document declares links, e.g.
  `Document added: x.md (tags: [...]) — 3 wiki link(s) indexed; no document defines [[Token Ledger]] yet.`
  Applies to the add, update and keyed-memory paths. The link read is advisory: a graph error never
  turns a successful write into a failure.
- **`kb_get_document`** — response gains `links` and `backlinks` sections.
- **`kb_search_documents`** — each hit gains `links` / `backlinks` counts (via the new
  `KBStore.LinkCountsFor`, one pass over `kb_links`), and the response gains
  `related_to_top_result` (top 3 neighbours of the best match) so the model follows a link instead
  of running a second search.

### Store additions (`internal/rag/kb/graph.go`)

- `HasLinks(ctx) (bool, error)` — `SELECT EXISTS(SELECT 1 FROM kb_links)`. The zero-cost guard the
  tools check first.
- `LinkCounts` + `LinkCountsFor(ctx, filePaths)` — outgoing/incoming counts for a set of documents
  in a single scan; documents outside the graph are absent from the map.

## Backward compatibility (the hard constraint)

A knowledge base that never used the syntax must look exactly as it did before. Enforced by
`HasLinks` short-circuiting `attachDocumentLinks` and `LinkCountsFor`, and by the view helpers
(`kbLinkViews`, `kbBacklinkViews`, `kbRelatedViews`) returning `nil` for empty input so the keys are
**omitted from the response entirely** — not present-but-empty. Counts use `omitempty`.

## Verification

- `go build ./...`, `go vet` clean; `go test ./internal/rag/kb ./internal/llm/tools`,
  `go test ./internal/llm/agent ./internal/api ./internal/llm/tooldiscovery` all green.
- New tests: `TestLinkCountsFor`, `TestHasLinksAndCountsOnLinklessKB` (kb), and
  `remembrances_kb_links_test.go` (view helpers return nil for empty input; `unresolvedTargets`
  keeps the author's spelling).
- Live smoke test against `.pando/data/pando.db`: `kb_related_documents` on
  `pando/changes/kb_wiki_links_phase1.md` resolved `[[kb_wiki_links_plan]]` by basename (score 1.0);
  the no-argument form listed the 2 wanted concepts; `kb_get_document` on the plan showed the
  backlink. Temp test file removed after the run.
- Pre-existing, unrelated failure on HEAD: `internal/mcpgateway` catalog TOML tests
  (`toml: expected character =`), reproduced on a clean worktree.

## Remaining

Rest of P4 (`SyncStats.LinksIndexed` for filesystem sync / `kb_import_path`) and P5 (config
`KBWikiLinks` toggle + i18n + `internal/agentsmd` template guidance + docs).
