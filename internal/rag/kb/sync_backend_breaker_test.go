package kb

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// timeoutEmbedder fails every call the way a saturated Ollama does: the request
// outlives its deadline.
type timeoutEmbedder struct{ calls atomic.Int32 }

func (e *timeoutEmbedder) EmbedDocuments(_ context.Context, _ []string) ([][]float32, error) {
	e.calls.Add(1)
	return nil, fmt.Errorf("ollama: text 0: ollama: request failed: %w", context.DeadlineExceeded)
}

func (e *timeoutEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return nil, context.DeadlineExceeded
}

func (e *timeoutEmbedder) Dimension() int { return 3 }

// TestSyncStopsWhenEmbeddingBackendUnavailable checks that a sync against an
// unavailable embedding backend gives up after kbSyncBackendFailureLimit
// documents instead of sending every document to it.
func TestSyncStopsWhenEmbeddingBackendUnavailable(t *testing.T) {
	db := openTestKBDB(t)
	embedder := &timeoutEmbedder{}
	store := NewKBStore(db, embedder, 0, 0)
	ctx := context.Background()

	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 10; i++ {
		writeKBFile(t, dir, fmt.Sprintf("doc-%02d.md", i), fmt.Sprintf("Document %d body.", i), base)
	}

	_, err := store.SyncDirectoryWithStats(ctx, dir, true)
	if !errors.Is(err, ErrSyncEmbeddingBackendUnavailable) {
		t.Fatalf("SyncDirectoryWithStats() error = %v, want ErrSyncEmbeddingBackendUnavailable", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("SyncDirectoryWithStats() error = %v, want it to wrap the backend error", err)
	}
	if got := embedder.calls.Load(); got != kbSyncBackendFailureLimit {
		t.Errorf("embedder calls = %d, want %d", got, kbSyncBackendFailureLimit)
	}
}
