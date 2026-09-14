package kb

import (
	"context"
	"testing"
	"time"
)

// TestRepairFrontMatterMetadataFixesWatcherDamage covers PANDO-US-0003's
// startup-repair acceptance criterion: a document whose metadata was
// stripped down to bare source_* fields (simulating what the pre-fix watcher
// did on every edit) gets its tags and custom front-matter key restored by
// RepairFrontMatterMetadata, ignoring the mtime that made the sync/watcher
// paths think the file was unchanged.
func TestRepairFrontMatterMetadataFixesWatcherDamage(t *testing.T) {
	ctx := context.Background()
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)

	raw := "" +
		"---\n" +
		"tags:\n" +
		"  - rag\n" +
		"  - kb\n" +
		"project: pando\n" +
		"---\n" +
		"# Body\n"
	absPath := writeKBFile(t, dir, "story.md", raw, mtime)

	// Simulate the pre-fix watcher: index the document with only bare
	// source_* metadata and the front-matter block still in the body, and
	// record the file's real current mtime so the regular sync would treat
	// it as unchanged (the exact trap PANDO-US-0003 describes).
	damaged := map[string]interface{}{
		"source_path":       absPath,
		"source_mtime_unix": mtime.Unix(),
		"source_format":     "md",
	}
	if err := store.AddDocument(ctx, "story.md", raw, damaged); err != nil {
		t.Fatalf("AddDocument() (seed damaged doc) error = %v", err)
	}

	stats, err := store.RepairFrontMatterMetadata(ctx, dir)
	if err != nil {
		t.Fatalf("RepairFrontMatterMetadata() error = %v", err)
	}
	if stats.Scanned != 1 {
		t.Fatalf("Scanned = %d, want 1", stats.Scanned)
	}
	if stats.Repaired != 1 {
		t.Fatalf("Repaired = %d, want 1", stats.Repaired)
	}

	doc, err := store.GetDocument(ctx, "story.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc == nil {
		t.Fatalf("expected story.md to still be indexed after repair")
	}
	tags := ExtractTagsFromMetadata(doc.Metadata)
	if len(tags) != 2 || tags[0] != "rag" || tags[1] != "kb" {
		t.Fatalf("tags after repair = %v, want [rag kb]", tags)
	}
	if doc.Metadata["project"] != "pando" {
		t.Fatalf("metadata.project after repair = %v, want pando", doc.Metadata["project"])
	}
	// ParseFrontMatterWithRaw trims the whole input first, so the body loses
	// its own trailing newline too — this matches what the sync path already
	// produces for the same raw content, not a new behavior from the repair.
	if doc.Content != "# Body" {
		t.Fatalf("content after repair = %q, want front-matter-stripped body", doc.Content)
	}
}

// TestRepairFrontMatterMetadataIsOneShot covers the "does not re-run on the
// next start" acceptance criterion: a second call for the same directory
// must not re-scan the corpus at all (Scanned stays 0), because the first
// successful run recorded a marker.
func TestRepairFrontMatterMetadataIsOneShot(t *testing.T) {
	ctx := context.Background()
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)

	raw := "---\ntags:\n  - rag\n---\nBody\n"
	absPath := writeKBFile(t, dir, "story.md", raw, mtime)
	damaged := map[string]interface{}{
		"source_path":       absPath,
		"source_mtime_unix": mtime.Unix(),
		"source_format":     "md",
	}
	if err := store.AddDocument(ctx, "story.md", raw, damaged); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	first, err := store.RepairFrontMatterMetadata(ctx, dir)
	if err != nil {
		t.Fatalf("RepairFrontMatterMetadata() first run error = %v", err)
	}
	if first.Repaired != 1 {
		t.Fatalf("first run Repaired = %d, want 1", first.Repaired)
	}

	second, err := store.RepairFrontMatterMetadata(ctx, dir)
	if err != nil {
		t.Fatalf("RepairFrontMatterMetadata() second run error = %v", err)
	}
	if second.Scanned != 0 || second.Repaired != 0 {
		t.Fatalf("second run = %+v, want a no-scan no-op (one-shot marker should have short-circuited it)", second)
	}
}

// TestRepairFrontMatterMetadataNoOpOnHealthyCorpus covers "the repair is a
// no-op on a corpus indexed after this change": a document indexed through
// the fixed sync path already has every front-matter key in its metadata, so
// the repair pass must scan it but write nothing.
func TestRepairFrontMatterMetadataNoOpOnHealthyCorpus(t *testing.T) {
	ctx := context.Background()
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)

	writeKBFile(t, dir, "story.md", ""+
		"---\n"+
		"tags:\n"+
		"  - rag\n"+
		"project: pando\n"+
		"---\n"+
		"Body\n",
		mtime)

	if _, err := store.SyncDirectoryWithStats(ctx, dir, false); err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}

	rec := &recorder{}
	store.SetWriteObserver(rec.observe)

	stats, err := store.RepairFrontMatterMetadata(ctx, dir)
	if err != nil {
		t.Fatalf("RepairFrontMatterMetadata() error = %v", err)
	}
	if stats.Scanned != 1 {
		t.Fatalf("Scanned = %d, want 1 (still looked at the document)", stats.Scanned)
	}
	if stats.Repaired != 0 {
		t.Fatalf("Repaired = %d, want 0 (nothing to fix on a healthy corpus)", stats.Repaired)
	}
	if got := len(rec.seen()); got != 0 {
		t.Fatalf("write events published during a no-op repair = %d, want 0 (no document should have been rewritten)", got)
	}
}

// TestRepairFrontMatterMetadataSkipsNonFilesystemDocuments covers that
// synthetic/memory documents (no source_path) — including the repair's own
// marker document — are never picked up by the scan.
func TestRepairFrontMatterMetadataSkipsNonFilesystemDocuments(t *testing.T) {
	ctx := context.Background()
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()

	if err := store.AddDocument(ctx, "memory/note.md", "a fact", map[string]interface{}{
		"tags": []string{"memory"},
	}); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	stats, err := store.RepairFrontMatterMetadata(ctx, dir)
	if err != nil {
		t.Fatalf("RepairFrontMatterMetadata() error = %v", err)
	}
	if stats.Scanned != 0 || stats.Repaired != 0 {
		t.Fatalf("stats = %+v, want a no-op (no filesystem-backed document present)", stats)
	}
}
