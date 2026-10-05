---
created_at: 2026-10-06T19:20:12.274914072Z
updated_at: 2026-10-06T19:20:12.274914072Z
tags:
    - plan
    - treesitter
    - code-index
---
# Plan: go-tree-sitter fork v15 runtime + Pando language coverage (2026-10-06)

Fork: /www/MCP/Remembrances/go-tree-sitter (github.com/madeindigio/go-tree-sitter, git, branch master, last tag v0.1.0). Pando consumes it at go.mod:45.
Go toolchain: ~/.go missing; use GOROOT=~/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.linux-amd64, GOTOOLCHAIN=local.

## Phase A — fork
- A1: upgrade vendored C runtime to tree-sitter core 0.25.x (ABI 15, min compatible 13) keeping the Go binding API stable; all existing ABI-14 grammars must load and tests pass. Fallback if infeasible: stay on ABI 14 and regenerate new grammars with `--abi 14`.
- A2: update swift grammar to alex-pinkus 0.7.x, kotlin to latest, review/update csharp, ruby, cpp; add objc grammar (tree-sitter-grammars/tree-sitter-objc).
- Tag v0.2.0 and push.

## Phase B — Pando
- B1: fix Swift extractor (all type decls are class_declaration + declaration_kind; add init/deinit/subscript/protocol members), Swift edges, tests.
- B2: edges for Kotlin, Java, Rust.
- B3: extractors for C#, Ruby, C++; ObjC on real grammar (.m/.mm; .h heuristics).
- Bump dependency to v0.2.0, update reference doc pando/reference/code_graph_language_support.md, commit, push, tag.

Related: [[pando/reference/code_graph_language_support.md]]
