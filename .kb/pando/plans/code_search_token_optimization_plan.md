---
created_at: 2026-06-14T21:07:01.203614676Z
updated_at: 2026-06-14T21:07:01.203614676Z
tags:
    - plan
    - architecture
    - rag
    - code-search
    - token-optimization
    - performance
---
# Analysis and Improvement Plan — Pando Code Search Tool Token Optimization

**Date:** 2026-06-14  
**Author:** Claude (Opus 4.8) at user request  
**Goal:** Make the search results from code tools (`code_hybrid_search`, `code_find_symbol`, `code_search_pattern`, `code_get_symbols_overview`) actually useful and stop squandering context tokens on penalizable spend, *without* eliminating their capabilities (hybrid/semantic search, symbol-based search, cross-project). The goal is NOT to remove the implementation: these tools have inherent value versus a `grep`/ripgrep. The goal is to make the *payload* lean and the *ranking* reliable.

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
| LLM tool wrappers (output projection) | `internal/llm/tools/remembrances_code.go` | `CodeHybridSearchTool.Run` (L254), `CodeSearchPatternTool.Run` (L714), `CodeFindSymbolTool.Run` (L378) |
| Fallback to grep in pattern search | `internal/llm/tools/remembrances_code.go` | `CodeSearchPatternTool.fallbackToGrep` (L770) |
| Markdown symbol extraction (BUG root cause) | `internal/rag/treesitter/markdown_extractor.go` | `extractHeading` (L79) |
| Symbol struct | `internal/rag/treesitter/types.go` | `CodeSymbol` (L57) |
| Server MCP tools registration | `internal/mesnada/server/tools.go` | `NewCodeHybridSearchTool` (L108) |
| Paginated session cache (line-based) | `internal/llm/tools/cache_interceptor.go` | `InterceptToolResponse` |

The MCP server reuses the exact same `llmtools.*` wrappers (no second projection), so a single array in the tools layer impacts both the internal agent and the MCP client equally.

---

## 2. Diagnosis — why results squander tokens uselessly

Live reproduction: a single call to `code_hybrid_search(project_id=pando, query="hybrid search...", limit=15)` returned **147 lines / 18,025 bytes**. Results #2 and #3 were the *full body* of `CLAUDE.md` and `AGENTS.md` (~12 KB, ~66 % of payload) dumped into the `name` and `name_path` fields. That's pure noise: the agent already has those files.

### Problem 1 — Markdown spews entire sections as `name`/`name_path` (ROOT CAUSE of bloat)
In `markdown_extractor.go:79-111`, for `section` nodes there is no direct `heading_content`/`inline` child, so it falls back to `name = GetNodeContent(node, sourceCode)` which captures the *entire section* (header + body). Result: symbols with `name` spanning thousands of characters. Plus `name_path` duplicates that content. This gets indexed and re-emitted on every search.

### Problem 2 — `code_search_pattern` emits raw full `CodeSymbol` directly
In `remembrances_code.go:756`, `"symbols": symbols` serializes the whole struct, including `source_code`, `doc_string`, `metadata`, `start_byte`/`end_byte`, `created_at`/`updated_at`. Unlike `code_hybrid_search` and `code_find_symbol` (which project to a lean `symbolItem`), this tool emits the heaviest payload of the three. `selectCodeSymbolColumns` (L747) always loads `source_code`.

### Problem 3 — No relevance threshold or cutoff
`HybridSearch` repopulates until `limit` is exceeded, even if the queue has near-zero scores. There's no minimum score filter or relative cut-off ("drop results < X % of best"). Tokens are squandered on irrelevant tails.

### Problem 4 — The emitted `score` isn't interpretable
The final `Score` mixes RRF (~0.016), lexical boosts, and FTS boosts in arbitrary units (example: 8.9, 8.2, 7.4…). It's not normalized 0–1, so neither the LLM nor user can use it to decide what deserves reading.

### Problem 5 — No per-result pagination
The tools expose no `offset`/page: to see "the next 10" you must re-execute with higher `limit` (over-fetch). The session cache (`cache_interceptor.go`) only works above 300 lines / 15 KB and is *line-based*: a giant markdown result in one line breaks the preview with no useful pagination. The correct granularity for search is *per result*, not per TOML line.

### Problem 6 — Minimal disambiguating context missing → `Read` forced
Results show `file_path` + `start_line` but no 1-line snippet or containing symbol. The agent ends up doing a follow-up `Read`—exactly what a `grep` with `output_mode=content` would have given inline and cheaper. You're losing the search semantics advantage.

