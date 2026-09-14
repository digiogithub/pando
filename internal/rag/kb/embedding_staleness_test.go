package kb

import (
	"context"
	"testing"
)

// dimEmbedder returns a fixed-dimension, fixed-direction vector regardless
// of content — enough to simulate an embedder swap to a different
// dimensionality without needing a real provider.
type dimEmbedder struct{ dim int }

func (e dimEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func (e dimEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, e.dim)
	v[0] = 1
	return v, nil
}

func (e dimEmbedder) Dimension() int { return e.dim }

// TestAddDocumentRecordsEmbeddingModelAndDims is a regression test for
// PANDO-US-0029: every newly written chunk must record the model id and
// dimension of the embedder that produced it.
func TestAddDocumentRecordsEmbeddingModelAndDims(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, dimEmbedder{dim: 3}, 0, 0)
	store.SetEmbeddingModel("text-embedding-test-v1")
	ctx := context.Background()

	if err := store.AddDocument(ctx, "docs/a.md", "some content to embed", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	var model string
	var dims int
	if err := db.QueryRowContext(ctx,
		`SELECT embedding_model, embedding_dims FROM kb_chunks WHERE document_id = (SELECT id FROM kb_documents WHERE file_path = ?)`,
		"docs/a.md",
	).Scan(&model, &dims); err != nil {
		t.Fatalf("query chunk embedding metadata: %v", err)
	}
	if model != "text-embedding-test-v1" {
		t.Fatalf("embedding_model = %q, want %q", model, "text-embedding-test-v1")
	}
	if dims != 3 {
		t.Fatalf("embedding_dims = %d, want 3", dims)
	}
}

// TestSearchDocumentsWithOptionsAndStats_SkipsStaleEmbeddingDimension is a
// regression test for PANDO-US-0029's detection half: switching the
// configured embedder's dimension must (a) count the chunks the vector leg
// skips for the mismatch, and (b) still return chunks whose recorded
// dimension matches the current embedder.
func TestSearchDocumentsWithOptionsAndStats_SkipsStaleEmbeddingDimension(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, dimEmbedder{dim: 3}, 0, 0)
	store.SetEmbeddingModel("model-v1")
	ctx := context.Background()

	if err := store.AddDocument(ctx, "docs/old.md", "marker-alpha content", nil); err != nil {
		t.Fatalf("AddDocument(old) error = %v", err)
	}

	// Switch the configured embedder to a different dimension/model, as an
	// operator would after reconfiguring Remembrances.DocumentEmbeddingModel.
	store.embedder = dimEmbedder{dim: 5}
	store.SetEmbeddingModel("model-v2")

	if err := store.AddDocument(ctx, "docs/new.md", "marker-beta content", nil); err != nil {
		t.Fatalf("AddDocument(new) error = %v", err)
	}

	// Query for a term unique to the new, matching-dimension document so the
	// FTS leg (which is dimension-independent) does not also surface the
	// stale chunk and confound what this test is isolating: the vector leg's
	// skip counter and its exclusion of the stale-dimension candidate.
	results, stats, err := store.SearchDocumentsWithOptionsAndStats(ctx, "marker-beta", 5, SearchOptions{})
	if err != nil {
		t.Fatalf("SearchDocumentsWithOptionsAndStats() error = %v", err)
	}
	if stats.SkippedForDimensionMismatch != 1 {
		t.Fatalf("SkippedForDimensionMismatch = %d, want 1", stats.SkippedForDimensionMismatch)
	}
	if len(results) == 0 {
		t.Fatal("expected the matching-dimension document to still be returned")
	}
	found := false
	for _, r := range results {
		if r.Document.FilePath == "docs/new.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("matching-dimension document missing from results: %+v", results)
	}
}

// TestCountStaleEmbeddings exercises the startup-check helper directly: zero
// on a consistent corpus, the right count plus the distinct recorded model
// names once chunks fall out of sync with the configured dimension.
func TestCountStaleEmbeddings(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, dimEmbedder{dim: 3}, 0, 0)
	store.SetEmbeddingModel("model-v1")
	ctx := context.Background()

	if err := store.AddDocument(ctx, "docs/a.md", "content a", nil); err != nil {
		t.Fatalf("AddDocument(a) error = %v", err)
	}

	// Consistent corpus: no chunk differs from the configured dimension.
	stats, err := store.CountStaleEmbeddings(ctx, 3)
	if err != nil {
		t.Fatalf("CountStaleEmbeddings() error = %v", err)
	}
	if stats.Count != 0 {
		t.Fatalf("Count = %d, want 0 on a consistent corpus", stats.Count)
	}
	if len(stats.RecordedModels) != 0 {
		t.Fatalf("RecordedModels = %v, want none on a consistent corpus", stats.RecordedModels)
	}

	// A second document written under a different configured model/dimension
	// leaves the first document's chunk stale relative to the new dimension.
	store.embedder = dimEmbedder{dim: 5}
	store.SetEmbeddingModel("model-v2")
	if err := store.AddDocument(ctx, "docs/b.md", "content b", nil); err != nil {
		t.Fatalf("AddDocument(b) error = %v", err)
	}

	stats, err = store.CountStaleEmbeddings(ctx, 5)
	if err != nil {
		t.Fatalf("CountStaleEmbeddings() error = %v", err)
	}
	if stats.Count != 1 {
		t.Fatalf("Count = %d, want 1", stats.Count)
	}
	if len(stats.RecordedModels) != 1 || stats.RecordedModels[0] != "model-v1" {
		t.Fatalf("RecordedModels = %v, want [model-v1]", stats.RecordedModels)
	}
}
