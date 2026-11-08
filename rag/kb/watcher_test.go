package kb

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// TestWatcherAndSyncBuildIdenticalMetadata covers PANDO-US-0003's first
// acceptance criterion: the watcher and the sync path build metadata and body
// through one shared helper (buildDocumentMetadata), so indexing the same
// file through either path produces identical metadata and body content.
func TestWatcherAndSyncBuildIdenticalMetadata(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)
	raw := "" +
		"---\n" +
		"status: backlog\n" +
		"project: pando\n" +
		"tags:\n" +
		"  - rag\n" +
		"  - kb\n" +
		"---\n" +
		"# Story body\n"
	absPath := writeKBFile(t, dir, "story.md", raw, mtime)

	// Sync path.
	syncStore := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	if _, err := syncStore.SyncDirectoryWithStats(ctx, dir, false); err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}
	syncDoc, err := syncStore.GetDocument(ctx, "story.md")
	if err != nil {
		t.Fatalf("GetDocument() (sync) error = %v", err)
	}
	if syncDoc == nil {
		t.Fatalf("expected sync to index story.md")
	}

	// Watcher path: call handleWatchEvent directly, as the fsnotify loop would
	// after a debounced create/write event.
	watchStore := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	watchStore.handleWatchEvent(ctx, dir, fsnotify.Event{Name: absPath, Op: fsnotify.Create})
	watchDoc, err := watchStore.GetDocument(ctx, "story.md")
	if err != nil {
		t.Fatalf("GetDocument() (watch) error = %v", err)
	}
	if watchDoc == nil {
		t.Fatalf("expected watcher to index story.md")
	}

	if !reflect.DeepEqual(syncDoc.Metadata, watchDoc.Metadata) {
		t.Fatalf("metadata differs between sync and watcher paths:\nsync=%#v\nwatch=%#v",
			syncDoc.Metadata, watchDoc.Metadata)
	}

	// Second acceptance criterion: the body stored by the watcher is
	// front-matter-stripped, matching the sync path.
	if syncDoc.Content != watchDoc.Content {
		t.Fatalf("content differs between sync and watcher paths:\nsync=%q\nwatch=%q",
			syncDoc.Content, watchDoc.Content)
	}
	if strings.Contains(watchDoc.Content, "---") || strings.Contains(watchDoc.Content, "status: backlog") {
		t.Fatalf("watcher-indexed content still carries front matter: %q", watchDoc.Content)
	}
}

// TestWatcherEditPreservesTagsAndCustomFrontMatterKey is the regression test
// for PANDO-US-0003: before the fix, handleWatchEvent built metadata from
// only source_path/source_mtime_unix/source_format, and UpdateDocument is
// delete-then-add, so the first edit made through the watcher permanently
// erased tags and any other front-matter key an initial sync had stored.
func TestWatcherEditPreservesTagsAndCustomFrontMatterKey(t *testing.T) {
	ctx := context.Background()
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour)

	original := "" +
		"---\n" +
		"tags:\n" +
		"  - rag\n" +
		"  - kb\n" +
		"project: pando\n" +
		"---\n" +
		"# Original body\n"
	absPath := writeKBFile(t, dir, "story.md", original, base)

	// Initial index, as the watcher would do on a fsnotify.Create event.
	store.handleWatchEvent(ctx, dir, fsnotify.Event{Name: absPath, Op: fsnotify.Create})

	doc, err := store.GetDocument(ctx, "story.md")
	if err != nil {
		t.Fatalf("GetDocument() after create error = %v", err)
	}
	if doc == nil {
		t.Fatalf("expected story.md to be indexed after create event")
	}
	tags := ExtractTagsFromMetadata(doc.Metadata)
	if len(tags) != 2 || tags[0] != "rag" || tags[1] != "kb" {
		t.Fatalf("tags after create = %v, want [rag kb]", tags)
	}
	if doc.Metadata["project"] != "pando" {
		t.Fatalf("metadata.project after create = %v, want pando", doc.Metadata["project"])
	}

	// Edit the file's body (front matter unchanged, mtime moves forward) and
	// let the watcher pick up the change, exactly like a fsnotify.Write event
	// debounced through handleWithDebounce in WatchDirectory.
	edited := "" +
		"---\n" +
		"tags:\n" +
		"  - rag\n" +
		"  - kb\n" +
		"project: pando\n" +
		"---\n" +
		"# Edited body\n"
	writeKBFile(t, dir, "story.md", edited, base.Add(time.Minute))
	store.handleWatchEvent(ctx, dir, fsnotify.Event{Name: absPath, Op: fsnotify.Write})

	doc2, err := store.GetDocument(ctx, "story.md")
	if err != nil {
		t.Fatalf("GetDocument() after edit error = %v", err)
	}
	if doc2 == nil {
		t.Fatalf("expected story.md to still be indexed after edit")
	}
	if !strings.Contains(doc2.Content, "Edited body") {
		t.Fatalf("expected the edit to take effect, content = %q", doc2.Content)
	}

	tags2 := ExtractTagsFromMetadata(doc2.Metadata)
	if len(tags2) != 2 || tags2[0] != "rag" || tags2[1] != "kb" {
		t.Fatalf("tags after edit = %v, want [rag kb] (must survive the watcher's UpdateDocument)", tags2)
	}
	if doc2.Metadata["project"] != "pando" {
		t.Fatalf("metadata.project after edit = %v, want pando (must survive the watcher's UpdateDocument)",
			doc2.Metadata["project"])
	}
}

// TestWatcherAddDocumentAlsoParsesFrontMatter covers the AddDocument branch of
// handleWatchEvent (a brand new file seen by the watcher, existingMeta == nil):
// it must parse front matter too, not just the UpdateDocument branch.
func TestWatcherAddDocumentAlsoParsesFrontMatter(t *testing.T) {
	ctx := context.Background()
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Hour)

	raw := "---\ntags:\n  - new\n---\nBody\n"
	absPath := writeKBFile(t, dir, "new.md", raw, mtime)

	store.handleWatchEvent(ctx, dir, fsnotify.Event{Name: absPath, Op: fsnotify.Create})

	doc, err := store.GetDocument(ctx, "new.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc == nil {
		t.Fatalf("expected new.md to be indexed")
	}
	tags := ExtractTagsFromMetadata(doc.Metadata)
	if len(tags) != 1 || tags[0] != "new" {
		t.Fatalf("tags = %v, want [new]", tags)
	}
	if strings.Contains(doc.Content, "tags:") {
		t.Fatalf("expected front matter stripped from body, got %q", doc.Content)
	}
}
