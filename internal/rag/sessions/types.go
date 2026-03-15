// Package sessions provides RAG-based storage for conversation sessions.
// Sessions are chunked, embedded, and stored in SQLite with FTS5 for hybrid search.
package sessions

import "time"

// SessionDocument represents a stored session in the RAG store.
type SessionDocument struct {
	ID           int64
	SessionID    string
	Title        string
	Content      string
	Metadata     map[string]interface{}
	MessageCount int
	TurnCount    int
	Model        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SessionChunk represents a chunk of a session document with its embedding.
type SessionChunk struct {
	ID         int64
	DocumentID int64
	ChunkIndex int
	Content    string
	Embedding  []float32
	Role       string // "user", "assistant", "mixed"
	TurnStart  int
	TurnEnd    int
	CreatedAt  time.Time
}

// SessionSearchResult represents a ranked search result from the session store.
type SessionSearchResult struct {
	SessionID  string
	Title      string
	Content    string
	Role       string
	Similarity float64
	Score      float64
	Source     string // always "session"
	TurnStart  int
	TurnEnd    int
}