### Problem 7 — Weak doc penalization
`lexicalBoost` only subtracts -0.2 for `.md` files and -0.1 for `namespace` types (L967-972), insufficient: markdown still ranked #2/#3.

### Problem 8 — No dedup/grouping by file
Multiple symbols from the same file are emitted as independent complete entries.

---

## 3. Guiding principle: what to conserve vs. what to fix

- **Conserve** (inherent value over ripgrep): *semantic/vector* search finds by *concept*; *cross-project* search reaches indexed projects outside cwd; structured metadata (`symbol_type`, `signature`, `name_path`) that grep doesn't know.
- **Fix**: payload size and ranking reliability.
- **Route**: literal/regex patterns on the current work tree → ripgrep is optimal (there's already `fallbackToGrep`, expand its use). Semantics/symbols/cross-project → Pando tools.

---

## 4. Implementation plan by phases

### Phase 1 — Eliminate bloat at source (highest ROI, lower risk)
1. **Fix the markdown extractor** (`markdown_extractor.go`): `name`/`name_path` must contain *only* the heading text. For `section` nodes, descend to `atx_heading`/`setext_heading` and extract their `heading_content`; never use section body as name. The body can go to `doc_string`/`source_code` (which aren't emitted by default). Requires **re-indexing** projects.
2. **Defensive safeguard in tool projection**: helper `truncateField` limiting `name`/`name_path`/`signature` to ~200 chars with trailing ellipsis, regardless of symbol type (depth buffer against future extractors).
3. **`code_search_pattern`**: replace `"symbols": symbols` with the same lean projection as `code_hybrid_search` (fields: `symbol_type`, `name`, `name_path`, `file_path`, `start_line`, `signature` + short snippet of match). Remove `source_code`/`metadata`/`timestamps`/`bytes` from payload.

### Phase 2 — Ranking and relevance cutoff
1. **Normalize final score to 0–1** (relative to top) so `score` becomes interpretable.
2. **Threshold + relative cutoff**: drop results with score < 35 % of best hit, or below an absolute floor. Configurable via `.pando.toml`.
3. **Configurable doc downranking**: introduce `kind` (code|doc) in results; `code_hybrid_search` defaults to prioritizing code, with optional `include_docs` flag. Harden the current penalty.
4. **`score_breakdown`** (vector/fts/lexical)*only in debug mode*, not by default.

### Phase 3 — Per-result pagination (using Pando's session paginated cache)
1. Add `offset` (keeping `limit`) to `code_hybrid_search` / `code_find_symbol` / `code_search_pattern`, returning `total`, `offset`, `has_more`, `next_offset`. Avoids over-fetch.
2. Emit results in **one-line-per-result compact format** so the line-based cache (`cache_interceptor.go`) pages cleanly (1 result = 1 line) instead of multi-line TOML with giant values. Alternative: register output as array-of-results pages in `SessionCache`.
3. Tune auto-cache to paginate beyond N entries with top-K preview + `cache_id` for the rest.

### Phase 4 — Cheap disambiguating context
1. Include **one-line snippet per result** (the `signature` line or `start_line`), capped, to avoid follow-up `Read`.
2. Optional: **group by file**—single entry with symbols anodized in a compact structure.

### Phase 5 — Smart routing and guidance
1. Sharpen tool descriptions: use `code_search_pattern` for concept/cross-project; prefer `grep` for literal text in cwd. Detect purely literal patterns and suggest grep (cheaper) on top of existing fallback.
2. Telemetry: log tokens emitted per search to measure improvement.

### Phase 6 — Tests and validation
1. Tests: markdown `name` is only the heading; payload size per result bounded; `offset` paginates; cutoff score eliminates noise; lean projection in `search_pattern`.
2. Token benchmark before/after on representative queries (target: reduce > 50 % in doc-heavy query cases like CLAUDE.md/AGENTS.md).
3. Verified command: `go test ./internal/rag/code ./internal/llm/tools`.

---

## 5. Recommended order and estimated impact

|| Phase | Effort | Token impact | Risk |
| --- | --- | --- | --- |
| 1 (bloat at source) | Low | **Very high** (eliminates ~66 % noise from the real case) | Low (requires re-index)|
| 2 (ranking/cutoff)*| Medium | High (cuts irrelevant tail) | Medium (tuning thresholds)|
| 3 (pagination) | Medium | Medium (avoids over-fetch) | Low |
| 4 (snippet/dedup) | Medium | Medium (avoids re-Reads) | Low |
| 5 (routing) | Low | Medium | Low |
| 6 (tests) | Low | — (guarantee) | Low |

**Start with Phase 1**: captures the bulk of savings at lowest risk. Phases 2–4 refine the quality of remaining results.

