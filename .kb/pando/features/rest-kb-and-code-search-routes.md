---
created_at: 2026-09-14T16:20:24.410028472Z
updated_at: 2026-09-14T16:20:24.410028472Z
tags:
    - feature
    - rag
    - kb
    - api
    - code-search
---

# REST KB and code search routes

Implements [[PANDO-US-0005]] (part of [[PANDO-EP-0005]] "Knowledge base: metadata
fidelity and REST search surface"), built after
[[pando/features/rest-kb-document-upsert-delete-reindex|PANDO-US-0001]] per the
epic's sequencing note. Adds `POST /api/v1/remembrances/kb/search` and
`POST /api/v1/remembrances/code/search` so a host that already speaks the
authenticated REST API can search without an MCP client.

## What changed

- **New file** `internal/api/handlers_remembrances_search.go`:
  - `handleKBSearch` — body mirrors `kb_search_documents`' parameters
    (`query`, `limit`, `tags`, `sort_by_date`, `exclude_outdated`, `scope`).
    Calls `KB.SearchDocumentsWithOptions`, then `KB.LinkCountsFor` and
    `KB.RelatedDocuments` for the same `links`/`backlinks`/
    `related_to_top_result` extras the MCP tool attaches. `kbSearchResultItem`
    duplicates the tool's private `resultItem` shape field-for-field
    (`file_path`, `chunk_content`, `score`, `rank`, `tags`, `created_at`,
    `updated_at`, `metadata`, `links`, `backlinks`); `kbSearchRelatedView`
    likewise duplicates the tool's private `kbRelatedView`. Limit defaults to
    5, clamped to 20 — same as the tool.
  - `handleCodeSearch` — body mirrors `code_hybrid_search`'s parameters
    (`project_id`, `query`, `limit`, `offset`, `languages`, `symbol_types`,
    `min_score`, `include_docs`, `debug`). Calls `Code.HybridSearch` with the
    same offset+limit over-fetch headroom the tool uses, then ranks through
    the newly-exported `tools.RankAndFilterHybrid` (see below) so ordering is
    identical to the MCP tool for the same arguments — the ranking algorithm
    itself is never reimplemented. Limit defaults to 20, clamped to 50 — same
    as the tool. `group_by_file` is intentionally not accepted: it only
    changes the MCP tool's compact *text* rendering (`renderCompact`), which
    has no equivalent in a structured JSON response.
  - Both routes answer `200` with an empty result (`count: 0`, empty
    `results`) rather than a panic or 503 when `Remembrances`/`KB`/`Code` is
    nil — this is a read/list route, so it follows the
    `handleListCodeProjects` (`handlers_remembrances.go:32`) pattern, not the
    503-on-write pattern US-0001's write routes use.
- **`internal/api/routes.go`**: two lines added to the remembrances block,
  right after the three US-0001 lines.
- **`internal/llm/tools/remembrances_code.go`**: added `HybridResultItem`
  (a type alias for the existing private `hybridResultItem`) and
  `RankAndFilterHybrid` (an exported one-line wrapper around the existing
  private `rankAndFilterHybrid`). Purely additive — no renames, no behavior
  change to `code_hybrid_search` or its tests. This was necessary to satisfy
  the story's explicit instruction ("do not reimplement the ranking") and is
  the one change in this story that falls outside `internal/api`/
  `internal/rag`; it was the minimal surgical option once outgrown from
  fully private.

## Design notes

- Did **not** export `kb_search_documents`'s local `resultItem`/
  `kbRelatedView` types the same way, since those are unexported types
  scoped to a single function body / a different file
  (`remembrances_kb_links.go`) with no single ten-line export point; instead
  `internal/api` holds its own small mirror structs built from already
  -exported `kb` package pieces (`SearchDocumentsWithOptions`,
  `LinkCountsFor`, `RelatedDocuments`, `RelatedDocument`). Verified
  field-for-field equal to what the store itself returns via
  `TestHandleKBSearch_MatchesKBSearchDocumentsToolShape`.
- The code-search parity test
  (`TestHandleCodeSearch_MatchesCodeHybridSearchToolOrdering`) runs the real
  `tools.NewCodeHybridSearchTool` against the same indexer/arguments and
  `reflect.DeepEqual`s its `results` metadata against the REST response —
  genuine tool-vs-REST equivalence, not a hand-rolled comparison.
- Did not add a generic `POST /api/v1/tools/{name}/call` bridge, and did not
  expose `hybrid_search_remembrances` as a route — both explicitly forbidden
  by the spec (unnormalized cross-source scores would bury KB results under
  boosted code scores).

## Test gotcha (worth remembering)

Both `KBStore.SearchDocumentsWithOptions` and `CodeIndexer.HybridSearch` run
vector and FTS search concurrently on two goroutines. An in-memory SQLite DB
(`sql.Open("sqlite3", ":memory:")`) gives every pooled connection its own
separate database unless the pool is capped at one connection — without
`db.SetMaxOpenConns(1)` right after opening, these searches intermittently
fail with `no such table` from whichever goroutine drew a second, empty
connection. `internal/rag/kb/observer_test.go` already does this for KB; the
new test DB helpers in `internal/api` (`openTestKBDB`,
`setupCodeSearchTestDB`) do it too now.

## Files touched

- `internal/api/handlers_remembrances_search.go` (new) — `handleKBSearch`,
  `handleCodeSearch`, `kbSearchRequest`, `kbSearchResultItem`,
  `kbSearchRelatedView`, `codeSearchRequest`, `paginateHybridResults`,
  `codeSearchEmptyResponse`.
- `internal/api/routes.go` — 2 new `mux.HandleFunc` registrations.
- `internal/llm/tools/remembrances_code.go` — `HybridResultItem` (type
  alias), `RankAndFilterHybrid` (exported wrapper).
- `internal/api/handlers_remembrances_search_test.go` (new) — full test
  suite below, plus `openTestKBDB` in
  `internal/api/handlers_remembrances_kb_test.go` gained
  `db.SetMaxOpenConns(1)`.

## Verification

- `go build ./...` — clean.
- `go test ./internal/api/... ./internal/rag/...` — all green (also ran
  `./internal/llm/tools/...` to confirm the additive export didn't disturb
  existing `code_hybrid_search` tests — green).
- Tests per acceptance criterion, in
  `internal/api/handlers_remembrances_search_test.go`:
  - `TestSearchRoutes_RejectNonPostThroughMux`,
    `TestHandleKBSearch_RejectsNonPost`, `TestHandleCodeSearch_RejectsNonPost`
    — AC1 (registered, non-POST rejected — one through the real mux, proving
    Go's method-routing itself 405s, one at the handler level).
  - `TestHandleKBSearch_MatchesKBSearchDocumentsToolShape` — AC2
    (file_path/metadata/tags/score/rank/chunk_content identical to the
    store's own search results, which is what the tool renders from).
  - `TestHandleCodeSearch_MatchesCodeHybridSearchToolOrdering`,
    `TestHandleCodeSearch_IncludeDocsAndMinScore` — AC3 (include_docs and
    min_score applied through `RankAndFilterHybrid`; ordering
    `reflect.DeepEqual` to the real tool's output for the same arguments).
  - `TestSearchRoutes_RequireValidToken` — AC4 (401 before the handler runs,
    via the real `corsMiddleware(basicAuthMiddleware(authMiddleware(...)))`
    stack with `s.app` left nil so a bypass would nil-deref instead of 401).
  - `TestHandleKBSearch_NilRemembrancesReturnsEmptyNotPanic`,
    `TestHandleCodeSearch_NilRemembrancesReturnsEmptyNotPanic` — AC5.
  - `TestHandleKBSearch_DefaultAndMaxLimit` — AC6 (default 5 / clamp 20 for
    KB); code's default 20 / clamp 50 exercised implicitly by
    `TestHandleCodeSearch_RequiresProjectIDAndQuery` fixture defaults (no
    separate clamp test was added for code — low risk, identical one-line
    clamp logic to the KB case already covered).

Known pre-existing unrelated failures live in `internal/llm/agent` (caveman +
tool-discovery tests) — not touched, not chased.