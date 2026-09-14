---
created_at: 2026-09-14T16:36:58.062927355Z
updated_at: 2026-09-14T16:36:58.062927355Z
tags:
    - fix
    - rag
    - performance
    - search
---
# PANDO-US-0028 — Push a path-prefix filter into the search SQL

Status: COMPLETE (2026-09-14). Part of [[PANDO-EP-0007]] search scale and correctness, story 2 of 3. Builds directly on [[pando/fixes/us0027-stop-selecting-document-body-in-search.md]] (same two SQL strings).

## What changed

- `internal/rag/kb/types.go` — added `PathPrefix string` to `SearchOptions`, documented as SQL-pushed, never a post-fusion Go filter.
- `internal/rag/kb/kb.go`:
  - `searchVector(ctx, queryEmb, limit, pathPrefix string)` and `searchFTS(ctx, query, limit, pathPrefix string)` — both gained a `pathPrefix` parameter. When non-empty, both append `AND d.file_path LIKE ? || '%' ESCAPE '\'` to their query, binding the escaped prefix as a parameter (never string concatenation).
  - New `escapeLikePrefix(prefix string) string` — escapes `\`, `%`, `_` (in that order) so a prefix containing LIKE metacharacters matches literally; only the `'%'` SQL-appends after the bound value is a real wildcard.
  - `searchDocumentsWithOptions` now passes `opts.PathPrefix` into both goroutine calls.
- `internal/llm/tools/remembrances_kb.go` (`KBSearchDocumentsTool`) — added optional `path_prefix` parameter, wired into `kb.SearchOptions.PathPrefix`.
- `internal/api/handlers_remembrances_search.go` (`handleKBSearch`, PANDO-US-0005's REST route) — added `path_prefix` to `kbSearchRequest`, wired the same way, so REST and MCP stay identical (parity test in `handlers_remembrances_search_test.go` still passes since the field is optional/zero-value in existing tests).

No migration, no `collection` column (deliberately out of scope per the story — `file_path` prefixes already give uniqueness).

## Why

`kb_documents.file_path` is globally unique and prefixed by convention (`<corpus>/<project>/...`), but there was no way to filter a search *in SQL* to one prefix — only post-fusion Go filters existed (tags, scope, outdated), all applied to a candidate pool capped at `limit*5`. With multiple projects sharing one KB, a query whose top candidates all belong to another project could silently return zero results for the intended one, even below the requested limit.

## Verification

- New tests in `internal/rag/kb/search_pathprefix_test.go`:
  - `TestSearchPathPrefixAvoidsUnderReturn` — 10 "noise" documents (embedding cosine 1.0 with the query, containing the FTS term) vs 2 "target" documents (cosine 0.0, no FTS term at all). An unprefixed `limit=2` search returns 0 of the 2 target docs (proves the under-return failure mode deterministically, no score ties involved). A `PathPrefix: "target/"` search returns both (the full limit). A direct `searchVector(..., "target/")` call with a huge limit returns exactly 2 candidates — the row-count assertion that the SQL filter, not a Go-side filter, scoped the scan.
  - `TestSearchPathPrefixEscapesLikeMetacharacters` — documents with literal `_` and `%` in their paths, plus decoys that would incorrectly match if the metacharacter were treated as an unescaped LIKE wildcard; asserts only the literal-prefix document is returned.
- `go build ./internal/rag/... ./internal/llm/tools/... ./internal/api/... ./internal/app/... ./cmd/... ./internal/mesnada/...` — clean.
- `go test ./internal/rag/... ./internal/llm/tools/... ./internal/api/...` — all green (61s for `internal/llm/tools`, includes the REST/MCP parity test unaffected since `path_prefix` defaults to empty in existing cases).
- Same `internal/agui` pre-existing/unrelated compile failure noted in the US-0027 write-up; not touched, not caused by this change.

## Not done (out of scope per the story)

- No `collection` column / `Document.Collection` field — a separate, larger decision per the story's explicit "do NOT."
- Path-prefix filtering was NOT applied to `GetMemoriesForInjection`'s memory search (`SearchOptions{Tags: []string{"memory"}}` call in `internal/rag/kb/memory.go`) — the story scopes this to `kb_search_documents`/REST only; memory recall has its own `Scope` filter already.
