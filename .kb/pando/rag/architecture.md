# RAG Architecture in Pando (SQLite-based, no CGO)

## Overview

Pando implements RAG (Retrieval-Augmented Generation) storage and search using the existing SQLite database (`pando.db`). The implementation lives in `internal/rag/` and uses the same `*sql.DB` connection from `internal/db.Connect()`.

## Key Design Decisions

### No SQLite Extension Required
- **FTS5** is compiled into the ncruces/go-sqlite3 WASM binary by default — no extension needed.
- **Vector search** is performed in **pure Go** (cosine similarity) — no sqlite-vec extension needed.
- Embeddings are stored as little-endian float32 BLOBs directly in `rag_chunks.embedding`.

### Why NOT sqlite-vec
`github.com/asg017/sqlite-vec-go-bindings` was evaluated and rejected:
- All versions (up to v0.1.7-alpha.2) were compiled against `ncruces/go-sqlite3 v0.17.1`
- Pando uses `ncruces/go-sqlite3 v0.25.0` — the host module `env` API changed between versions
- Specific error: `"go_final" is not exported in module "env"` (v0.25.0 removed/renamed it)
- A second incompatibility: the sqlite-vec WASM requires WebAssembly atomics (`i32.atomic.store`) which needs wazero configured with `experimental.CoreFeaturesThreads`
- **Future**: if a version of sqlite-vec-go-bindings compatible with ncruces v0.25.x appears, the embedding BLOB format is identical (little-endian float32), so migration would be trivial — just create the `rag_vec` virtual table and add vec0 rowids.

## Database Driver Stack

```
internal/db/connect.go
  ├── _ "github.com/ncruces/go-sqlite3/driver"   (database/sql driver)
  └── _ "github.com/ncruces/go-sqlite3/embed"    (WASM binary: SQLite 3.49.1)
                                                   Includes: FTS5, JSON, R*Tree,
                                                   GeoPoly, Spellfix1, regexp, ...
                                                   Does NOT include: sqlite-vec
```

## Schema (migration 20260309000001_add_rag.sql)

```sql
-- Main document chunk store
CREATE TABLE rag_chunks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    collection  TEXT    NOT NULL DEFAULT 'default',
    source      TEXT    NOT NULL DEFAULT '',
    content     TEXT    NOT NULL,
    chunk_index INTEGER NOT NULL DEFAULT 0,
    metadata    TEXT    NOT NULL DEFAULT '{}',
    embedding   BLOB,          -- little-endian float32 array, NULL = no embedding
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);

-- FTS5 external-content table (no text duplication)
CREATE VIRTUAL TABLE rag_fts USING fts5(
    content, source, collection,
    content='rag_chunks', content_rowid='id'
);

-- Per-store metadata (embedding dimension, etc.)
CREATE TABLE rag_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
```

`rag_vec` (sqlite-vec virtual table) is NOT in the migration. Reserved for future use if compatible bindings become available.

## Package API (`internal/rag/`)

```go
// Create a store (call after db.Connect())
store := rag.New(db, rag.StoreOptions{EmbeddingDim: 1536})
err   := store.Init(ctx)  // creates rag_meta entry, validates dimension

// CRUD
id, err := store.InsertChunk(ctx, chunk, embedding)  // embedding can be nil
err      = store.UpdateChunk(ctx, id, chunk, embedding)
err      = store.DeleteChunk(ctx, id)
chunk, err = store.GetChunk(ctx, id)
emb, err   = store.GetChunkEmbedding(ctx, id)
chunks, err = store.ListChunks(ctx, collection, limit, offset)
err      = store.DeleteCollection(ctx, collection)
count, err = store.CountChunks(ctx, collection)
err      = store.RebuildFTS(ctx)  // recover FTS index from content table

// Search
results, err := store.SearchVector(ctx, embedding, opts)  // cosine similarity, Go pure
results, err := store.SearchFTS(ctx, query, opts)         // BM25 via FTS5
results, err := store.SearchHybrid(ctx, query, emb, opts) // RRF fusion, parallel
```

## SearchOptions

```go
type SearchOptions struct {
    Collection string   // filter by collection, empty = all
    TopK       int      // max results, default 5
    MinScore   float64  // minimum score threshold, 0 = no filter
}
```

## SearchResult

```go
type SearchResult struct {
    Chunk               // embedded: ID, Collection, Source, Content, ...
    Distance float64    // cosine distance (1 - similarity), vector search only
    Score    float64    // [0,1] relevance: cosine similarity / normalised BM25 / RRF score
    Rank     int        // 1-based position
}
```

## Vector Search Details (pure Go)

- **Algorithm**: exact brute-force cosine similarity
- **Suitable for**: up to ~100k chunks per collection (typical RAG for dev tools)
- **Scalability**: O(n) per query; for >100k chunks, consider batching or an HNSW library
- **Collection filter**: applied in SQL WHERE clause before loading embeddings
- **Over-fetch in hybrid**: SearchHybrid uses `TopK * 3` per sub-search before RRF fusion

## FTS5 Details

- **Table type**: external-content (`content='rag_chunks'`) — no text duplication
- **Indexed columns**: `content`, `source`, `collection`
- **Ranking**: BM25 via `bm25()` function (negative value, negate for positive score)
- **Sync**: manual — application must call INSERT/DELETE on `rag_fts` on every chunk change
- **FTS5 query syntax**: plain terms, `"phrase"`, `term*`, `col:term`, AND/OR/NOT

## Hybrid Search (RRF)

Uses Reciprocal Rank Fusion with k=60 (standard):
```
rrf_score(d) = Σ  1 / (60 + rank(d, list))
```
Both sub-searches run in parallel goroutines sharing the connection pool. Results are merged, sorted by RRF score, and limited to TopK.

## Embedding Format Compatibility

The BLOB format (little-endian raw float32) is compatible with sqlite-vec's expected format:
```go
// Serialize:  binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v[i]))
// Deserialize: math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
```
If sqlite-vec becomes available in a compatible version, existing BLOBs can be migrated to a `rag_vec` virtual table without re-embedding.
