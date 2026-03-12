// Package kb provides a knowledge base system for storing and searching documents.
//
// Documents are chunked and embedded for hybrid search combining vector similarity
// and full-text search using Reciprocal Rank Fusion (RRF).
package kb

import "time"

// Document represents a stored document in the knowledge base.
type Document struct {
	ID        int64
	FilePath  string
	Content   string
	Metadata  map[string]interface{}
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SearchResult represents a ranked search result from the knowledge base.
type SearchResult struct {
	Document     Document
	ChunkContent string
	Score        float64
	Rank         int
}
