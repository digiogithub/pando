---
created_at: 2026-07-13T13:42:38.795005592Z
updated_at: 2026-07-13T13:42:38.795005592Z
tags:
    - change
    - kb
    - wiki-links
    - graph
    - phase2
---
# KB wiki links — Phase 2: graph queries

Date: 2026-07-13. Plan: `pando/plans/kb_wiki_links_plan.md`. Follows `pando/changes/kb_wiki_links_phase1.md` and `pando/changes/kb_wiki_links_phase4_backfill.md`.

## What was changed

New file `internal/rag/kb/graph.go` — the read side of the wiki graph. Phase 1 stores links with an **unresolved** `target_slug`; this phase resolves them at **query time**, which is what lets a link point at a document that does not exist yet and lets a renamed document be found again without rewriting any stored row.

### Types

- `LinkTarget{WikiLink, FilePath, Resolved}` — a link plus the document it resolves to.
- `Backlink{FilePath, Slug, Label}` — an incoming link.
- `RelatedDocument{FilePath, Score, Reasons}` — a scored neighbour, with human-readable reasons ("links to it", "links here", "shared tags: kb, wiki").
- `WantedConcept{Slug, Raw, Count, Sources}` — a link target no document defines (the wiki "red link").

### Resolver

`linkResolver` (built by `newLinkResolver`) indexes every document by slug in **three precedence passes**: full path, then basename, then declared `aliases`. First-claim wins, and documents are scanned in `ORDER BY file_path`, so resolution is deterministic when two documents share a basename: the bare `[[notes]]` belongs to `a/notes.md`, and `b/notes.md` is only reachable as `[[b/notes.md]]`. It loads `id, file_path, metadata` only — **never `content`** — and the same scan feeds tags for the relatedness score.

### Queries (all on `*KBStore`)

- `OutgoingLinks(ctx, filePath) []LinkTarget` — `LinksFrom` + resolution.
- `Backlinks(ctx, filePath) []Backlink` — matches `kb_links.target_slug` against every slug the document answers to (path, basename, aliases), then **filters by resolution**: a link to `[[notes]]` is a backlink only of the document that actually won that slug, not of every `notes.md`.
- `RelatedDocuments(ctx, filePath, limit) []RelatedDocument` — weighted union: outgoing 1.0, backlink 0.9, shared tags 0.3 (`weightOutgoing`/`weightBacklink`/`weightSharedTags`). Each **kind** of connection is counted once per neighbour, so linking to the same document by both path and basename is still one link. Sorted by score, then path; `limit <= 0` → `defaultRelatedLimit` (10).
- `WantedConcepts(ctx, limit) []WantedConcept` — unresolved targets grouped by slug, most-linked first, keeping the **first raw spelling** an author used (reads better than the slug when suggesting a title).

## Backward compatibility

Explicit requirement: a KB written by an older Pando has no links at all, and that is a valid state, not a degraded one. Every query returns an empty result and a nil error in that case, including for a document that does not exist. Pinned by `TestGraphOnLinklessKB`.

## Verification

- `go build ./...`, `go vet ./internal/rag/kb` clean; `go test -race ./internal/rag/...` green.
- New `internal/rag/kb/graph_test.go`: path/basename/alias resolution, unresolved targets kept, precedence + the ambiguous-basename backlink rule, labelled alias backlinks, ranking (outgoing > backlink > shared tags, unconnected excluded), accumulated signals counted once per kind, wanted concepts (count, ordering, raw spelling, sources), and the link-less-KB guarantee.
- Live smoke test against `.pando/data/pando.db`: `[[kb_wiki_links_plan]]` written in the Phase 1 change doc resolves by basename to `pando/plans/kb_wiki_links_plan.md` (related: score 1.0, "links to it"); two wanted concepts surface, both pointing at memory-file names that have no KB document.

## Next

Phase 3 — tool surface: enrich `kb_get_document` (links + backlinks), `kb_add_document` (document the syntax in its description, report links indexed / unresolved), `kb_search_documents` (link counts), and add `kb_related_documents` (registered in `builtin_names.go`, `cmd/mcp_server.go` and the coder toolsets in `internal/llm/agent/tools.go`; **not** the TaskAgent). The tools must omit link sections entirely when a document has no links, so a link-less KB looks exactly as it does today.
