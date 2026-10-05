---
created_at: 2026-10-06T19:48:18.563082046Z
updated_at: 2026-10-06T19:48:58.412049267Z
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

**Updated:** 2026-10-06 (go-tree-sitter v0.2.0, runtime 0.25.10)
**Source of truth:** `internal/rag/treesitter/languages.go` (grammars),
`internal/rag/treesitter/ast_walker.go` (`RegisterExtractor` — symbol extractors),
`internal/rag/treesitter/edges.go` + `*_edges.go` (`EdgeExtractor` — graph edges).

Binding: `github.com/madeindigio/go-tree-sitter` v0.2.0 (fork of smacker/go-tree-sitter,
local clone `/www/MCP/Remembrances/go-tree-sitter`). Vendored C runtime is tree-sitter
0.25.10: it loads grammars with ABI 13, 14 and 15. New grammars can be generated with
tree-sitter CLI 0.25.x (not 0.26+). `(*Language).ABIVersion()`, `Parser.TrySetLanguage`
report incompatible grammars.

Three capability tiers, from broadest to narrowest:

1. **Parseable (tree-sitter grammar available)** — a grammar is registered, so the
   file can be parsed. If no dedicated symbol extractor exists, a `GenericExtractor`
   fallback still pulls common declaration nodes.
2. **Symbol extraction (dedicated extractor)** — a language-specific
   `SymbolExtractor` emits structured symbols used by `code_find_symbol`,
   `code_get_symbols_overview`, `code_hybrid_search`, and the `view` read-modes.
3. **Property-graph edges (`EdgeExtractor`)** — emits `imports`/`calls`/`type_ref`
   edges into `code_edges`, powering `code_impact_analysis` and `code_related_files`.

## Capability matrix

| Language    | Extensions (sample)         | Grammar (fork v0.2.0)          | Symbol extractor | Graph edges |
|-------------|-----------------------------|--------------------------------|:----------------:|:-----------:|
| Go          | go                          | golang                         | ✅ | imports, calls |
| TypeScript  | ts, mts, cts                | typescript                     | ✅ | imports, calls |
| JavaScript  | js, mjs, cjs, jsx           | javascript                     | ✅ | imports, calls |
| TSX         | tsx                         | tsx                            | ⚠️ generic | ❌ |
| PHP         | php, phtml                  | php                            | ✅ | ❌ |
| Python      | py, pyw, pyi                | python                         | ✅ | ❌ |
| Rust        | rs                          | rust                           | ✅ | imports, calls, type_ref |
| Java        | java                        | java                           | ✅ | imports, calls, type_ref |
| Kotlin      | kt, kts                     | kotlin main 1852ea17           | ✅ | imports, calls, type_ref |
| Swift       | swift                       | swift 0.7.4                    | ✅ | imports, calls, type_ref |
| C           | c, h (by content)           | c                              | ✅ C | ❌ |
| C++         | cpp, cc, hpp, h (by content)| cpp v0.23.4                    | ✅ C++ | imports, calls, type_ref |
| Objective-C | m, mm, h (by content)       | objc v3.0.2 (real grammar)     | ✅ ObjC | imports, calls, type_ref |
| Ruby        | rb, rake, gemspec           | ruby v0.23.1                   | ✅ Ruby | imports, calls, type_ref |
| C#          | cs                          | csharp v0.23.5 (ABI 15)        | ✅ C# | imports, calls, type_ref |
| Scala       | scala, sc                   | scala                          | ⚠️ generic | ❌ |
| Lua         | lua                         | lua                            | ✅ | ❌ |
| Svelte      | svelte                      | svelte                         | ✅ | ❌ |
| Vue         | vue                         | vue2                           | ✅ | ❌ |
| Markdown    | md, markdown                | markdown                       | ✅ | ❌ |
| TOML        | toml                        | toml                           | ✅ | ❌ |
| YAML/HTML/CSS/Bash | yml, html, css, sh   | yaml/html/css/bash             | ⚠️ generic | ❌ |

## `.h` header detection

`DetectLanguage(path)` maps `.h` to C. Where content is available
(`ParseFile`, `internal/rag/code/indexer.go`, `readmode.ParseSymbols`),
`DetectLanguageForContent` / `DetectHeaderLanguage` (`parser.go`) classifies it:
ObjC markers (`@interface`, `@protocol`, `@end`, `#import`, `NS_ENUM`, ...) win, then C++
markers (namespace, template, class, `std::`, `virtual`, ...), else C. The indexer's
directory filter accepts `.h` when any of C/C++/ObjC is in the language filter.

## Per-language notes (grammar gotchas)

- **Swift (0.7.4)**: struct, enum, class, actor and extension are all `class_declaration`,
  told apart by the anonymous `declaration_kind` field (extension `name:` is a `user_type`).
  Protocols are `protocol_declaration`. Enum cases are `enum_entry` (one `name:` per case).
  Extension members are named under the extended type path (`/Square/double`) with
  `extension_of` (+ `conforms_to`) metadata and no duplicate type symbol. `init`,
  `deinit`, `subscript`, protocol members are extracted. One-line members without `;`
  parse as ERROR — use multi-line fixtures.
- **Kotlin**: grammar has no fields (positional children). interface/enum class are
  `class_declaration` (anonymous `interface` token / `enum_class_body`). `companion_object`
  is extracted (name `Companion` when unnamed); `const` is a `property_modifier`.
  type_ref = `type_identifier` under `user_type` only.
- **Rust**: imports from `use_declaration`, `extern_crate_declaration`, bodyless `mod_item`;
  macros are calls without `!`.
- **Java**: `new Foo()` emits both a call and a type_ref edge.
- **C#**: namespace segments go into the name path; file-scoped namespaces apply to
  following siblings; contiguous `///` comments joined as docstring. type_ref only for
  identifiers in type position; `var` excluded.
- **C++**: out-of-line `Foo::bar` definitions are attributed to `Foo`; templates unwrapped;
  C stays on `CExtractor`.
- **ObjC**: class name is the first `identifier` child (no field); methods named by full
  selector (`initWithFoo:bar:`; `init*` = constructor); categories named `Foo(Extras)`;
  C-level declarations delegated to `CExtractor`. `typedef NS_ENUM(...)` parses as ERROR.
- **Ruby**: no import node — `require`/`include`/`attr_*` are `call` nodes;
  `class << self` flattened into `self.x` methods.

type_ref edges for struct/field-level references outside functions have an empty
`SrcSymbolID` (`enclosingSymbolID` only matches functions/methods/constructors).

## How to add edge support for another language

1. Implement `EdgeExtractor` on the language's extractor in `<lang>_edges.go`, reusing
   `enclosingSymbolID` and `isDeclarationName`.
2. Add the language to `TestEdgeCapableLanguages` in `edges_test.go`.
Nothing else changes — `ASTWalker.ExtractEdges` auto-discovers the interface.

Remaining candidates: Python, PHP, C, Lua.

Related: [[plan_leanctx_context_intelligence]], [[pando/changes/tree-sitter-v0.2.0-language-coverage.md]],
[[pando/plans/tree-sitter-v15-runtime-and-language-coverage.md]].
