// Package rag provides RAG (Retrieval-Augmented Generation) storage and search
// on top of the existing Pando SQLite database.
//
// It supports:
//   - Chunked document storage with JSON metadata
//   - Vector similarity search (top-k ANN) via sqlite-vec / vec0 virtual tables
//   - Full-text search (BM25) via SQLite FTS5
//   - Hybrid search combining both with Reciprocal Rank Fusion
//
// Prerequisites:
//   - The sqlite-vec extension must be loaded before any Store is used. This is
//     handled automatically by internal/db.Connect(), which calls sqlite_vec.Auto().
//   - Call Store.Init after obtaining a *sql.DB to create the vec0 virtual table.
package rag

import "time"

// DefaultEmbeddingDim is the default vector dimension (OpenAI text-embedding-ada-002 / -3-small).
const DefaultEmbeddingDim = 1536

// Chunk is a unit of text stored in the RAG system.
type Chunk struct {
	// ID is the database row ID (set after insert, zero for new chunks).
	ID int64
	// Collection groups related chunks together (e.g. project path, session ID).
	Collection string
	// Source identifies the origin document (file path, URL, …).
	Source string
	// Content is the raw text of this chunk.
	Content string
	// ChunkIndex is the zero-based position of this chunk inside its source document.
	ChunkIndex int
	// Metadata is an arbitrary JSON string stored alongside the chunk.
	Metadata string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SearchResult is a ranked entry returned from vector, FTS, or hybrid search.
type SearchResult struct {
	Chunk
	// Distance is the L2 (or cosine) distance from the query vector (lower = more similar).
	// Populated only by SearchVector and SearchHybrid.
	Distance float64
	// Score is a normalised relevance score in [0, 1] where higher is better.
	// For vector search:  1 / (1 + Distance).
	// For FTS search:     normalised BM25 (−bm25 / max(−bm25) across the result set).
	// For hybrid search:  Reciprocal Rank Fusion score.
	Score float64
	// Rank is the 1-based position in the result list.
	Rank int
}

// SearchOptions controls search behaviour.
type SearchOptions struct {
	// Collection restricts results to a single collection. Empty means all collections.
	Collection string
	// TopK is the maximum number of results to return. Defaults to 5.
	TopK int
	// MinScore excludes results with Score below this value (0 means no filter).
	MinScore float64
}

// StoreOptions configures a new Store.
type StoreOptions struct {
	// EmbeddingDim is the dimensionality of the embedding vectors.
	// Must match the dimension used when the vec0 table was first created.
	// Defaults to DefaultEmbeddingDim (1536).
	EmbeddingDim int
}

// defaultSearchOptions fills in zero values with sensible defaults.
func defaultSearchOptions(opts SearchOptions) SearchOptions {
	if opts.TopK <= 0 {
		opts.TopK = 5
	}
	return opts
}
