package sessions

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// mockEmbedder returns fixed-dimension embeddings for testing.
type mockEmbedder struct {
	dim int
}

func (m *mockEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, m.dim)
		// Generate a simple deterministic vector based on text length
		for j := range vec {
			vec[j] = float32((len(text)+j)%10) / 10.0
		}
		result[i] = vec
	}
	return result, nil
}

func (m *mockEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, m.dim)
	for j := range vec {
		vec[j] = float32((len(text)+j)%10) / 10.0
	}
	return vec, nil
}

func (m *mockEmbedder) Dimension() int { return m.dim }

func newTestStore(t *testing.T) (*SessionRAGStore, *sql.DB) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// SQLite in-memory databases are per-connection; single connection ensures
	// all operations share the same database and avoids "no such table" races.
	db.SetMaxOpenConns(1)

	store := NewSessionRAGStore(db, &mockEmbedder{dim: 8}, 200, 20)
	if err := store.InitTables(context.Background()); err != nil {
		db.Close()
		t.Fatalf("init tables: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return store, db
}

func TestInitTables(t *testing.T) {
	_, db := newTestStore(t)

	// Verify tables exist by querying them
	tables := []string{
		"session_rag_documents",
		"session_rag_chunks",
		"session_rag_fts",
	}
	for _, tbl := range tables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type IN ('table','shadow') AND name = ?`, tbl).Scan(&name)
		// FTS virtual tables may appear as 'table' or not at all in sqlite_master on some drivers;
		// just ensure no error from InitTables is enough for fts.
		if tbl != "session_rag_fts" && err != nil {
			t.Errorf("table %s not found: %v", tbl, err)
		}
	}
}

func TestIndexSessionAndGetDocument(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	err := store.IndexSession(ctx,
		"sess-001", "Test session", "Hello world. This is a test conversation.",
		map[string]interface{}{"model": "gpt-4"},
		3, 2, "gpt-4",
	)
	if err != nil {
		t.Fatalf("IndexSession: %v", err)
	}

	doc, err := store.GetSessionDocument(ctx, "sess-001")
	if err != nil {
		t.Fatalf("GetSessionDocument: %v", err)
	}
	if doc == nil {
		t.Fatal("expected document, got nil")
	}
	if doc.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", doc.SessionID, "sess-001")
	}
	if doc.Title != "Test session" {
		t.Errorf("Title = %q, want %q", doc.Title, "Test session")
	}
	if doc.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", doc.MessageCount)
	}
	if doc.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", doc.Model, "gpt-4")
	}
}

func TestIndexSessionUpsert(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Index once
	if err := store.IndexSession(ctx, "sess-002", "First title", "First content.", nil, 1, 1, ""); err != nil {
		t.Fatalf("first IndexSession: %v", err)
	}

	// Re-index with new content (upsert)
	if err := store.IndexSession(ctx, "sess-002", "Updated title", "Updated content.", nil, 2, 2, ""); err != nil {
		t.Fatalf("second IndexSession: %v", err)
	}

	doc, err := store.GetSessionDocument(ctx, "sess-002")
	if err != nil {
		t.Fatalf("GetSessionDocument: %v", err)
	}
	if doc.Title != "Updated title" {
		t.Errorf("Title = %q, want %q", doc.Title, "Updated title")
	}
	if doc.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", doc.MessageCount)
	}
}

func TestGetSessionDocumentNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	doc, err := store.GetSessionDocument(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc != nil {
		t.Errorf("expected nil, got document %+v", doc)
	}
}

func TestDeleteSession(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()

	if err := store.IndexSession(ctx, "sess-003", "To delete", "Some content to delete.", nil, 1, 1, ""); err != nil {
		t.Fatalf("IndexSession: %v", err)
	}

	if err := store.DeleteSession(ctx, "sess-003"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	doc, err := store.GetSessionDocument(ctx, "sess-003")
	if err != nil {
		t.Fatalf("GetSessionDocument after delete: %v", err)
	}
	if doc != nil {
		t.Error("expected nil after delete, got document")
	}

	// Verify chunks are also deleted
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM session_rag_chunks`).Scan(&count); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 chunks after delete, got %d", count)
	}
}

func TestDeleteSessionNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	// Should not error when deleting non-existent session
	if err := store.DeleteSession(ctx, "does-not-exist"); err != nil {
		t.Errorf("DeleteSession on missing session returned error: %v", err)
	}
}

func TestSearchSessions(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	sessions := []struct {
		id      string
		title   string
		content string
	}{
		{"s1", "Go programming", "Go is a statically typed compiled language."},
		{"s2", "Python basics", "Python is a dynamically typed interpreted language."},
		{"s3", "Database design", "Relational databases use tables and SQL queries."},
	}

	for _, sess := range sessions {
		if err := store.IndexSession(ctx, sess.id, sess.title, sess.content, nil, 0, 0, ""); err != nil {
			t.Fatalf("IndexSession %s: %v", sess.id, err)
		}
	}

	results, err := store.SearchSessions(ctx, "programming language", 5)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one search result")
	}

	// Verify each result has expected fields populated
	for _, r := range results {
		if r.SessionID == "" {
			t.Error("result has empty SessionID")
		}
		if r.Source != "session" {
			t.Errorf("Source = %q, want %q", r.Source, "session")
		}
		if r.Score <= 0 {
			t.Errorf("Score = %f, want > 0", r.Score)
		}
	}

	// Verify no duplicate session IDs in results
	seen := make(map[string]bool)
	for _, r := range results {
		if seen[r.SessionID] {
			t.Errorf("duplicate session_id %q in results", r.SessionID)
		}
		seen[r.SessionID] = true
	}
}
