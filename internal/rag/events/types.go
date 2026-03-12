// Package events provides temporal event storage with semantic search capabilities.
package events

import "time"

// Event represents a temporal event with optional semantic search.
type Event struct {
	// ID is the database row ID (set after insert, zero for new events).
	ID int64
	// Subject categorizes the event (e.g. user ID, session ID, topic).
	Subject string
	// Content is the event text content.
	Content string
	// Metadata is an arbitrary JSON string stored alongside the event.
	Metadata map[string]interface{}
	// EventAt is when the event occurred (defaults to creation time).
	EventAt time.Time
	// CreatedAt is when the event was stored in the database.
	CreatedAt time.Time
}

// SearchOptions controls event search behaviour.
type SearchOptions struct {
	// Query is the search text (used for both vector and FTS search).
	Query string
	// Subject restricts results to events with this subject (empty means all).
	Subject string
	// FromDate filters events that occurred on or after this time (inclusive).
	FromDate *time.Time
	// ToDate filters events that occurred on or before this time (inclusive).
	ToDate *time.Time
	// LastHours filters events from the last N hours (overrides FromDate).
	LastHours int
	// LastDays filters events from the last N days (overrides FromDate).
	LastDays int
	// Limit is the maximum number of results to return (defaults to 10).
	Limit int
}

// SearchResult is a ranked event returned from search.
type SearchResult struct {
	Event Event
	// Score is a normalised relevance score in [0, 1] where higher is better.
	// For vector search:  cosine similarity.
	// For FTS search:     normalised BM25.
	// For hybrid search:  Reciprocal Rank Fusion score.
	Score float64
	// Rank is the 1-based position in the result list.
	Rank int
}

// defaultSearchOptions fills in zero values with sensible defaults.
func defaultSearchOptions(opts SearchOptions) SearchOptions {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	// Apply time filters based on LastHours / LastDays.
	if opts.LastHours > 0 {
		from := time.Now().Add(-time.Duration(opts.LastHours) * time.Hour)
		opts.FromDate = &from
	}
	if opts.LastDays > 0 {
		from := time.Now().AddDate(0, 0, -opts.LastDays)
		opts.FromDate = &from
	}
	return opts
}
