# RAG Usage Guide — Pando internal/rag

## Quick Start

```go
import (
    "context"
    "github.com/digiogithub/pando/internal/db"
    "github.com/digiogithub/pando/internal/rag"
)

func main() {
    ctx := context.Background()

    sqlDB, err := db.Connect()
    if err != nil { panic(err) }
    defer sqlDB.Close()

    store := rag.New(sqlDB, rag.StoreOptions{
        EmbeddingDim: 1536, // OpenAI text-embedding-ada-002 / text-embedding-3-small
    })
    if err := store.Init(ctx); err != nil { panic(err) }

    // Insert a chunk with embedding
    id, err := store.InsertChunk(ctx, rag.Chunk{
        Collection: "myproject",
        Source:     "README.md",
        Content:    "Pando is an AI-powered terminal assistant...",
        ChunkIndex: 0,
        Metadata:   `{"lang":"en","tokens":42}`,
    }, myEmbedding) // []float32 of length 1536

    // Insert without embedding (FTS only)
    id2, err := store.InsertChunk(ctx, rag.Chunk{
        Collection: "notes",
        Source:     "todo.txt",
        Content:    "Fix the bug in the parser",
    }, nil)
}
```

## Typical RAG Ingestion Flow

```go
// 1. Chunk the document
chunks := chunkDocument(content, 512) // tokens per chunk

// 2. Get embeddings from LLM provider (batch)
embeddings := provider.EmbedBatch(ctx, chunks)

// 3. Store all chunks
for i, text := range chunks {
    _, err := store.InsertChunk(ctx, rag.Chunk{
        Collection: sessionID,
        Source:     filePath,
        Content:    text,
        ChunkIndex: i,
        Metadata:   `{}`,
    }, embeddings[i])
}
```

## Search Examples

### Vector Search (semantic similarity)

```go
queryEmb := provider.Embed(ctx, "how does authentication work?")
results, err := store.SearchVector(ctx, queryEmb, rag.SearchOptions{
    Collection: "myproject",
    TopK:       5,
    MinScore:   0.7, // only return results with cosine similarity >= 0.7
})
for _, r := range results {
    fmt.Printf("[%.3f] %s (chunk %d)\n%s\n\n", r.Score, r.Source, r.ChunkIndex, r.Content)
}
```

### Full-Text Search (keyword/BM25)

```go
results, err := store.SearchFTS(ctx, "authentication JWT token", rag.SearchOptions{
    Collection: "myproject",
    TopK:       10,
})
// FTS5 query syntax:
//   "exact phrase"
//   word1 AND word2
//   word1 OR word2
//   NOT word
//   word*  (prefix)
//   content:word  (column filter)
```

### Hybrid Search (best of both)

```go
queryEmb := provider.Embed(ctx, "authentication")
results, err := store.SearchHybrid(ctx, "authentication JWT", queryEmb, rag.SearchOptions{
    Collection: "myproject",
    TopK:       5,
})
// Internally: SearchVector(TopK*3) + SearchFTS(TopK*3) → RRF fusion → top TopK
// Both run concurrently using the connection pool.
```

### Search across all collections

```go
results, err := store.SearchHybrid(ctx, query, emb, rag.SearchOptions{
    // Collection: ""  ← empty = all collections
    TopK: 10,
})
```

## Collection Management

```go
// List chunks in a collection (paginated)
chunks, err := store.ListChunks(ctx, "myproject", 50, 0) // limit=50, offset=0

// Count chunks
n, err := store.CountChunks(ctx, "myproject")
n2, err := store.CountChunks(ctx, "") // all collections

// Delete entire collection (cascades FTS)
err = store.DeleteCollection(ctx, "myproject")

// Delete single chunk
err = store.DeleteChunk(ctx, chunkID)

// Rebuild FTS index (after bulk operations or corruption)
err = store.RebuildFTS(ctx)
```

## Embedding Providers for Pando

The RAG package is provider-agnostic — any `[]float32` embedding works.
Common dimension values:

| Provider | Model | Dimension |
|---|---|---|
| OpenAI | text-embedding-ada-002 | 1536 |
| OpenAI | text-embedding-3-small | 1536 (default) / 512 |
| OpenAI | text-embedding-3-large | 3072 |
| Ollama | nomic-embed-text | 768 |
| Ollama | mxbai-embed-large | 1024 |
| Ollama | all-minilm | 384 |
| Anthropic | (no embedding API) | — |

**Important**: The dimension is stored in `rag_meta` on first `Init()`. To change it, you must
reset the `rag_meta` entry and recreate the store with the new dimension.

## Score Interpretation

| Search Type | Score Meaning | Range | Higher = Better |
|---|---|---|---|
| `SearchVector` | Cosine similarity | [0, 1] | ✅ |
| `SearchFTS` | Normalised BM25 | [0, 1] | ✅ |
| `SearchHybrid` | RRF score = Σ 1/(60+rank) | ~(0, 0.033] | ✅ |
| `SearchResult.Distance` | Cosine distance = 1 - similarity | [0, 2] | ❌ lower |

## Chunk Metadata Best Practices

Store metadata as JSON string for maximum flexibility:
```go
meta, _ := json.Marshal(map[string]any{
    "lang":     "go",
    "file":     "internal/rag/store.go",
    "tokens":   128,
    "section":  "Init",
    "repo":     "pando",
    "revision": "04773aa",
})
chunk.Metadata = string(meta)
```

Parse it back in search results:
```go
var meta map[string]any
json.Unmarshal([]byte(result.Metadata), &meta)
```

## Performance Notes

- `SearchVector`: O(n) — loads all embeddings in the collection. Adequate for ≤100k chunks.
  For larger datasets, add pagination or use an external HNSW library.
- `SearchFTS`: O(log n) via FTS5 inverted index — very fast for any size.
- `SearchHybrid`: runs both concurrently, dominated by `SearchVector` for large collections.
- Batch inserts: use a transaction and insert multiple chunks before committing.
  `RebuildFTS` after bulk insert is more efficient than per-row FTS sync.
