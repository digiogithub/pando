package kb

import (
	"context"
	"testing"
)

// insertLegacyDocument writes a document straight into kb_documents, the way an
// older Pando (one without the link graph) left it: content intact, no kb_links
// rows.
func insertLegacyDocument(t *testing.T, store *KBStore, filePath, content string) {
	t.Helper()
	if _, err := store.db.Exec(
		`INSERT INTO kb_documents (file_path, content) VALUES (?, ?)`,
		filePath, content,
	); err != nil {
		t.Fatalf("insert legacy document %q: %v", filePath, err)
	}
}

func countLinks(t *testing.T, store *KBStore) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM kb_links`).Scan(&n); err != nil {
		t.Fatalf("count kb_links: %v", err)
	}
	return n
}

func TestBackfillLinksIndexesLegacyDocuments(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	insertLegacyDocument(t, store, "pando/plans/alpha.md", "Alpha needs [[beta]] and [[pando/features/gamma.md|Gamma]].")
	insertLegacyDocument(t, store, "pando/plans/plain.md", "A document from an older version, with no wiki links at all.")
	insertLegacyDocument(t, store, "pando/plans/fenced.md", "Syntax demo:\n\n```md\n[[not-a-link]]\n```\n")

	stats, err := store.BackfillLinks(ctx)
	if err != nil {
		t.Fatalf("BackfillLinks() error = %v", err)
	}

	// The link-free document is never a candidate; the fenced one is (its content
	// holds "[[") but yields nothing.
	if stats.Candidates != 2 {
		t.Errorf("Candidates = %d, want 2", stats.Candidates)
	}
	if stats.Documents != 1 {
		t.Errorf("Documents = %d, want 1", stats.Documents)
	}
	if stats.Links != 2 {
		t.Errorf("Links = %d, want 2", stats.Links)
	}

	links, err := store.LinksFrom(ctx, "pando/plans/alpha.md")
	if err != nil {
		t.Fatalf("LinksFrom() error = %v", err)
	}
	if len(links) != 2 || links[0].Slug != "beta" || links[1].Slug != "pando/features/gamma" {
		t.Fatalf("LinksFrom() = %#v, want beta + pando/features/gamma", links)
	}
}

func TestBackfillLinksIsIdempotent(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	insertLegacyDocument(t, store, "pando/plans/alpha.md", "Alpha needs [[beta]].")

	if _, err := store.BackfillLinks(ctx); err != nil {
		t.Fatalf("first BackfillLinks() error = %v", err)
	}

	// A second pass must find nothing left to do: an indexed document stops being
	// a candidate, so rows are neither duplicated nor rewritten.
	stats, err := store.BackfillLinks(ctx)
	if err != nil {
		t.Fatalf("second BackfillLinks() error = %v", err)
	}
	if stats.Candidates != 0 || stats.Links != 0 {
		t.Errorf("second pass stats = %+v, want no candidates and no links", stats)
	}
	if n := countLinks(t, store); n != 1 {
		t.Errorf("kb_links rows = %d, want 1", n)
	}
}

// TestBackfillLinksDoesNotRewriteLinklessCandidates pins the cost of the startup
// pass: a document that mentions "[[" only inside a code fence stays a candidate
// forever (it never gets rows), so repeated passes must read it and write
// nothing.
func TestBackfillLinksDoesNotRewriteLinklessCandidates(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	insertLegacyDocument(t, store, "pando/plans/syntax.md", "Write links as `[[concept]]`.")

	for i := range 2 {
		stats, err := store.BackfillLinks(ctx)
		if err != nil {
			t.Fatalf("BackfillLinks() pass %d error = %v", i, err)
		}
		if stats.Candidates != 1 || stats.Scanned != 1 {
			t.Errorf("pass %d stats = %+v, want the document scanned", i, stats)
		}
		if stats.Documents != 0 || stats.Links != 0 {
			t.Errorf("pass %d stats = %+v, want nothing written", i, stats)
		}
	}
	if n := countLinks(t, store); n != 0 {
		t.Errorf("kb_links rows = %d, want 0", n)
	}
}

func TestBackfillLinksSkipsAlreadyIndexedDocuments(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	// Written through the normal path: its links are already in the graph.
	if err := store.AddDocument(ctx, "pando/changes/new.md", "Fresh doc pointing at [[delta]].", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	insertLegacyDocument(t, store, "pando/plans/old.md", "Legacy doc pointing at [[epsilon]].")

	stats, err := store.BackfillLinks(ctx)
	if err != nil {
		t.Fatalf("BackfillLinks() error = %v", err)
	}
	if stats.Candidates != 1 || stats.Scanned != 1 {
		t.Errorf("stats = %+v, want exactly the legacy document scanned", stats)
	}
	if n := countLinks(t, store); n != 2 {
		t.Errorf("kb_links rows = %d, want 2 (one per document)", n)
	}
}

func TestRelinkAllRebuildsTheGraph(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	if err := store.AddDocument(ctx, "pando/changes/new.md", "Points at [[delta]].", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	// Simulate rows left stale by an older extractor: a link the current content
	// does not contain, plus one belonging to no document at all.
	if _, err := store.db.Exec(
		`INSERT INTO kb_links (source_document_id, source_path, target_slug, target_raw, position)
		 SELECT id, file_path, 'stale', 'stale', 9 FROM kb_documents WHERE file_path = ?`,
		"pando/changes/new.md",
	); err != nil {
		t.Fatalf("insert stale link: %v", err)
	}

	stats, err := store.RelinkAll(ctx)
	if err != nil {
		t.Fatalf("RelinkAll() error = %v", err)
	}
	if stats.Candidates != 1 || stats.Links != 1 {
		t.Errorf("stats = %+v, want 1 candidate and 1 link", stats)
	}

	links, err := store.LinksFrom(ctx, "pando/changes/new.md")
	if err != nil {
		t.Fatalf("LinksFrom() error = %v", err)
	}
	if len(links) != 1 || links[0].Slug != "delta" {
		t.Fatalf("LinksFrom() = %#v, want only delta (stale row dropped)", links)
	}
}

// TestBackfillLinksEmptyKB covers the common upgrade case: a knowledge base whose
// documents never used the syntax must be left untouched, with no error.
func TestBackfillLinksEmptyKB(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)

	insertLegacyDocument(t, store, "pando/plans/plain.md", "No wiki syntax here.")

	stats, err := store.BackfillLinks(context.Background())
	if err != nil {
		t.Fatalf("BackfillLinks() error = %v", err)
	}
	if stats != (BackfillStats{}) {
		t.Errorf("stats = %+v, want zero", stats)
	}
}
