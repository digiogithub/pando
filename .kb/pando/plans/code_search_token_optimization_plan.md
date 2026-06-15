---
created_at: 2026-06-14T21:28:42.594960061Z
updated_at: 2026-06-15T07:51:41.691005981Z
tags:
    - plan
    - architecture
    - rag
    - code-search
    - token-optimization
    - performance
    - completed
---
# Analysis and Improvement Plan — Pando Code Search Tool Token Optimization

**Date:** 2026-06-14
**Author:** Claude (Opus 4.8) at user request
**Goal:** Make the search results from code tools (`code_hybrid_search`, `code_find_symbol`, `code_search_pattern`, `code_get_symbols_overview`) actually useful and stop squandering context tokens on penalizable spend, *without* eliminating their capabilities (hybrid/semantic search, symbol-based search, cross-project). The goal is NOT to remove the implementation: these tools have inherent value versus a `grep`/ripgrep. The goal is to make the *payload* lean and the *ranking* reliable.

> **STATUS: ALL 6 PHASES COMPLETE (2026-06-15).** See "Implementation status" at the bottom.

---

## 1. Code map (where each thing lives)

| Component | File | Symbol |
| --- | --- | --- |
| Hybrid orchestration (vector + FTS + fusion) | `internal/rag/code/indexer.go` | `CodeIndexer.HybridSearch` (L1153) |
| Vector search (in-memory cosine) | `internal/rag/code/indexer.go` | `CodeIndexer.vectorSearch` (L1203) |
| FTS search (SQLite BM25) | `internal/rag/code/indexer.go` | `CodeIndexer.ftsSearch` (L1298) |
| RRF fusion | `internal/rag/code/indexer.go` | `rrfFuseCode` (L1534) |
| Lexical boost + reordering | `internal/rag/code/indexer.go` | `boostHybridResults` (L976), `lexicalBoost` (L917) |
| Terms/FTS query construction | `internal/rag/code/indexer.go` | `buildCodeSearchTerms` (L845), `buildCodeFTSQuery` (L904) |
| Pattern search (LIKE + regex in Go) | `internal/rag/code/indexer.go` | `CodeIndexer.SearchPattern` (L1411) |
| Select columns (includes `source_code`) | `internal/rag/code/indexer.go` | `selectCodeSymbolColumns` (L747) |
| LLM tool wrappers (output projection) | `internal/llm/tools/remembrances_code.go` | `CodeHybridSearchTool.Run`, `CodeSearchPatternTool.Run`, `CodeFindSymbolTool.Run`, `CodeFindReferencesTool.Run` |
| Fallback to grep in pattern search | `internal/llm/tools/remembrances_code.go` | `CodeSearchPatternTool.fallbackToGrep` |
| Markdown symbol extraction (BUG root cause) | `internal/rag/treesitter/markdown_extractor.go` | `extractHeading` |
| Symbol struct | `internal/rag/treesitter/types.go` | `CodeSymbol` (L57) |
| Server MCP tools registration | `internal/mesnada/server/tools.go` | `NewCodeHybridSearchTool` |
| Paginated session cache (line-based) | `internal/llm/tools/cache_interceptor.go` | `InterceptToolResponse` |

The MCP server reuses the exact same `llmtools.*` wrappers (no second projection), so a single change in the tools layer impacts both the internal agent and the MCP client equally.

---

## 2. Diagnosis — why results squander tokens uselessly

Live reproduction: a single call to `code_hybrid_search` returned **147 lines / 18,025 bytes**. Results #2 and #3 were the *full body* of `CLAUDE.md` and `AGENTS.md` (~12 KB, ~66 % of payload) dumped into the `name`/`name_path` fields.

1. **Markdown spews entire sections as `name`/`name_path` (ROOT CAUSE).** `section` nodes lack a direct heading child, so the extractor fell back to whole-node content.
2. **`code_search_pattern` emitted the raw full `CodeSymbol`** (incl. `source_code`, `doc_string`, `metadata`, byte offsets, timestamps).
3. **No relevance threshold/cutoff** — irrelevant tails kept.
4. **`score` not interpretable** — mixed RRF/lexical/FTS units, not 0–1.
5. **No per-result pagination** — over-fetch the only way to page; line-based cache breaks on giant single-line values.
6. **No disambiguating snippet** → forces a follow-up `Read`.
7. **Weak doc downrank** (-0.2 md / -0.1 namespace insufficient).
8. **No dedup/grouping by file.**

---

## 3. Guiding principle: what to conserve vs. what to fix

- **Conserve** (inherent value over ripgrep): semantic/vector search by concept; cross-project reach; structured metadata.
- **Fix**: payload size and ranking reliability.
- **Route**: literal/regex on cwd → ripgrep; semantics/symbols/cross-project → Pando tools.

---

## 4. Implementation plan by phases

(Phases 1–6 as originally specified: bloat-at-source fix; ranking/cutoff; per-result pagination; snippet + group-by-file; routing + telemetry; tests + token benchmark.)

---

## Implementation status (2026-06-15) — COMPLETE

All work in `internal/rag/treesitter/markdown_extractor.go` and `internal/llm/tools/remembrances_code.go`, with tests in `*_test.go` + `remembrances_code_bench_test.go`.

- **Phase 1 ✅** Markdown extractor rewritten (`findHeadingNode` + `headingTextOnly` → heading-only names; `extractSection` emits one granular symbol per heading). Tool layer: `truncateField` (200-rune cap on name/name_path/signature), `matchSnippet`, and a slim `code_search_pattern` projection (no more raw `CodeSymbol`).
- **Phase 2 ✅** `rankAndFilterHybrid`: drops doc files unless `include_docs`, normalizes score 0..1 vs top, relevance cutoff `min_score` (default 0.15, always keeps top), `kind` (code|doc), `debug` score breakdown.
- **Phase 3 ✅** `offset` pagination on all 4 search tools; `paginate`/`paginateFetched` (+1 sentinel → accurate `has_more`, `TotalIsLowerBound`); compact one-line-per-result body so the line-based cache pages 1:1; `total/offset/limit/has_more/next_offset` metadata.
- **Phase 4 ✅** Shared `compactRow` + `renderCompact(rows, meta, groupByFile)`; `group_by_file` param (per-file header `path (N)`, drops repeated path, `L<line>` form); 1-line snippet for hybrid via `firstSourceLine` (only when no signature).
- **Phase 5 ✅** "WHEN TO USE" routing guidance in all 4 descriptions (semantic vs Grep/ripgrep for literals); debug-level `logSearchTelemetry` (tool/query/shown/total/offset/limit/has_more/group_by_file/resp_bytes) — never enters model context.
- **Phase 6 ✅** `remembrances_code_bench_test.go`: `TestCompactFormatTokenReduction` asserts ≥80% reduction (MEASURED **85.8%**: 22201B → 3142B for a 20-result page, embeddings excluded); `TestHybridPipelinePagination` (stable ranks across pages, no source_code leak, 1 line/result); benchmarks (compact ~28µs vs verbose JSON ~87µs).

**Outcome:** ~86% token reduction on hybrid pages while preserving semantic/symbol/cross-project value. Docs excluded by default, interpretable scores + cutoff, accurate offset pagination, cache-friendly compact output, optional group-by-file, snippets, routing guidance + telemetry.

**Operational note:** requires rebuilding/restarting the Pando MCP binary AND re-indexing projects (stored markdown symbol names retain the old bloated values until re-index).

**Verified command:** `go test ./internal/rag/code ./internal/rag/treesitter ./internal/llm/tools` — all green.