---
created_at: 2026-06-25T22:01:11.36532658Z
updated_at: 2026-06-25T22:01:11.36532658Z
tags:
    - plan
    - analysis
    - rtk
    - lean-ctx
    - integration
    - implementation
---
# Integration Analysis and Implementation Plan: RTK + lean-ctx in Pando

**Date:** 2026-06-25
**Goal:** Incorporate the best features from RTK and lean-ctx into Pando that are not yet implemented

## 1. Comparative Feature Matrix

| Feature | RTK | lean-ctx | Pando (Current) | Priority |
|---------|-----|----------|-----------------|----------|
| CLI output compression | ✅ 40+ commands | ✅ 95+ tools | ❌ None | HIGH |
| AST-based code compression | ❌ | ✅ tree-sitter 18 langs | ❌ | HIGH |
| Session continuity (CCP) | ❌ | ✅ Cross-session memory | Partial (kb + memory) | MEDIUM |
| MCP server/tools | ❌ | ✅ 77+ tools | Partial (code tools) | MEDIUM |
| Token analytics | ✅ SQLite tracking | ✅ Metrics dashboard | ❌ | HIGH |
| Hook integration | ✅ Shell hooks | ✅ Shell hooks + MCP | ❌ | LOW |
| Property graph | ❌ | ✅ Import/call tracking | Partial (code index) | MEDIUM |
| Entropy filtering | ❌ | ✅ Shannon entropy | ❌ | MEDIUM |
| LITM optimization | ❌ | ✅ Attention-aware layout | ❌ | LOW |
| TDD (symbol dialect) | ❌ | ✅ λ§∂τε | ❌ | LOW |
| Cognition/memory | ❌ | ✅ Ebbinghaus forgetting | Partial (memory system) | LOW |
| Exit code preservation | ✅ | N/A (MCP-focused) | N/A | N/A |
| Fail-safe fallback | ✅ Raw output fallback | ✅ Safeguard ratio | N/A | N/A |
| Budget enforcement | ❌ | ✅ Token/cost limits | Partial (context window) | MEDIUM |
| Formal verification | ❌ | ✅ Lean4 proofs | ❌ | LOW |
| Adaptive routing | ❌ | ✅ Thompson Sampling | ❌ | MEDIUM |

## 2. What RTK Does That Pando Doesn't

