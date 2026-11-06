---
created_at: 2026-11-09T10:10:08.980593908Z
updated_at: 2026-11-09T10:10:08.980593908Z
tags:
    - change
    - release
    - rag
---
# Release: rag/v0.1.1, kb.Search full-text uses OR by default

**Date:** 2026-11-09. José approved the scope (relayed by Pips in #106) and confirmed tag and push directly in the session.

## Problem
gintrack reported (#103) that the full-text legs of `kb.Search` required every query word: a four-word query missed documents containing only some of the words. The cause was that Search reused the legacy `sanitizeFTSQuery`, which quotes each word and joins them with spaces, an implicit FTS5 AND.

## Change (commit d8d7f6e4 on `rag-api`)
- `rag/kb/search_api.go`:
  - New `ftsMatch(text, matchAll)`. By default it joins the quoted words with ` OR `; with matchAll it delegates to `sanitizeFTSQuery` (AND).
  - `Search` builds the match once and passes it to both `apiSearchFTS` (chunks) and `apiSearchFields` (front-matter fields). Those two functions now take the MATCH expression instead of the raw text.
  - bm25 and RRF rank documents that match more, and rarer, words first.
- New field `Query.MatchAll bool` (default false), which restores AND.
- Pando's legacy `SearchDocuments*` and `sanitizeFTSQuery` are unchanged, so Pando's behaviour is the same.
- Test `TestSearchFTSMatchesAnyWordUnlessMatchAll` in `rag/kb/search_api_test.go` covers partial matches, ordering (all-words document first) and MatchAll, on both legs.

## Release
- Annotated tag `rag/v0.1.1` (tag object 5274c527) on d8d7f6e4. Both `rag-api` and the tag were pushed to origin; `main` was not touched.
- `GOPROXY=https://proxy.golang.org go list -m github.com/digiogithub/pando/rag@v0.1.1` returns `v0.1.1`.
- gintrack and Pips were notified.

## Verification
- Sluice can't run because the jj workspace has no `.git`, so the checks were run by hand.
- Root module: `go build ./...`, `go vet ./...` and `go test ./internal/db ./internal/rag/... ./internal/llm/tools ./internal/api ./internal/extensions`.
- `rag/`: `go vet ./...`, `go test ./...` and `go test -race ./kb ./migrate`.
- All passed.

Related: [[pando/changes/rag-v0.1.0-release.md]], [[pando/changes/rag-api-phase2.md]]
