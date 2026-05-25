# Plan: Fix code indexing tools result quality and timestamp behavior

Date: 2026-05-25
Project: pando
Status: proposed

## Summary

Analysis of the current code intelligence tools found three main issues:

1. `created_at` and `updated_at` are returned incorrectly for indexed symbols because query paths do not hydrate those fields from the database.
2. `code_hybrid_search` returns low-quality or irrelevant matches for realistic natural-language and identifier-heavy queries.
3. `code_search_pattern` does not match its contract: `isRegex` is ignored, search is limited to `source_code`, and it frequently falls back to grep.

There is also a consistency gap between documented capabilities and exposed tools, especially around `code_find_references`.

## Findings

### 1. Timestamp fields are indexed but not returned

The indexer writes timestamps correctly:
- `internal/rag/code/indexer.go` inserts `created_at` and `updated_at` into `code_projects` during `IndexProject`
- `internal/rag/code/indexer.go` inserts `created_at` and `updated_at` into `code_symbols` during `indexFile`

But symbol query paths do not select or scan those columns, so `CodeSymbol.CreatedAt` and `CodeSymbol.UpdatedAt` remain Go zero values (`0001-01-01T00:00:00Z`).

Affected query paths include:
- `FindSymbol`
- `loadChildren`
- `GetSymbolsOverview`
- `vectorSearch`
- `ftsSearch`
- `FindReferences`
- `SearchPattern`

The same partial hydration also omits fields such as `start_byte`, `end_byte`, `signature`, and `metadata` in several paths.

### 2. Hybrid search ranking quality is weak

Observed behavior:
- `code_find_symbol` can find the relevant code in `internal/rag/code/indexer.go`
- `code_hybrid_search` for a query about code tools and timestamp fields returns irrelevant symbols such as editor package entries and unrelated SDK content

Likely causes:
- `sanitizeFTSQuery()` turns natural-language queries into a rigid quoted-token expression, which is not robust for long mixed queries
- vector search may contribute noisy results for identifier-heavy queries
- reciprocal-rank fusion has no quality guardrail or fallback when one side is weak or noisy
- FTS scoring does not explicitly boost structural matches in `name` / `name_path`

### 3. `code_search_pattern` does not honor its advertised behavior

Current implementation issues:
- `isRegex` is ignored entirely
- search is limited to `source_code`
- pattern search does not consider `name`, `name_path`, `doc_string`, `signature`, or metadata
- users can easily get fallback grep results even when indexed data should be the primary search source

### 4. Capability exposure is inconsistent

The backend contains `CodeIndexer.FindReferences(...)`, and prompts/documentation mention `code_find_references`, but the tool surface is not consistently exposed through the code tools implementation. That creates a mismatch between documented and actual capabilities.

## Modification Plan

## Phase 1: Fix field hydration for indexed symbol results

### Goal
Return complete and correct symbol data from all indexed-query code paths.

### Changes
Update all relevant SQL queries and row scanning logic to include and hydrate:
- `start_byte`
- `end_byte`
- `signature`
- `metadata`
- `created_at`
- `updated_at`

Apply this to:
- `FindSymbol`
- `loadChildren`
- `GetSymbolsOverview`
- `vectorSearch`
- `ftsSearch`
- `FindReferences`
- `SearchPattern`

### Expected result
- `created_at` and `updated_at` stop returning zero values
- all code tools become internally consistent in their symbol payloads
- debugging and downstream consumers can rely on timestamps and richer metadata

### Tests
Add tests to verify that:
- `FindSymbol` returns non-zero persisted timestamps
- `GetSymbolsOverview` returns non-zero persisted timestamps
- tool-level `code_find_symbol` responses serialize those fields correctly

## Phase 2: Improve `code_hybrid_search` result quality

### Goal
Make hybrid search return relevant results for realistic developer queries.

### Changes

#### 2.1 Improve FTS query construction
Replace the current simplistic `sanitizeFTSQuery()` approach with a more search-aware query builder that:
- extracts significant identifiers (`code_hybrid_search`, `created_at`, etc.)
- handles mixed natural language and identifier queries more gracefully
- reduces over-strict token matching
- removes or downweights trivial words

#### 2.2 Boost structural matches
Improve ranking so matches in:
- `name`
- `name_path`

score more strongly than incidental matches in `source_code`.

This can be done via:
- weighted `bm25(...)` column scoring in FTS
- or post-processing boosts before fusion

#### 2.3 Add quality-aware fallback behavior
When FTS returns empty/weak results and vector search is noisy, fall back to a more deterministic strategy, for example:
- exact or substring `FindSymbol` lookups on extracted identifiers
- or a structured search over `name`, `name_path`, and content fields

#### 2.4 Reduce noise from long queries
Preprocess long queries to:
- deduplicate tokens
- separate identifiers from natural-language filler
- ignore extremely generic terms when building the search query

### Expected result
Queries about code tools, APIs, or indexed fields should rank the relevant implementation files and symbols ahead of unrelated packages.

### Tests
Create ranking tests with controlled fixtures to verify that queries such as:
- `code_hybrid_search code_find_symbol created_at updated_at`

surface results in files like:
- `internal/rag/code/indexer.go`
- `internal/llm/tools/remembrances_code.go`

before unrelated symbols.

## Phase 3: Make `code_search_pattern` match its contract

### Goal
Ensure the pattern search tool behaves honestly and usefully.

### Changes

#### 3.1 Handle `isRegex` correctly
Choose one of these approaches:
- implement regex support properly
- or reject `isRegex=true` explicitly with a clear error instead of silently ignoring it

#### 3.2 Expand indexed search scope
Search should not be limited to `source_code`. Extend support to relevant indexed fields such as:
- `name`
- `name_path`
- `doc_string`
- `signature`
- optionally metadata

#### 3.3 Improve fallback transparency
If indexed search cannot satisfy the request and fallback behavior is used, the result should clearly explain why.

### Expected result
The tool becomes predictable, accurate, and aligned with its public description.

### Tests
Add tests for:
- literal pattern matches in `source_code`
- matches present only in `name_path`
- regex behavior (supported or explicitly rejected)

## Phase 4: Align backend, tools, and capability docs

### Goal
Remove mismatches between implementation and advertised capabilities.

### Changes
- expose `code_find_references` through the code tool layer if it is intended to be supported
- otherwise remove it from prompts/capabilities until implemented end-to-end
- review JSON response shapes across all code tools for consistency

### Expected result
Users and agents see only capabilities that actually work, with consistent payloads.

## Phase 5: Add integration coverage for code tools

### Goal
Catch real end-to-end regressions in the tool layer.

### Changes
Add tests covering:
- `code_find_symbol` timestamp serialization
- `code_hybrid_search` relevance for realistic queries
- `code_search_pattern` contract and fallback behavior

### Expected result
Future regressions in indexing/query/tool wiring are caught at the layer where users experience them.

## Recommended implementation priority

### High priority
1. Fix symbol query hydration for `created_at` / `updated_at` and related fields
2. Improve hybrid search ranking and fallback behavior
3. Correct or explicitly narrow `code_search_pattern` behavior

### Medium priority
4. Align `code_find_references` exposure with actual support
5. Add end-to-end integration tests for the code tools

## Success criteria

The work should be considered complete when:
- indexed symbol results return correct non-zero timestamps
- code search tools return consistent structured symbol payloads
- hybrid search ranks relevant implementation symbols ahead of unrelated code for realistic queries
- pattern search either supports regex properly or rejects it explicitly
- documented code-tool capabilities match the actual tool surface