### 2.1 CLI Output Compression (HIGH)
**RTK approach:** Specialized parsers per command (git status, cargo test, npm lint) that strip noise and preserve essential info.
**Pando gap:** When Pando executes shell commands via Bash tool, it gets full raw output. No compression happens.
**Implementation in Pando:**
- Add a `compress_output(command, output)` function in `internal/tools/`
- Start with 10 highest-value commands: git status/diff/log, ls, tree, grep, cargo test, npm test, go test
- Use regex-based pattern matching (like RTK's `rules.rs`)
- Integrate into Bash tool's output handling before returning to LLM

### 2.2 Token Analytics (HIGH)
**RTK approach:** SQLite database tracking every command execution with input/output token counts.
**Pando gap:** No token usage tracking for tool outputs.
**Implementation in Pando:**
- Add `token_tracker` module with SQLite or in-memory tracking
- Track: tool name, input size, output size, estimated tokens saved
- Add `pando gain` or `pando stats` command to show savings
- Integrate with existing `token_usage` tracking in session

### 2.3 Fail-Safe Fallback Pattern (MEDIUM)
**RTK approach:** If filter fails, fall back to raw output. Never block.
**Pando gap:** Tool errors are presented as-is.
**Implementation in Pando:**
- Wrap output compression in try/catch
- On failure, return original output with warning
- Log failures for later analysis

## 3. What lean-ctx Does That Pando Doesn't

### 3.1 AST-Aware Code Compression (HIGH)
**lean-ctx approach:** tree-sitter parsing extracts signatures (functions, classes, types) instead of full source.
**Pando gap:** `code_get_symbols_overview` exists but returns full symbol info without compression modes.
**Implementation in Pando:**
- Extend `code_get_symbols_overview` with compression modes: `full`, `signatures`, `compact`
- Use tree-sitter (Go has `smacker/go-tree-sitter`) to extract AST signatures
- Add to `code_hybrid_search` result formatting: compact mode by default
- Add `code_compress` tool that takes a file path and mode, returns compressed output

### 3.2 Property Graph / Code Intelligence (MEDIUM)
**lean-ctx approach:** SQLite-backed graph tracking imports, calls, exports with weighted impact analysis.
**Pando gap:** `code_index_project` does symbol extraction but no relationship graph.
**Implementation in Pando:**
- Extend code indexer to build import/call graph edges
- Add `code_impact_analysis(symbol)` tool — given a symbol, find all dependents
- Add `code_related_files(file)` tool — find files that import or are imported by
- Store in existing SQLite code index DB

### 3.3 Adaptive Compression Routing (MEDIUM)
**lean-ctx approach:** Thompson Sampling bandit learns optimal compression mode per context.
**Pando gap:** All tools return same format regardless of context.
**Implementation in Pando:**
- Add `ContextIntent` detection: is the user exploring, debugging, implementing?
- Route tool outputs through different compression pipelines based on intent
- Start simple: if user asks "show me errors" → failure-focused compression
- If user asks "overview" → signatures/map mode

### 3.4 Token Budget Enforcement (MEDIUM)
**lean-ctx approach:** Configurable limits on tokens, cost, disk, RAM per session.
**Pando gap:** `model.ContextWindow` exists but no per-tool budget enforcement.
**Implementation in Pando:**
- Add `BudgetGuard` that monitors cumulative tool output tokens
- Warn when approaching context window limits
- Auto-compress or truncate outputs that would exceed budget
- Integrate with `context.go` ContextManager

### 3.5 Entropy-Based Filtering (MEDIUM)
**lean-ctx approach:** Shannon entropy on BPE token IDs to drop low-information lines.
**Pando gap:** No information-theoretic filtering.
**Implementation in Pando:**
- Add `entropy_filter(text, threshold)` utility
- Calculate Shannon entropy per line
- Drop lines below threshold (low information content)
- Apply to large file reads and log outputs

### 3.6 Session Cache with F-References (MEDIUM)
**lean-ctx approach:** If file hasn't changed, use "F-reference" back to previous context (~13 tokens).
**Pando gap:** `pando_cache` exists but doesn't do content-hash deduplication.
**Implementation in Pando:**
- Add MD5/content-hash to `pando_cache` entries
- Before reading a file, check if content hash matches cached version
- If match, return reference token instead of full content
- Track cache hit rate for analytics

### 3.7 LITM-Aware Context Positioning (LOW)
**lean-ctx approach:** Priority tiers (P1/P2/P3) with attention-aware layout.
**Pando gap:** Context is linear, no priority-based ordering.
**Implementation in Pando:**
- Add priority tiers to context injection: P1 (critical), P2 (important), P3 (reference)
- Place P1 at prompt boundaries (start/end)
- Apply to checkpoint rebuild context and tool result formatting

## 4. Implementation Plan

### Phase 1: CLI Output Compression (Week 1-2)
**Goal:** Reduce token consumption from shell command outputs by 60-80%

**Tasks:**
1. Create `internal/tools/outputcompressor/` package
2. Implement regex-based pattern library for top 10 commands:
   - `git status`, `git diff`, `git log`
   - `ls`, `tree`, `find`
   - `grep`, `rg`
   - `cargo test`, `go test`, `npm test`
3. Add `compress_output(command string, raw string) string` function
4. Integrate into Bash tool output handling
5. Add fail-safe fallback (raw output on compression error)
6. Write tests with real command outputs

**Files to create/modify:**
- `internal/tools/outputcompressor/compressor.go`
- `internal/tools/outputcompressor/patterns.go`
- `internal/tools/outputcompressor/patterns_test.go`
- `internal/tools/bash.go` (add compression call)

### Phase 2: Token Analytics (Week 2-3)
**Goal:** Track and report token savings across all tool outputs

**Tasks:**
1. Create `internal/analytics/` package
2. Implement `TokenTracker` with SQLite or in-memory ring buffer
3. Track: tool_name, input_chars, output_chars, estimated_tokens, saved_tokens
4. Add `pando stats` CLI command
5. Add `pando_stats` MCP tool for LLM visibility
6. Generate daily/weekly reports

**Files to create/modify:**
- `internal/analytics/tracker.go`
- `internal/analytics/report.go`
- `cmd/stats.go`
- `internal/mcp/tools_stats.go`

### Phase 3: AST-Aware Code Compression (Week 3-4)
**Goal:** Add signature extraction and compression modes to code tools

**Tasks:**
1. Add `smacker/go-tree-sitter` dependency
2. Implement `SignaturesExtractor` for Go, TypeScript, Python, Rust
3. Add compression modes to `code_get_symbols_overview`: full, signatures, compact
4. Create `code_compress` MCP tool
5. Add mode parameter to `code_hybrid_search` results
6. Default to compact mode for search results

**Files to create/modify:**
- `internal/codeindex/signatures/extractor.go`
- `internal/codeindex/signatures/languages.go`
- `internal/mcp/tools_code_compress.go`
- `internal/mcp/tools_code.go` (modify existing)

### Phase 4: Property Graph Extension (Week 4-5)
**Goal:** Build import/call graph for impact analysis

**Tasks:**
1. Extend `code_index_project` to extract edges (imports, calls)
2. Store edges in SQLite `edges` table
3. Implement `code_impact_analysis(symbol)` tool
4. Implement `code_related_files(file)` tool
5. Add weighted impact scoring

**Files to create/modify:**
- `internal/codeindex/graph.go`
- `internal/codeindex/graph_test.go`
- `internal/mcp/tools_code_impact.go`
- `internal/mcp/tools_code_related.go`

### Phase 5: Adaptive Routing + Budget (Week 5-6)
**Goal:** Intelligent compression selection and token budget enforcement

**Tasks:**
1. Implement `ContextIntent` detector (explore/debug/implement/review)
2. Create compression router that selects mode based on intent
3. Implement `BudgetGuard` with configurable limits
4. Integrate budget checking into tool execution pipeline
5. Add warnings when approaching limits

**Files to create/modify:**
- `internal/context/intent.go`
- `internal/context/router.go`
- `internal/context/budget.go`
- `internal/tools/tool_executor.go`

## 5. Expected Impact

| Metric | Before | After (Phase 1-2) | After (All Phases) |
|--------|--------|-------------------|-------------------|
| Shell output tokens | 100% | 20-40% | 20-40% |
| Code search results | 100% | 100% | 30-60% |
| Context utilization | Linear | Linear | Priority-optimized |
| Token visibility | None | Per-tool stats | Full analytics |
| Cold-start cost | High | High | ~400 tokens (CCP) |

## 6. Implementation Priority

1. **Phase 1 (CLI Output Compression)** — Highest ROI, easiest to implement
2. **Phase 2 (Token Analytics)** — Essential for measuring Phase 1 impact
3. **Phase 3 (AST Compression)** — High value for code-heavy sessions
4. **Phase 4 (Property Graph)** — Medium value, builds on existing indexer
5. **Phase 5 (Adaptive + Budget)** — Medium value, sophisticated but complex

## 7. Risk Mitigation

- **Phase 1-2:** Low risk, additive features, no breaking changes
- **Phase 3:** Medium risk — tree-sitter C bindings may complicate cross-compilation
- **Phase 4:** Low risk — extends existing code index
- **Phase 5:** Medium risk — adaptive routing needs careful tuning

## 8. Success Criteria

- Phase 1: Shell output tokens reduced by ≥60% for top 10 commands
- Phase 2: Token savings tracked and reported in `pando stats`
- Phase 3: Code search results reduced by ≥40% in compact mode
- Phase 4: Impact analysis works for Go and TypeScript
- Phase 5: Budget warnings trigger before context window exhaustion
