package kb

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/digiogithub/pando/rag/embeddings"
)

func TestOpenDefaults(t *testing.T) {
	db := openTestKBDB(t)
	s, err := Open(db, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.chunkSize != embeddings.DefaultChunkSize || !s.WikiLinksEnabled() || s.getSyncWorkers() != defaultSyncWorkers() {
		t.Fatalf("zero Options: chunk=%d wiki=%v workers=%d", s.chunkSize, s.WikiLinksEnabled(), s.getSyncWorkers())
	}
	if s.logger() != slog.Default() || s.retryable != nil {
		t.Fatal("zero Options must fall back to slog.Default and the default classifier")
	}
}

func TestOpenAppliesOptions(t *testing.T) {
	db := openTestKBDB(t)
	l := slog.New(slog.DiscardHandler)
	s, err := Open(db, Options{
		ChunkSize: 500, ChunkOverlap: 600, Workers: 3, DisableWikiLinks: true,
		EmbeddingModel: "m", Logger: l, Retryable: func(error) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.chunkSize != 500 || s.chunkOverlap != 250 || s.getSyncWorkers() != 3 || s.WikiLinksEnabled() ||
		s.EmbeddingModel() != "m" || s.logger() != l || s.retryable == nil || !s.retryable(errors.New("x")) {
		t.Fatalf("options not applied: %+v", s)
	}
	if _, err := Open(nil, Options{}); err == nil {
		t.Fatal("Open(nil) must fail")
	}
}
