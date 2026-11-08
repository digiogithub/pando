package kb

import (
	"context"
	"testing"
	"time"
)

// TestSyncDirectoryPreservesUnknownFrontMatterKeys covers PANDO-US-0002: a
// document's front matter can declare host-defined keys (status, project,
// milestone, ...) that the typed FrontMatter struct does not name, and those
// keys must still land in the document's stored metadata after a sync.
func TestSyncDirectoryPreservesUnknownFrontMatterKeys(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)
	writeKBFile(t, dir, "story.md", ""+
		"---\n"+
		"id: PANDO-US-0002\n"+
		"type: story\n"+
		"status: backlog\n"+
		"project: pando\n"+
		"milestone: PANDO-M-0002\n"+
		"tags:\n"+
		"  - rag\n"+
		"  - kb\n"+
		"---\n"+
		"# Story body\n",
		mtime)

	if _, err := store.SyncDirectoryWithStats(ctx, dir, false); err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}

	doc, err := store.GetDocument(ctx, "story.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc == nil {
		t.Fatalf("expected document to be indexed")
	}

	if doc.Metadata["id"] != "PANDO-US-0002" {
		t.Errorf("expected metadata.id == PANDO-US-0002, got %v", doc.Metadata["id"])
	}
	if doc.Metadata["type"] != "story" {
		t.Errorf("expected metadata.type == story, got %v", doc.Metadata["type"])
	}
	if doc.Metadata["status"] != "backlog" {
		t.Errorf("expected metadata.status == backlog, got %v", doc.Metadata["status"])
	}
	if doc.Metadata["project"] != "pando" {
		t.Errorf("expected metadata.project == pando, got %v", doc.Metadata["project"])
	}
	if doc.Metadata["milestone"] != "PANDO-M-0002" {
		t.Errorf("expected metadata.milestone == PANDO-M-0002, got %v", doc.Metadata["milestone"])
	}

	// tags/aliases still go through the existing typed injection path.
	tags := ExtractTagsFromMetadata(doc.Metadata)
	if len(tags) != 2 || tags[0] != "rag" || tags[1] != "kb" {
		t.Errorf("expected tags [rag kb] via InjectTagsIntoMetadata, got %v", tags)
	}

	// Reserved sync-owned fields keep their authoritative values.
	if doc.Metadata["source_path"] == nil || doc.Metadata["source_path"] == "" {
		t.Errorf("expected source_path to be set by sync, got %v", doc.Metadata["source_path"])
	}
}

// TestSyncDirectoryFrontMatterKeyCannotOverwriteSourcePath verifies a
// front-matter key literally named "source_path" cannot shadow the real,
// sync-computed source_path metadata field.
func TestSyncDirectoryFrontMatterKeyCannotOverwriteSourcePath(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)
	writeKBFile(t, dir, "collide.md", ""+
		"---\n"+
		"source_path: /not/the/real/path.md\n"+
		"status: backlog\n"+
		"---\n"+
		"Body\n",
		mtime)

	if _, err := store.SyncDirectoryWithStats(ctx, dir, false); err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}

	doc, err := store.GetDocument(ctx, "collide.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc == nil {
		t.Fatalf("expected document to be indexed")
	}

	sourcePath, _ := doc.Metadata["source_path"].(string)
	if sourcePath == "/not/the/real/path.md" {
		t.Fatalf("front-matter source_path overwrote the real source_path metadata field")
	}
	if sourcePath == "" {
		t.Fatalf("expected a real source_path to be set, got empty")
	}
	if doc.Metadata["status"] != "backlog" {
		t.Errorf("expected unreserved key status to still be merged, got %v", doc.Metadata["status"])
	}
}

// TestSyncDirectoryUnparseableFrontMatterStillIndexed verifies that a
// document whose front matter fails to parse is still indexed, with the raw
// content used as the body (existing fallback behavior).
func TestSyncDirectoryUnparseableFrontMatterStillIndexed(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)
	// Duplicate top-level key makes this invalid YAML front matter.
	raw := "---\nstatus: backlog\nstatus: done\n---\nBody text\n"
	writeKBFile(t, dir, "broken.md", raw, mtime)

	stats, err := store.SyncDirectoryWithStats(ctx, dir, false)
	if err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}
	if stats.Added != 1 {
		t.Fatalf("expected 1 document added, got %d", stats.Added)
	}

	doc, err := store.GetDocument(ctx, "broken.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc == nil {
		t.Fatalf("expected document to still be indexed despite unparseable front matter")
	}
	if doc.Content != raw {
		t.Errorf("expected raw content indexed as-is when front matter fails to parse, got %q", doc.Content)
	}
	if doc.Metadata["status"] != nil {
		t.Errorf("expected no status metadata pulled from unparseable front matter, got %v", doc.Metadata["status"])
	}
}
