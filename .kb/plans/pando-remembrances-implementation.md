# Pando Remembrances Implementation Plan

## Overview

Add remembrances capabilities (Knowledge Base, Events, Code Indexing) to Pando, inspired by remembrances-mcp but using Pando's existing SQLite + RAG infrastructure. Key differences from remembrances-mcp:

1. **Storage:** SQLite (pando's existing DB) instead of SurrealDB
2. **Embeddings:** Provider-agnostic via pando's enabled LLM providers + Ollama (instead of local GGUF)
3. **Reduced function set:** Focused on KB, Events, and Code Indexing (no key-value facts, no knowledge graph)
4. **Dual API:** Both MCP tools (external) and internal function-call tools (for pando's agent)
5. **Configuration UI:** Embedding model selector in pando's TUI settings panel

## Architecture

```
┌─────────────────────────────────────────────────┐
│                  Pando App                       │
├──────────┬──────────┬──────────┬────────────────┤
│ MCP API  │ LLM Tools│ Config UI│  CLI Commands  │
├──────────┴──────────┴──────────┴────────────────┤
│              Service Layer                       │
│  ┌──────────┐ ┌──────────┐ ┌──────────────────┐ │
│  │ KB Store │ │ Events   │ │ Code Indexer     │ │
│  │          │ │ Store    │ │ (tree-sitter)    │ │
│  └────┬─────┘ └────┬─────┘ └───────┬──────────┘ │
├───────┴────────────┴───────────────┴────────────┤
│           Embeddings Provider Layer              │
│  ┌────────┐ ┌───────┐ ┌──────┐ ┌──────────────┐│
│  │ OpenAI │ │Google │ │Ollama│ │Anthropic/etc ││
│  └────────┘ └───────┘ └──────┘ └──────────────┘│
├─────────────────────────────────────────────────┤
│              SQLite (existing DB)                │
│  rag_chunks │ kb_documents │ kb_chunks          │
│  events     │ code_projects│ code_files         │
│  code_symbols│ code_chunks │ events_fts│ kb_fts │
└─────────────────────────────────────────────────┘
```

## Phase 1: Embeddings Provider Abstraction Layer

**Priority:** Critical (blocks all other phases)
**Effort:** Medium

Create `internal/rag/embeddings/` package:
- `Embedder` interface: EmbedDocuments, EmbedQuery, Dimension
- Provider implementations: OpenAI, Google/Gemini, Ollama, Anthropic (Voyage)
- Factory pattern for creating embedders from config
- Text chunking utilities (sentence-aware splitting)
- Reuse existing provider API keys from pando's config

Config additions:
```toml
[Remembrances]
Enabled = true
DocumentEmbeddingProvider = "ollama"
DocumentEmbeddingModel = "nomic-embed-text"
CodeEmbeddingProvider = "ollama"
CodeEmbeddingModel = "nomic-embed-text"
```

## Phase 2: Knowledge Base (KB) System

**Priority:** High
**Effort:** Medium
**Depends on:** Phase 1

Create `internal/rag/kb/` package:
- SQLite tables: kb_documents, kb_chunks, kb_fts
- KBStore with CRUD: AddDocument, GetDocument, DeleteDocument, SearchDocuments
- Automatic chunking with configurable size/overlap
- Hybrid search (vector + FTS5) using existing RRF from rag/search.go
- Optional directory watcher for auto-indexing markdown files

## Phase 3: Events System

**Priority:** High
**Effort:** Low-Medium
**Depends on:** Phase 1

Create `internal/rag/events/` package:
- SQLite tables: events, events_fts
- EventStore: SaveEvent, SearchEvents, ListEvents, DeleteEvent
- Time-based filtering (from_date, to_date, last_hours, last_days)
- Subject categorization
- Hybrid search with time weighting

## Phase 4: Tree-sitter Integration and Code Indexing

**Priority:** High
**Effort:** High (largest phase)
**Depends on:** Phase 1

Port from remembrances-mcp:
- `pkg/treesitter/` - Full parser package with 25+ language support
- 15 language-specific extractors (Go, TS, JS, Python, Java, Rust, C, C++, PHP, Kotlin, Swift, Ruby, Lua, Svelte, Vue, Markdown, TOML)
- AST walker and symbol extraction framework

Create `internal/rag/code/` package:
- SQLite tables: code_projects, code_files, code_symbols, code_chunks
- CodeIndexer: IndexProject, ReindexFile, HybridSearch, FindSymbol
- Concurrent file processing (worker pool, 4 workers default)
- Hash-based change detection
- Large symbol chunking (800 byte threshold)
- Uses code-specific embedding provider

Dependencies to add to go.mod:
- github.com/smacker/go-tree-sitter
- Language grammar packages for all 25+ languages

## Phase 5: MCP Tools and Function Call API

**Priority:** Medium-High
**Effort:** Medium
**Depends on:** Phases 2, 3, 4

A) MCP Tools (extend mesnada server):
- KB: kb_add_document, kb_search_documents, kb_get_document, kb_delete_document
- Events: save_event, search_events
- Code: code_index_project, code_hybrid_search, code_find_symbol, code_get_symbols_overview, code_get_project_stats, code_reindex_file, code_list_projects

B) Internal LLM Tools (for pando's coder agent):
- `internal/llm/tools/remembrances_kb.go`
- `internal/llm/tools/remembrances_events.go`
- `internal/llm/tools/remembrances_code.go`

Both APIs call the same service layer.

## Phase 6: Configuration UI for Embedding Model Selection

**Priority:** Medium
**Effort:** Medium
**Depends on:** Phase 1

TUI settings panel additions:
- Remembrances enable/disable toggle
- Document embedding provider + model selector
- Code embedding provider + model selector
- "Use same model for both" checkbox
- Model suggestions per provider
- Connection test on save
- Dimension display after test
- Re-embedding warning on model change

## Supported Languages (Tree-sitter)

### Primary (19 languages):
Go, TypeScript, JavaScript, PHP, Lua, Markdown, Svelte, TOML, Vue, Rust, Java, Kotlin, Swift, Objective-C, C, C++, Python, Ruby, C#

### Additional (6 languages):
TSX, Scala, Bash, YAML, HTML, CSS

### Symbol Types Extracted:
class, struct, interface, trait, method, function, constructor, property, field, variable, constant, enum, enum_member, type_alias, namespace, module, package

## Implementation Order

1. **Phase 1** → Foundation (embeddings)
2. **Phase 2 + Phase 3** → Can be done in parallel (KB + Events)
3. **Phase 4** → Code indexing (largest effort)
4. **Phase 5** → API exposure (depends on 2+3+4)
5. **Phase 6** → UI (can start after Phase 1, finish after Phase 5)

## Key Technical Decisions

1. **Pure Go SQLite** (ncruces/go-sqlite3) - no C compilation needed
2. **Vector search in Go** - cosine similarity computed in-memory (suitable for ~100k chunks)
3. **FTS5** for text search - built into SQLite
4. **RRF hybrid search** - combines vector + FTS results
5. **Tree-sitter via go-tree-sitter** - mature Go bindings
6. **Provider-agnostic embeddings** - any enabled provider or Ollama
7. **Separate embedding models** for docs vs code - configurable