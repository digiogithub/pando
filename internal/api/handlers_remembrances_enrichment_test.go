// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// enrichmentDimEmbedder returns a fixed-dimension, fixed-direction vector
// regardless of content, enough to control the "configured" dimension for
// the stale-embedding count without a real provider.
type enrichmentDimEmbedder struct{ dim int }

func (e enrichmentDimEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func (e enrichmentDimEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, e.dim)
	v[0] = 1
	return v, nil
}

func (e enrichmentDimEmbedder) Dimension() int { return e.dim }

// setTestDocumentEmbeddingModel sets Remembrances.DocumentEmbeddingModel on
// the global config for the duration of the test, restoring whatever was
// there before (creating a fresh empty config first if none was loaded yet —
// config.Get() can be nil in a test binary that never called config.Load()).
func setTestDocumentEmbeddingModel(t *testing.T, model string) {
	t.Helper()
	if config.Get() == nil {
		config.SetForTests(&config.Config{})
		t.Cleanup(config.ResetForTests)
	} else {
		prev := config.Get().Remembrances.DocumentEmbeddingModel
		t.Cleanup(func() {
			if cfg := config.Get(); cfg != nil {
				cfg.Remembrances.DocumentEmbeddingModel = prev
			}
		})
	}
	config.Get().Remembrances.DocumentEmbeddingModel = model
}

// TestHandleGetEnrichmentStatus_ReportsStaleChunksAndModel is a regression
// test for PANDO-US-0029: GET /api/v1/remembrances/enrichment must report
// stale_chunks and the active document embedding model.
func TestHandleGetEnrichmentStatus_ReportsStaleChunksAndModel(t *testing.T) {
	db := openTestKBDB(t)
	ctx := context.Background()

	// A chunk written under a 3-dim embedder/model.
	store3 := kb.NewKBStore(db, enrichmentDimEmbedder{dim: 3}, 0, 0)
	store3.SetEmbeddingModel("model-v1")
	if err := store3.AddDocument(ctx, "docs/old.md", "old content", nil); err != nil {
		t.Fatalf("AddDocument(old) error = %v", err)
	}

	// The "currently configured" embedder is 5-dim: the chunk above is now
	// stale relative to it.
	svc := &rag.RemembrancesService{KB: store3}
	svc.SetDocumentEmbedder(enrichmentDimEmbedder{dim: 5})

	setTestDocumentEmbeddingModel(t, "model-v2")

	s := &Server{app: &app.App{Remembrances: svc}}

	rec := httptest.NewRecorder()
	s.handleGetEnrichmentStatus(rec, doJSONRequest(http.MethodGet, "/api/v1/remembrances/enrichment", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got struct {
		StaleChunks            int64  `json:"stale_chunks"`
		DocumentEmbeddingModel string `json:"document_embedding_model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, rec.Body.String())
	}
	if got.StaleChunks != 1 {
		t.Fatalf("stale_chunks = %d, want 1", got.StaleChunks)
	}
	if got.DocumentEmbeddingModel != "model-v2" {
		t.Fatalf("document_embedding_model = %q, want %q", got.DocumentEmbeddingModel, "model-v2")
	}
}

// TestHandleGetEnrichmentStatus_NoStaleChunksOnConsistentCorpus asserts the
// zero-value path: a consistent corpus (or no remembrances/embedder at all)
// reports stale_chunks = 0 without erroring.
func TestHandleGetEnrichmentStatus_NoStaleChunksOnConsistentCorpus(t *testing.T) {
	db := openTestKBDB(t)
	ctx := context.Background()

	store := kb.NewKBStore(db, enrichmentDimEmbedder{dim: 3}, 0, 0)
	store.SetEmbeddingModel("model-v1")
	if err := store.AddDocument(ctx, "docs/a.md", "content", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	svc := &rag.RemembrancesService{KB: store}
	svc.SetDocumentEmbedder(enrichmentDimEmbedder{dim: 3})

	s := &Server{app: &app.App{Remembrances: svc}}

	rec := httptest.NewRecorder()
	s.handleGetEnrichmentStatus(rec, doJSONRequest(http.MethodGet, "/api/v1/remembrances/enrichment", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got struct {
		StaleChunks int64 `json:"stale_chunks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, rec.Body.String())
	}
	if got.StaleChunks != 0 {
		t.Fatalf("stale_chunks = %d, want 0 on a consistent corpus", got.StaleChunks)
	}
}

// TestHandleGetEnrichmentStatus_NilRemembrancesDoesNotPanic asserts the route
// still answers safely when remembrances/KB is not available.
func TestHandleGetEnrichmentStatus_NilRemembrancesDoesNotPanic(t *testing.T) {
	s := &Server{app: &app.App{}}
	rec := httptest.NewRecorder()
	s.handleGetEnrichmentStatus(rec, doJSONRequest(http.MethodGet, "/api/v1/remembrances/enrichment", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
