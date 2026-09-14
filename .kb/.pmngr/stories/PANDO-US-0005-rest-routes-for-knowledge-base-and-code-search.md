---
id: PANDO-US-0005
type: story
title: REST routes for knowledge-base and code search
status: done
priority: high
parent: PANDO-EP-0005
milestone: PANDO-M-0002
author: claude
labels: [rag, api]
estimate: 5
created: 2026-09-13T21:14:28Z
updated: 2026-09-13T21:14:28Z
---

## Description

As a host that already speaks authenticated HTTP to Pando, I want KB and code search over REST, so that I do not have to carry an MCP client just to run a query.

`internal/api/routes.go:144-152` is the complete remembrances block: seven administrative routes, none for search (`POST /remembrances/reindex` at `:147` is code projects only, `handlers_remembrances.go:122`). Add two routes in the same block, backed by methods that are already exported:

- `POST /api/v1/remembrances/kb/search` → `KB.SearchDocumentsWithOptions(ctx, query, limit, kb.SearchOptions{Tags, SortByDate, ExcludeOutdated, Scope})` (`internal/rag/kb/kb.go:671`). Mirror the MCP tool's request and response shapes (`internal/llm/tools/remembrances_kb.go:227-259` for parameters and the limit default of 5, clamped to 20 at `:274-279`; `:296-316` for the result shape, which includes `metadata`, `tags`, `score` and `rank`).
- `POST /api/v1/remembrances/code/search` → `Code.HybridSearch(ctx, projectID, query, limit, langs, symbolTypes)` (`internal/rag/code/indexer.go:1248`). Note that `include_docs`, `min_score`, `offset` and `group_by_file` are not part of `HybridSearch` — they are applied by `rankAndFilterHybrid` in `internal/llm/tools/remembrances_code.go:396-417,:505`. Export that helper (about ten lines) and call it from the handler so REST and MCP rank identically; do not reimplement the ranking.

Auth needs no wiring: `internal/api/server.go:496` exempts `/health` and anything outside `/api/`, and everything else goes through `hasValidToken` (`:448-453`, `X-Pando-Token` header or `?token=`, constant-time compare). CORS already lists `X-Pando-Token` in `Allow-Headers` (`server.go:430-434`).

Do NOT add a generic tool-call bridge next to `GET /api/v1/tools` (`handlers_tools.go:9`) — it would be shorter but exposes every tool over REST. Do NOT expose `hybrid_search_remembrances` (`internal/rag/hybrid.go:34`) as a route: it sorts raw scores from three sources with no normalisation (`hybrid.go:118-121`), so boosted code scores near 1.0 bury KB RRF scores near 0.016.

## Acceptance Criteria

- [ ] Both routes are registered in the remembrances block of `internal/api/routes.go` and reject a non-POST method.
- [ ] The KB response carries `file_path`, `metadata`, `tags`, `score`, `rank` and the chunk content, field-for-field identical to what `kb_search_documents` returns for the same query.
- [ ] The code response applies `include_docs` and `min_score` through the shared `rankAndFilterHybrid`, producing the same ordering as `code_hybrid_search` for the same arguments.
- [ ] A request without a valid token gets `401` before any handler runs (test asserts the handler is not entered).
- [ ] With `Remembrances` or its `KB`/`Code` field nil, both routes answer an empty result rather than panicking, matching `handlers_remembrances.go:32`.
- [ ] `limit` is clamped to the same ceiling as the MCP tool.

## Notes

Size M. Independent of the metadata stories; benefits from the path-prefix filter in PANDO-EP-0007, which should be exposed here once it exists. git-in-track ships its first semantic-search release against the MCP tools (its client boundary is transport-agnostic) and will move to these routes afterwards, so this is an improvement, not a blocker — schedule it after the reindex story.
