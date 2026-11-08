package kb

import (
	"context"
	"database/sql"
	"hash/fnv"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digiogithub/pando/rag/migrate"
)

// apiEmbedder is a deterministic, offline bag-of-words embedder: each word
// lights one of 64 dimensions, so texts sharing words have a positive cosine.
// It counts the chunks it embeds so tests can assert that nothing was
// re-embedded.
type apiEmbedder struct {
	model    string
	embedded atomic.Int64
}

func (f *apiEmbedder) vec(text string) []float32 {
	v := make([]float32, 64)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(strings.Trim(w, ".,:;!?#*`\"'()[]")))
		v[h.Sum32()%64]++
	}
	v[63] += 0.01 // never a zero vector
	return v
}

func (f *apiEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	f.embedded.Add(int64(len(texts)))
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = f.vec(t)
	}
	return out, nil
}

func (f *apiEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	return f.vec(text), nil
}

func (f *apiEmbedder) Dimension() int { return 64 }
func (f *apiEmbedder) Model() string  { return f.model }

// openAPIStore opens a store over a fresh file database migrated with
// rag/migrate, the way a library consumer does. A file (not :memory:) lets
// the two search legs use separate connections.
func openAPIStore(t *testing.T, opts Options) (*KBStore, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rag.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrate.Up(context.Background(), db); err != nil {
		t.Fatalf("migrate.Up: %v", err)
	}
	s, err := Open(db, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, db
}
