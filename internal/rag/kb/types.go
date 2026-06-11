// Package kb provides a knowledge base system for storing and searching documents.
//
// Documents are chunked and embedded for hybrid search combining vector similarity
// and full-text search using Reciprocal Rank Fusion (RRF).
package kb

import "time"

// Document represents a stored document in the knowledge base.
type Document struct {
	ID         int64
	FilePath   string
	Content    string
	Metadata   map[string]interface{}
	Tags       []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	// Memory system fields (populated only when reading memory documents)
	MemoryKey   string
	MemoryScope string
	Importance  float64
	Hits        int
	Source      string
	Outdated    bool
	ExpiresAt   *time.Time
}

// SearchOptions provides optional filtering/sorting for search queries.
type SearchOptions struct {
	Tags            []string // filter results by tags (fuzzy match)
	SortByDate      bool     // sort results by updated_at descending
	ExcludeOutdated bool     // exclude documents where outdated = 1
	Scope           string   // filter by memory_scope prefix (empty = all)
}

// SearchResult represents a ranked search result from the knowledge base.
type SearchResult struct {
	Document     Document
	ChunkContent string
	Score        float64
	Rank         int
}

// SyncStats contains counters from a filesystem-to-KB synchronization run.
type SyncStats struct {
	Scanned   int `json:"scanned"`
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Deleted   int `json:"deleted"`
}
