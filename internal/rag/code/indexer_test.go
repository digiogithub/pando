package code

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/digiogithub/pando/internal/rag/treesitter"
)

type testEmbedder struct {
	gotNilCtx bool
}

func (e *testEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if ctx == nil {
		e.gotNilCtx = true
	}
	return []float32{0.1}, nil
}

func (e *testEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if ctx == nil {
		e.gotNilCtx = true
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

func TestEmbedSymbols_NilContextUsesBackground(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE code_symbols (id TEXT PRIMARY KEY, embedding BLOB)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO code_symbols (id) VALUES ('sym-1')`); err != nil {
		t.Fatalf("seed symbol: %v", err)
	}

	emb := &testEmbedder{}
	idx := &CodeIndexer{db: db, embedder: emb}

	syms := []*treesitter.CodeSymbol{
		{
			ID:         "sym-1",
			NamePath:   "pkg.Func",
			SourceCode: "func Func() {}",
		},
	}

	if err := idx.embedSymbols(nil, "proj", 1, syms); err != nil {
		t.Fatalf("embedSymbols returned error: %v", err)
	}
	if emb.gotNilCtx {
		t.Fatalf("embedder received nil context")
	}
}
