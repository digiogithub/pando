package kb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeKBFile writes a document to the sync directory with an explicit mtime, so
// a test can make a file look newer without waiting for the clock: the sync skips
// files whose mtime has not changed.
func writeKBFile(t *testing.T, dir, name, body string, mtime time.Time) string {
	t.Helper()

	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chtimes(full, mtime, mtime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	return full
}

func TestSyncReportsLinksIndexed(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	writeKBFile(t, dir, "alpha.md", "Alpha builds on [[beta]] and [[gamma]].", base)
	writeKBFile(t, dir, "beta.md", "Beta stands alone.", base)

	stats, err := store.SyncDirectoryWithStats(ctx, dir, false)
	if err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}
	if stats.Added != 2 {
		t.Fatalf("Added = %d, want 2", stats.Added)
	}
	if stats.LinksIndexed != 2 {
		t.Errorf("LinksIndexed = %d, want 2 (both links of alpha.md)", stats.LinksIndexed)
	}

	// A run that changes nothing indexes nothing: unchanged documents keep the
	// links they already had, so counting them again would overstate the run.
	stats, err = store.SyncDirectoryWithStats(ctx, dir, false)
	if err != nil {
		t.Fatalf("SyncDirectoryWithStats() second run error = %v", err)
	}
	if stats.Unchanged != 2 {
		t.Fatalf("Unchanged = %d, want 2", stats.Unchanged)
	}
	if stats.LinksIndexed != 0 {
		t.Errorf("LinksIndexed on a no-op run = %d, want 0", stats.LinksIndexed)
	}
}

// TestSyncRefreshesLinksOfEditedFile covers the path the filesystem watcher takes:
// an edit on disk flows through UpdateDocument, so the document's links must end
// up matching the new body, not the old one.
func TestSyncRefreshesLinksOfEditedFile(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)
	writeKBFile(t, dir, "alpha.md", "Alpha builds on [[beta]].", base)

	if _, err := store.SyncDirectoryWithStats(ctx, dir, false); err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}

	writeKBFile(t, dir, "alpha.md", "Alpha now builds on [[gamma]] instead.", base.Add(time.Minute))

	stats, err := store.SyncDirectoryWithStats(ctx, dir, false)
	if err != nil {
		t.Fatalf("SyncDirectoryWithStats() after edit error = %v", err)
	}
	if stats.Updated != 1 {
		t.Fatalf("Updated = %d, want 1", stats.Updated)
	}
	if stats.LinksIndexed != 1 {
		t.Errorf("LinksIndexed = %d, want 1", stats.LinksIndexed)
	}

	links, err := store.LinksFrom(ctx, "alpha.md")
	if err != nil {
		t.Fatalf("LinksFrom() error = %v", err)
	}
	if len(links) != 1 || links[0].Slug != "gamma" {
		t.Errorf("LinksFrom() after edit = %#v, want only gamma", links)
	}
}

// TestWikiLinksDisabledWritesNothing is the opt-out guarantee: with the toggle
// off no link row is written and every graph query answers empty, so the KB tools
// present the output they presented before the graph existed.
func TestWikiLinksDisabledWritesNothing(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	store.SetWikiLinksEnabled(false)
	ctx := context.Background()

	const alpha = "pando/changes/alpha.md"
	if err := store.AddDocument(ctx, alpha, "Alpha builds on [[beta]].", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	if err := store.AddDocument(ctx, "pando/changes/beta.md", "Beta.", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	links, err := store.LinksFrom(ctx, alpha)
	if err != nil {
		t.Fatalf("LinksFrom() error = %v", err)
	}
	if len(links) != 0 {
		t.Errorf("LinksFrom() with wiki links off = %#v, want none", links)
	}

	has, err := store.HasLinks(ctx)
	if err != nil {
		t.Fatalf("HasLinks() error = %v", err)
	}
	if has {
		t.Error("HasLinks() = true with wiki links off, want false")
	}

	outgoing, err := store.OutgoingLinks(ctx, alpha)
	if err != nil {
		t.Fatalf("OutgoingLinks() error = %v", err)
	}
	if len(outgoing) != 0 {
		t.Errorf("OutgoingLinks() = %#v, want none", outgoing)
	}

	related, err := store.RelatedDocuments(ctx, alpha, 5)
	if err != nil {
		t.Fatalf("RelatedDocuments() error = %v", err)
	}
	if len(related) != 0 {
		t.Errorf("RelatedDocuments() = %#v, want none", related)
	}

	wanted, err := store.WantedConcepts(ctx, 5)
	if err != nil {
		t.Fatalf("WantedConcepts() error = %v", err)
	}
	if len(wanted) != 0 {
		t.Errorf("WantedConcepts() = %#v, want none", wanted)
	}

	backfill, err := store.BackfillLinks(ctx)
	if err != nil {
		t.Fatalf("BackfillLinks() error = %v", err)
	}
	if backfill.Links != 0 {
		t.Errorf("BackfillLinks() indexed %d links with the graph off, want 0", backfill.Links)
	}
}

// TestWikiLinksReenabledRecoversTheGraph: the content is the source of truth, so
// turning the graph back on and running the backfill rebuilds what was skipped —
// no re-embedding, no rewrite of the documents.
func TestWikiLinksReenabledRecoversTheGraph(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	store.SetWikiLinksEnabled(false)
	ctx := context.Background()

	const alpha = "pando/changes/alpha.md"
	if err := store.AddDocument(ctx, alpha, "Alpha builds on [[beta]].", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	store.SetWikiLinksEnabled(true)

	stats, err := store.BackfillLinks(ctx)
	if err != nil {
		t.Fatalf("BackfillLinks() error = %v", err)
	}
	if stats.Links != 1 {
		t.Fatalf("BackfillLinks() indexed %d links, want 1", stats.Links)
	}

	links, err := store.LinksFrom(ctx, alpha)
	if err != nil {
		t.Fatalf("LinksFrom() error = %v", err)
	}
	if len(links) != 1 || links[0].Slug != "beta" {
		t.Errorf("LinksFrom() after re-enable = %#v, want beta", links)
	}
}
