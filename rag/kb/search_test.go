package kb

import (
	"context"
	"strings"
	"testing"
)

// TestSearchDocumentsDoesNotPopulateDocumentContent is a regression test for
// PANDO-US-0027: search must return the matched chunk excerpt (and every
// other document field: id, path, tags, metadata, score, rank) without ever
// materialising the full document body.
func TestSearchDocumentsDoesNotPopulateDocumentContent(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	body := strings.Repeat("filler sentence about golang programming. ", 50) +
		"unique-marker-xyz appears once here."
	if err := store.AddDocument(ctx, "docs/big.md", body, map[string]any{"tags": []string{"reference"}}); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	results, err := store.SearchDocumentsWithOptions(ctx, "unique-marker-xyz", 5, SearchOptions{})
	if err != nil {
		t.Fatalf("SearchDocumentsWithOptions() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result")
	}

	for _, r := range results {
		if r.Document.Content != "" {
			t.Fatalf("Document.Content = %q, want empty (search must not select d.content)", r.Document.Content)
		}
		if r.ChunkContent == "" {
			t.Fatal("ChunkContent is empty, want the matched chunk text")
		}
		if r.Document.FilePath != "docs/big.md" {
			t.Fatalf("FilePath = %q, want docs/big.md", r.Document.FilePath)
		}
		if r.Document.ID == 0 {
			t.Fatal("Document.ID is zero, want the real row id")
		}
	}
	if len(results[0].Document.Tags) != 1 || results[0].Document.Tags[0] != "reference" {
		t.Fatalf("Tags = %v, want [reference]", results[0].Document.Tags)
	}
	if !strings.Contains(results[0].ChunkContent, "unique-marker-xyz") {
		t.Fatalf("ChunkContent = %q, want it to contain the matched term", results[0].ChunkContent)
	}
}

// TestGetMemoriesForInjectionBackfillsContent is a regression test for the
// memory-recall path: SearchDocumentsWithOptions no longer loads
// Document.Content (PANDO-US-0027), but recall (formatMemoryLine, the recall
// tool) renders the full memory content, not a chunk excerpt, so
// GetMemoriesForInjection must backfill it for its search-path hits.
func TestGetMemoriesForInjectionBackfillsContent(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	const content = "remember-token-42: the deployment key lives in vault."
	if _, err := store.UpsertMemory(ctx, MemoryUpsertOptions{
		FilePath: "memory/project/deploy.md",
		Content:  content,
		Scope:    "project/",
	}); err != nil {
		t.Fatalf("UpsertMemory() error = %v", err)
	}

	results, err := store.GetMemoriesForInjection(ctx, "remember-token-42", 5, 0, nil)
	if err != nil {
		t.Fatalf("GetMemoriesForInjection() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one memory result")
	}
	if results[0].Document.Content != content {
		t.Fatalf("Document.Content = %q, want %q", results[0].Document.Content, content)
	}
}

// TestDocumentContentByID exercises the backfill helper directly: it must
// return exactly the requested ids' content and nothing for an id that does
// not exist, and tolerate an empty id slice.
func TestDocumentContentByID(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	if err := store.AddDocument(ctx, "docs/a.md", "body a", nil); err != nil {
		t.Fatalf("AddDocument(a) error = %v", err)
	}
	if err := store.AddDocument(ctx, "docs/b.md", "body b", nil); err != nil {
		t.Fatalf("AddDocument(b) error = %v", err)
	}

	docA, err := store.GetDocument(ctx, "docs/a.md")
	if err != nil || docA == nil {
		t.Fatalf("GetDocument(a) error = %v, doc = %v", err, docA)
	}
	docB, err := store.GetDocument(ctx, "docs/b.md")
	if err != nil || docB == nil {
		t.Fatalf("GetDocument(b) error = %v, doc = %v", err, docB)
	}

	out, err := store.documentContentByID(ctx, []int64{docA.ID, docB.ID, 999999})
	if err != nil {
		t.Fatalf("documentContentByID() error = %v", err)
	}
	if out[docA.ID] != "body a" {
		t.Fatalf("content[a] = %q, want %q", out[docA.ID], "body a")
	}
	if out[docB.ID] != "body b" {
		t.Fatalf("content[b] = %q, want %q", out[docB.ID], "body b")
	}
	if _, ok := out[999999]; ok {
		t.Fatal("content map has an entry for a non-existent id")
	}

	if out, err := store.documentContentByID(ctx, nil); err != nil || out != nil {
		t.Fatalf("documentContentByID(nil) = %v, %v; want nil, nil", out, err)
	}
}
