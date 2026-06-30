---
created_at: 2026-06-30T21:16:42.830731034Z
updated_at: 2026-06-30T21:16:42.830731034Z
tags:
    - change
    - lean-ctx
    - token-optimization
    - code-graph
    - property-graph
    - phase-4
    - impact-analysis
    - related-files
---
# Change: lean-ctx Phase 4 — Code property graph (impact & related-file intelligence)

**Date:** 2026-06-30
**Plan:** `pando/plans/leanctx_context_intelligence_plan.md` (Phase 4)
**Status:** DONE (Go + TypeScript + JavaScript edge extraction; Python/Rust deferred)

## What changed

Turned the symbol index into a relationship graph so the agent can ask "what
breaks if I change X" and "what files relate to this file", reusing the AST we
already parse — no new tree-sitter dependency.

### New persistence
- **Migration** `internal/db/migrations/20260630000001_add_code_edges.sql` —
  new `code_edges` table: `(id, project_id, file_id, edge_type, src_file,
  src_symbol, dst_name, dst_path, start_line, created_at)` + 6 indexes
  (project, file, type, dst_name, dst_path, src_symbol). FK `file_id`→`code_files`
  ON DELETE CASCADE. Destinations stay **unresolved** (callee/import names) so
  cross-file/package resolution is a cheap query at analysis time, not a brittle
  index-time join (lean-ctx's pragmatic approach).

### Edge model + extraction (treesitter)
- `internal/rag/treesitter/edges.go` — `EdgeType` (`imports`/`calls`/`type_ref`),
  `CodeEdge` struct, optional `EdgeExtractor` interface, `ASTWalker.ExtractEdges`
  (dispatch; languages without the interface return nil ⇒ graceful degradation),
  `SupportsEdges`/`EdgeCapableLanguages`, `enclosingSymbolID` (attributes a call to
  the smallest function/method/constructor whose byte range contains it), `unquote`.
- `internal/rag/treesitter/go_edges.go` — `(*GoExtractor).ExtractEdges`: `imports`
  (per `import_spec`, DstPath = path string) + `calls` (per `call_expression`,
  DstName = trailing selector for `pkg.Func`/`recv.Method`, else bare identifier).
- `internal/rag/treesitter/typescript_edges.go` — `(*TypeScriptExtractor)` and
  `(*JavaScriptExtractor)` `ExtractEdges` via shared `extractJSFamilyEdges`: ESM
  `import ... from "x"` + CommonJS `require("x")` (both → `imports`), `call_expression`
  → `calls` (member calls resolve to the trailing property). `string_fragment`-aware.

### Indexing pipeline (code)
- `internal/rag/code/indexer.go` — `CodeIndexer.graphEnabled` (default true) +
  `SetGraphEnabled`/`GraphEnabled`. `indexFile` extracts edges from the same tree
  (best-effort: a failure never blocks symbol indexing) and threads them through
  **both** write paths: local tx (delete old `code_edges` by file_id on reindex +
  `insertEdges`) and the IPC proxy request. `codeIndexFileRequest.Edges` added.
  `IndexFileDirect` now takes `edgesJSON json.RawMessage` and persists them
  (primary IPC path). `internal/rag/proxy/dispatcher.go` mirrors `Edges` and
  forwards it.
- `internal/rag/code/graph.go` (NEW) — re-exports `CodeEdge`/`EdgeType`;
  `insertEdges`; **`ImpactAnalysis(projectID, symbol, depth, limit)`** = reverse
  `calls` BFS by name (joins `code_edges.src_symbol`→`code_symbols`, dedup by
  caller id, depth-capped, `truncated` flag); **`RelatedFiles(projectID, file,
  limit)`** = weighted neighbors blending resolved imports (1.0) + bidirectional
  call coupling (0.8 + small capped count bonus). Relative ESM specifiers resolved
  against the project file list (`resolveRelativeImport`, tries `.ts/.tsx/.js/.jsx/
  .mjs/.cjs` + `index.*`); bare/package paths rely on call coupling. Sorted by
  score desc then path; `round2`, `sqlInClause`, `definitionSymbolTypes`.

### Tools / surfaces
- `internal/llm/tools/graph_tools.go` (NEW) — `code_impact_analysis` (project_id,
  symbol, depth, limit) and `code_related_files` (project_id, file, limit). Slim
  structured responses. Registered in `builtin_names.go`, the coder toolsets
  (`internal/llm/agent/tools.go` — NOT the minimal TaskAgent set), and the MCP
  server (`cmd/mcp_server.go`).

### Config
- Reused the pre-wired `[TokenOptimization]` knobs: `BuildCodeGraph` (default true)
  and `RelatedFilesHint` (default false), already present in the struct + viper
  defaults + Token Optimization settings UI (WebUI toggles + 7-i18n). Added
  accessors `Config.BuildCodeGraphEnabled()` / `RelatedFilesHintEnabled()`
  (nil-safe). `internal/rag/service.go` calls `SetGraphEnabled(
  config.Get().BuildCodeGraphEnabled())` at indexer construction.

## Language coverage (this increment)
- **Edge extraction implemented:** Go, TypeScript, JavaScript.
- **AST/symbol support only (no edges yet):** PHP, Rust, Java, Kotlin, Swift, C,
  Python, Lua, Svelte, TOML, Markdown, Vue (+ generic fallback). Adding edges = just
  implement `EdgeExtractor` on the extractor; the rest of the pipeline is generic.
- Full matrix saved separately in `pando/reference/code_graph_language_support.md`.

## Verification
- `go build ./...` ✓; `go vet` on treesitter/code/tools/rag ✓.
- New tests:
  - `internal/rag/treesitter/edges_test.go` — Go imports+calls (qualified→trailing
    selector, enclosing-symbol attribution), TS relative imports + member calls, JS
    `require()`→imports, `SupportsEdges`/capability (TOML has none).
  - `internal/rag/code/graph_test.go` — impact depth-1/depth-2 transitive + unknown
    symbol; related files imports+calls reasons & score, reverse direction, empty.
  - `internal/rag/code/indexer_test.go` manual schema gains `code_edges`;
    `hydrate_test.go` `IndexFileDirect` calls updated for the new `edgesJSON` arg.
- `go test -race ./internal/rag/treesitter ./internal/rag/code` ✓;
  `go test ./internal/llm/agent ./internal/api ./internal/llm/tools` ✓.
- Pre-existing `internal/config` failures (`TestDefaultConfigTemplateEnablesPando…`,
  `TestMesnadaDelegationWarmDefaultsUnderShadowing`) remain — unrelated (init.go
  template refactor + viper nested-default shadow), not touched by P4.

## Deferred (documented, not done)
- `type_ref` edges (declared in the enum, not yet emitted) — would feed the 0.5
  weight tier.
- Optional token-bounded `[related: …]` footer on `view`/`code_hybrid_search`
  (gated by `RelatedFilesHint`, default off) — plan marks it optional.
- `code_repomap` via personalized PageRank — plan marks it "Stretch".
- Python/Rust (then others) edge extractors — additive via `EdgeExtractor`.

Closes Phase 4. Remaining lean-ctx phase: P6 (optional transcript compaction /
session brief / budget guard).
