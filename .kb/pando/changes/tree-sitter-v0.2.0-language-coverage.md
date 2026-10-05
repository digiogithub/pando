---
created_at: 2026-10-06T19:48:58.231364841Z
updated_at: 2026-10-06T19:48:58.231364841Z
tags:
    - change
    - treesitter
    - code-index
    - code-graph
---
# Change: go-tree-sitter v0.2.0 (ABI 15 runtime) + Pando language coverage (2026-10-06)

Plan: [[pando/plans/tree-sitter-v15-runtime-and-language-coverage.md]]. Reference updated: [[pando/reference/code_graph_language_support.md]].

## Fork github.com/madeindigio/go-tree-sitter (tag v0.2.0, pushed)
Local clone /www/MCP/Remembrances/go-tree-sitter. Commits b7e5438 + f494f83.
- Vendored C runtime 0.22.5 -> 0.25.10 via smacker's `_automation/treesitter_updater` (sitterVersion bump). Loads ABI 13–15: all existing ABI-14 grammars keep working, ABI-15 grammars load. New `portable_endian.h`, `ts_assert.h`.
- Go API backward compatible. Timeout/cancellation reimplemented with `ts_parser_parse_with_options` progress callback (deprecated set_timeout/cancellation_flag no longer used; cancel flag in C memory). Additions: `(*Language).ABIVersion()`, `(*Language).Name()`, `(*Parser).TrySetLanguage`, `ErrIncompatibleLanguage`, constants `LanguageVersion`/`MinCompatibleLanguageVersion`.
- Bug fix: `EditInput.c()` filled `new_end_point` from `OldEndPoint`.
- Test fixes: example names for go vet, leak tests measure heap growth (were flaky). New `bindings_abi_test.go`.
- Grammars: swift 0.5.0 -> 0.7.4; kotlin 0.3.8 -> main 1852ea17 (scanner NUL hang fix); csharp v0.21.3 -> v0.23.5 (ABI 15); ruby v0.21.0 -> v0.23.1; cpp v0.22.3 -> v0.23.4; NEW objc (tree-sitter-grammars/tree-sitter-objc v3.0.2).
- Gotchas: updater drops the `note` field of `blade` in grammars.json (restore by hand). Generate new grammars with CLI 0.25.x, not 0.26+. Two copies of a grammar symbol can't link in one binary.

## Pando
- go.mod: go-tree-sitter v0.1.0 -> v0.2.0.
- Swift extractor rewritten (`swift_extractor.go`): dispatch on `declaration_kind` (struct/enum/class/actor/extension were all indexed as Class and enum cases/extension metadata lost); init/deinit/subscript/protocol members; extension members under extended type path.
- Kotlin extractor fixes: interface and enum class detection, enum entries, companion objects, `const`, type aliases.
- New extractors: C# (`csharp_extractor.go`), C++ (`cpp_extractor.go`), Ruby (`ruby_extractor.go`), ObjC (`objc_extractor.go`, real objc grammar instead of C fallback).
- New edges (imports/calls/type_ref): `swift_edges.go`, `kotlin_edges.go`, `rust_edges.go`, `java_edges.go`, `csharp_edges.go`, `cpp_edges.go`, `ruby_edges.go`, `objc_edges.go`. First languages emitting `type_ref`.
- `.h` detection by content: `DetectLanguageForContent`/`DetectHeaderLanguage` in `parser.go`, used in `ParseFile`, `internal/rag/code/indexer.go`, `readmode/parse.go`; `.h` static mapping now C. `readmode.IsCodeLanguage` adds ObjC, Ruby, C#.
- Registration in `ast_walker.go`; `TestEdgeCapableLanguages` extended.
- Nil guards added after review in `java_edges.go`, `ruby_edges.go`.

## Verification
- Fork: go build/vet/test ./... (40 packages) pass; race on root passes.
- Pando: go build ./...; go test internal/rag/treesitter, internal/rag/code, internal/llm/tools/..., internal/llm/agent, internal/api pass; full `go test ./...` run.
- Pre-existing flaky test NOT caused by this work: `internal/rag` `TestBuildMemoryBlockWithResultAgainstRealStore` (fails ~2/6 on the base revision too).

## Environment note
`~/.go` (GOROOT from the `g` installer) is missing on this machine; used cached toolchain GOROOT=~/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.linux-amd64 with GOTOOLCHAIN=local.
