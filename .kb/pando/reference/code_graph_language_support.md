---
created_at: 2026-06-30T21:17:12.053818022Z
updated_at: 2026-06-30T21:17:12.053818022Z
tags:
    - reference
    - code-graph
    - property-graph
    - languages
    - treesitter
    - ast
    - phase-4
    - lean-ctx
---
# Reference: Code AST & property-graph language support in Pando

**Updated:** 2026-06-30 (lean-ctx Phase 4)
**Source of truth:** `internal/rag/treesitter/languages.go` (grammars),
`internal/rag/treesitter/ast_walker.go` (`RegisterExtractor` — symbol extractors),
`internal/rag/treesitter/edges.go` + `*_edges.go` (`EdgeExtractor` — graph edges).

Three capability tiers, from broadest to narrowest:

1. **Parseable (tree-sitter grammar available)** — a grammar is registered, so the
   file can be parsed. If no dedicated symbol extractor exists, a `GenericExtractor`
   fallback still pulls common declaration nodes.
2. **Symbol extraction (dedicated extractor)** — a language-specific
   `SymbolExtractor` emits structured symbols (functions/types/etc.) used by
   `code_find_symbol`, `code_get_symbols_overview`, `code_hybrid_search`, and the
   `view` signatures/map read-modes.
3. **Property-graph edges (P4 `EdgeExtractor`)** — also emits `imports`/`calls`
   edges into `code_edges`, powering `code_impact_analysis` and `code_related_files`.

## Capability matrix

| Language    | Extensions (sample)         | Parseable | Symbol extractor | P4 graph edges |
|-------------|-----------------------------|:---------:|:----------------:|:--------------:|
| Go          | go                          | ✅ | ✅ Go         | ✅ **yes** |
| TypeScript  | ts, mts, cts                | ✅ | ✅ TypeScript | ✅ **yes** |
| JavaScript  | js, mjs, cjs, jsx           | ✅ | ✅ JavaScript | ✅ **yes** |
| TSX         | tsx                         | ✅ | ⚠️ generic    | ❌ |
| PHP         | php, phtml                  | ✅ | ✅ PHP        | ❌ |
| Python      | py, pyw, pyi                | ✅ | ✅ Python     | ❌ |
| Rust        | rs                          | ✅ | ✅ Rust       | ❌ |
| Java        | java                        | ✅ | ✅ Java       | ❌ |
| Kotlin      | kt, kts                     | ✅ | ✅ Kotlin     | ❌ |
| Swift       | swift                       | ✅ | ✅ Swift      | ❌ |
| C           | c                           | ✅ | ✅ C          | ❌ |
| C++         | cpp, cc, hpp                | ✅ | ⚠️ C grammar  | ❌ |
| Objective-C | m, mm, h                    | ✅ | ⚠️ C grammar  | ❌ |
| Ruby        | rb, rake, gemspec           | ✅ | ⚠️ generic    | ❌ |
| C#          | cs                          | ✅ | ⚠️ generic    | ❌ |
| Scala       | scala, sc                   | ✅ | ⚠️ generic    | ❌ |
| Lua         | lua                         | ✅ | ✅ Lua        | ❌ |
| Svelte      | svelte                      | ✅ | ✅ Svelte     | ❌ |
| Vue         | vue                         | ✅ | ✅ Vue        | ❌ |
| Markdown    | md, markdown                | ✅ | ✅ Markdown   | ❌ |
| TOML        | toml                        | ✅ | ✅ TOML       | ❌ |
| YAML        | yml, yaml                   | ✅ | ⚠️ generic    | ❌ |
| HTML        | html, htm                   | ✅ | ⚠️ generic    | ❌ |
| CSS         | css                         | ✅ | ⚠️ generic    | ❌ |
| Bash        | sh, bash, zsh               | ✅ | ⚠️ generic    | ❌ |

Legend: ✅ dedicated · ⚠️ generic/shared grammar fallback · ❌ not available.

## P4 (property-graph edges) — implemented for: Go, TypeScript, JavaScript

What each edge-capable language emits today:
- **Go** — `imports` (one per `import_spec`, the import path); `calls` (callee =
  trailing selector of `pkg.Func`/`recv.Method`, or the bare identifier).
- **TypeScript / JavaScript** (shared) — `imports` (ESM `import … from "x"` **and**
  CommonJS `require("x")`); `calls` (callee = trailing member for `obj.method()`,
  or the bare identifier).

Not yet emitted by any language: `type_ref` edges (declared in the `EdgeType` enum,
reserved for the 0.5 weight tier).

## How to add P4 support for another language

1. Implement `EdgeExtractor` on the language's existing extractor
   (`ExtractEdges(tree, src, filePath, projectID, symbols) ([]*CodeEdge, error)`),
   emitting `imports`/`calls` (and optionally `type_ref`). Reuse
   `enclosingSymbolID` to attribute call/type-ref edges to their symbol.
2. Nothing else changes — `ASTWalker.ExtractEdges` auto-discovers the interface, the
   indexer persists the edges, and `code_impact_analysis` / `code_related_files`
   immediately use them.

Recommended next: **Python**, then **Rust** (named in the lean-ctx plan), then the
remaining dedicated-extractor languages (Java, Kotlin, Swift, PHP, C).

Related: [[plan_leanctx_context_intelligence]], change doc
`pando/changes/leanctx_p4_code_property_graph.md`.
