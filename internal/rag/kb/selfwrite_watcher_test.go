package kb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// TestShouldSkipSelfWriteEventMatchesRecordedWrite is the direct,
// deterministic form of PANDO-US-0004's second acceptance criterion: an
// fsnotify event whose path and on-disk mtime match a recorded self-write is
// dropped.
func TestShouldSkipSelfWriteEventMatchesRecordedWrite(t *testing.T) {
	store := &KBStore{}
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Minute)
	absPath := writeKBFile(t, dir, "note.md", "body", mtime)

	store.recordSelfWrite(absPath, mtime.Unix())

	if !store.shouldSkipSelfWriteEvent(fsnotify.Event{Name: absPath, Op: fsnotify.Write}) {
		t.Fatal("expected a self-write event with matching path+mtime to be skipped")
	}
}

// TestShouldSkipSelfWriteEventProcessesDifferentMtime is the other half: an
// event for the same path but a different mtime (the file changed again
// since the self-write was recorded) must be processed, not skipped.
func TestShouldSkipSelfWriteEventProcessesDifferentMtime(t *testing.T) {
	store := &KBStore{}
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Minute)
	absPath := writeKBFile(t, dir, "note.md", "body", mtime)

	// Record a self-write with a stale mtime, simulating that the file was
	// hand-edited after the recorded mirror write.
	store.recordSelfWrite(absPath, mtime.Add(-time.Hour).Unix())

	if store.shouldSkipSelfWriteEvent(fsnotify.Event{Name: absPath, Op: fsnotify.Write}) {
		t.Fatal("expected an event whose mtime differs from the recorded self-write to be processed, not skipped")
	}
}

func TestShouldSkipSelfWriteEventMatchesRecordedDelete(t *testing.T) {
	store := &KBStore{}
	dir := t.TempDir()
	absPath := filepath.Join(dir, "gone.md")
	store.recordSelfDelete(absPath)

	if !store.shouldSkipSelfWriteEvent(fsnotify.Event{Name: absPath, Op: fsnotify.Remove}) {
		t.Fatal("expected a self-delete event to be skipped")
	}
}

func TestShouldSkipSelfWriteEventNoEntryProcessesNormally(t *testing.T) {
	store := &KBStore{}
	dir := t.TempDir()
	mtime := time.Now().Add(-time.Minute)
	absPath := writeKBFile(t, dir, "note.md", "body", mtime)

	if store.shouldSkipSelfWriteEvent(fsnotify.Event{Name: absPath, Op: fsnotify.Write}) {
		t.Fatal("expected an event with no recorded self-write to be processed, not skipped")
	}
}

// startTestWatcher starts store.WatchDirectory on dir in the background and
// returns a cancel func that stops it and waits for it to exit. It gives the
// fsnotify watch a moment to be installed before returning, so a write made
// right after this call is not missed.
func startTestWatcher(t *testing.T, store *KBStore, dir string) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- store.WatchDirectory(ctx, dir)
	}()
	time.Sleep(150 * time.Millisecond)
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("WatchDirectory() exited with error = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("WatchDirectory() did not exit after cancel")
		}
	}
}

// TestMirrorSelfWriteDoesNotClobberMetadata is the fourth acceptance
// criterion: writing a document with metadata under a live watcher must not
// have the mirror's own write feed back through the watcher and erase that
// metadata. This mirrors what internal/llm/tools/remembrances_kb.go's
// KBAddDocumentTool does (AddDocument, then WriteDocumentToFilesystem), kept
// inside internal/rag/kb since that is this change's scope.
func TestMirrorSelfWriteDoesNotClobberMetadata(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	if err := store.ConfigureFilesystemMirror(dir); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}

	stop := startTestWatcher(t, store, dir)
	defer stop()

	ctx := context.Background()
	metadata := map[string]interface{}{"status": "x"}
	if err := store.AddDocument(ctx, "note.md", "Body.", metadata); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	mirrorContent := SerializeFrontMatter(NewFrontMatter(nil, nil), "Body.")
	if err := store.WriteDocumentToFilesystem("note.md", mirrorContent); err != nil {
		t.Fatalf("WriteDocumentToFilesystem() error = %v", err)
	}

	// The acceptance criterion asks for the metadata to still be there five
	// seconds later: comfortably past the watcher's 250ms debounce and the
	// suppression TTL, so a delayed clobber would show up here.
	time.Sleep(5 * time.Second)

	doc, err := store.GetDocument(ctx, "note.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc == nil {
		t.Fatalf("expected note.md to still be indexed")
	}
	if doc.Metadata["status"] != "x" {
		t.Fatalf("metadata.status = %v, want x (the mirror's self-write must not have been fed back through the watcher)",
			doc.Metadata["status"])
	}
}

// TestHandEditToMirroredFileStillIndexed is the fifth acceptance criterion:
// suppressing the mirror's own writes must not turn into suppressing the
// watcher altogether. A human editing a previously-mirrored file directly on
// disk must still be picked up.
func TestHandEditToMirroredFileStillIndexed(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	if err := store.ConfigureFilesystemMirror(dir); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}

	stop := startTestWatcher(t, store, dir)
	defer stop()

	ctx := context.Background()

	// Seed the document the same way the mirror tool would, so there is a
	// self-write entry the watcher suppresses for this specific write.
	if err := store.AddDocument(ctx, "note.md", "Original.", map[string]interface{}{}); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	seedContent := SerializeFrontMatter(NewFrontMatter([]string{"seed"}, nil), "Original.")
	if err := store.WriteDocumentToFilesystem("note.md", seedContent); err != nil {
		t.Fatalf("WriteDocumentToFilesystem() error = %v", err)
	}
	// Let the (suppressed) self-write event fully settle before the hand
	// edit, and make sure the hand edit lands in a different wall-clock
	// second: mtimes are compared at one-second resolution
	// (source_mtime_unix), so a hand edit landing in the same second as the
	// seed write could coincidentally share its mtime and be suppressed too.
	time.Sleep(1200 * time.Millisecond)

	// A human now edits the mirrored file directly on disk: not through
	// WriteDocumentToFilesystem, so no self-write entry covers this write.
	target := filepath.Join(dir, "note.md")
	handEdited := SerializeFrontMatter(NewFrontMatter([]string{"seed"}, nil), "Hand-edited content.")
	if err := os.WriteFile(target, []byte(handEdited), 0o644); err != nil {
		t.Fatalf("WriteFile() (hand edit) error = %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var doc *Document
	for time.Now().Before(deadline) {
		var gerr error
		doc, gerr = store.GetDocument(ctx, "note.md")
		if gerr != nil {
			t.Fatalf("GetDocument() error = %v", gerr)
		}
		if doc != nil && strings.Contains(doc.Content, "Hand-edited content.") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if doc == nil {
		t.Fatalf("expected note.md to still be indexed after the hand edit")
	}
	if !strings.Contains(doc.Content, "Hand-edited content.") {
		t.Fatalf("hand edit was not indexed within 5s, content = %q", doc.Content)
	}
}
