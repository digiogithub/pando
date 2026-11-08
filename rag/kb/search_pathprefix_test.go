package kb

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// prefixTestEmbedder gives deterministic, non-tied vector scores so the
// under-return test below does not depend on how ties are broken: content
// containing "HIGH" embeds parallel to the query vector (cosine 1.0), every
// other chunk embeds orthogonal to it (cosine 0.0).
type prefixTestEmbedder struct{}

func (prefixTestEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if strings.Contains(t, "HIGH") {
			out[i] = []float32{1, 0, 0}
		} else {
			out[i] = []float32{0, 1, 0}
		}
	}
	return out, nil
}

func (prefixTestEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (prefixTestEmbedder) Dimension() int { return 3 }

// TestSearchPathPrefixAvoidsUnderReturn is a regression test for
// PANDO-US-0028: without a SQL-level path filter, a query whose top
// candidates all belong to another prefix silently excludes every document
// under the prefix the caller actually wants, even below the requested
// limit. Pushing the filter into the SQL fixes that.
func TestSearchPathPrefixAvoidsUnderReturn(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, prefixTestEmbedder{}, 0, 0)
	ctx := context.Background()

	// Ten noise documents score a perfect cosine match on the query and also
	// contain the FTS term, so they dominate both search legs.
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("noise/doc-%02d.md", i)
		if err := store.AddDocument(ctx, path, "HIGH relevance widget content", nil); err != nil {
			t.Fatalf("AddDocument(noise %d) error = %v", i, err)
		}
	}
	// Two target documents score a zero cosine match (orthogonal embedding)
	// and never match the FTS term "widget" at all, so an unprefixed query
	// has nothing to recommend them over the noise.
	if err := store.AddDocument(ctx, "target/one.md", "low relevance filler", nil); err != nil {
		t.Fatalf("AddDocument(target one) error = %v", err)
	}
	if err := store.AddDocument(ctx, "target/two.md", "low relevance filler", nil); err != nil {
		t.Fatalf("AddDocument(target two) error = %v", err)
	}

	// Unprefixed: the two target documents are crowded out entirely, even
	// though the caller asked for 2 results.
	unprefixed, err := store.SearchDocumentsWithOptions(ctx, "widget", 2, SearchOptions{})
	if err != nil {
		t.Fatalf("unprefixed search error = %v", err)
	}
	if len(unprefixed) != 2 {
		t.Fatalf("unprefixed results = %d, want 2 (the noise docs)", len(unprefixed))
	}
	for _, r := range unprefixed {
		if r.Document.FilePath == "target/one.md" || r.Document.FilePath == "target/two.md" {
			t.Fatalf("target document %q unexpectedly present in the unprefixed top-2 — test setup does not exercise under-return", r.Document.FilePath)
		}
	}

	// Prefixed to "target/": the SQL-level filter returns the full limit of
	// results from the target prefix, exactly the under-return fix.
	prefixed, err := store.SearchDocumentsWithOptions(ctx, "widget", 2, SearchOptions{PathPrefix: "target/"})
	if err != nil {
		t.Fatalf("prefixed search error = %v", err)
	}
	if len(prefixed) != 2 {
		t.Fatalf("prefixed results = %d, want 2 (the full limit, both target docs)", len(prefixed))
	}
	for _, r := range prefixed {
		if r.Document.FilePath != "target/one.md" && r.Document.FilePath != "target/two.md" {
			t.Fatalf("prefixed result %q does not belong to target/", r.Document.FilePath)
		}
	}

	// Candidate count scanned equals the number of matching chunks: a
	// large-limit vector scan restricted to "target/" must return exactly the
	// 2 chunks under that prefix, never any of the 10 noise chunks — proof
	// the WHERE clause runs in SQL rather than as a post-fusion Go filter
	// over a candidate pool that already excluded them.
	queryEmb, err := (prefixTestEmbedder{}).EmbedQuery(ctx, "widget")
	if err != nil {
		t.Fatalf("EmbedQuery() error = %v", err)
	}
	scanned, _, err := store.searchVector(ctx, queryEmb, 1000, "target/")
	if err != nil {
		t.Fatalf("searchVector() error = %v", err)
	}
	if len(scanned) != 2 {
		t.Fatalf("candidates scanned under target/ = %d, want exactly 2", len(scanned))
	}
}

// TestSearchPathPrefixEscapesLikeMetacharacters is a regression test for the
// ESCAPE clause: a prefix containing a literal '%' or '_' must match only
// that literal text, not act as a SQL LIKE wildcard.
func TestSearchPathPrefixEscapesLikeMetacharacters(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	// same shared content everywhere: only the path prefix distinguishes hits.
	const body = "shared searchable content"

	// '_' case: an unescaped '_' in the prefix would match any single
	// character, so "a_b/real.md" would incorrectly also match "axb/decoy.md".
	if err := store.AddDocument(ctx, "a_b/real.md", body, nil); err != nil {
		t.Fatalf("AddDocument(a_b) error = %v", err)
	}
	if err := store.AddDocument(ctx, "axb/decoy.md", body, nil); err != nil {
		t.Fatalf("AddDocument(axb) error = %v", err)
	}

	got, err := store.SearchDocumentsWithOptions(ctx, "shared", 10, SearchOptions{PathPrefix: "a_b/"})
	if err != nil {
		t.Fatalf("search with '_' prefix error = %v", err)
	}
	assertOnlyPaths(t, got, "a_b/real.md")

	// '%' case: an unescaped '%' in the prefix would match any run of
	// characters, so "c%d/real.md" would incorrectly also match
	// "cZZZd/decoy.md".
	if err := store.AddDocument(ctx, "c%d/real.md", body, nil); err != nil {
		t.Fatalf("AddDocument(c%%d) error = %v", err)
	}
	if err := store.AddDocument(ctx, "cZZZd/decoy.md", body, nil); err != nil {
		t.Fatalf("AddDocument(cZZZd) error = %v", err)
	}

	got, err = store.SearchDocumentsWithOptions(ctx, "shared", 10, SearchOptions{PathPrefix: "c%d/"})
	if err != nil {
		t.Fatalf("search with '%%' prefix error = %v", err)
	}
	assertOnlyPaths(t, got, "c%d/real.md")
}

func assertOnlyPaths(t *testing.T, results []SearchResult, want ...string) {
	t.Helper()
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w] = true
	}
	if len(results) != len(want) {
		got := make([]string, len(results))
		for i, r := range results {
			got[i] = r.Document.FilePath
		}
		t.Fatalf("results = %v, want exactly %v", got, want)
	}
	for _, r := range results {
		if !wantSet[r.Document.FilePath] {
			t.Fatalf("unexpected result %q, want only %v", r.Document.FilePath, want)
		}
	}
}
