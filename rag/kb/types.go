// Package kb provides a knowledge base system for storing and searching documents.
//
// Documents are chunked and embedded for hybrid search combining vector similarity
// and full-text search using Reciprocal Rank Fusion (RRF).
package kb

import "time"

// Document represents a stored document in the knowledge base.
type Document struct {
	ID       int64
	FilePath string
	// Content is the full document body. It is populated by GetDocument and by
	// direct row-level queries such as the pinned-memory path in
	// GetMemoriesForInjection, but it is NOT populated when a Document arrives
	// embedded in a SearchResult from SearchDocuments/SearchDocumentsWithOptions
	// (searchVector/searchFTS never select it — PANDO-US-0027, kb.go). Read
	// SearchResult.ChunkContent for the matched excerpt instead; a caller that
	// genuinely needs the full body for its top-k hits backfills it with one
	// keyed query (see documentContentByID in kb.go), never by re-adding the
	// column to the scan.
	Content   string
	Metadata  map[string]interface{}
	Tags      []string
	CreatedAt time.Time
	UpdatedAt time.Time
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
	// PathPrefix restricts both search legs to documents whose file_path
	// starts with this prefix, pushed into the SQL as a bound
	// `AND d.file_path LIKE ? || '%' ESCAPE '\'` clause (PANDO-US-0028) rather
	// than filtered in Go after fusion — a prefix filtered after the fact would
	// under-return whenever the unfiltered top candidates all belong to another
	// prefix. Empty (the default) changes nothing about current behaviour.
	PathPrefix string
}

// SearchResult represents a ranked search result from the knowledge base.
//
// Document.Content is unpopulated: see the field's doc comment on Document.
// Use ChunkContent for the matched text.
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
	// LinksIndexed counts the [[wiki links]] found in the documents this run
	// added or updated. Unchanged documents keep the links they already had and
	// are not counted, so this is what the run wrote, not the size of the graph.
	LinksIndexed int `json:"links_indexed"`
}

// RepairStats reports the outcome of RepairFrontMatterMetadata: how many
// filesystem-backed documents it looked at, and how many of those had their
// metadata rebuilt because it was missing front-matter-derived keys their
// source file still declares.
type RepairStats struct {
	Scanned  int `json:"scanned"`
	Repaired int `json:"repaired"`
}
