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

// The front matter a KB file records is the document's history: indexing the
// file (startup sync, watcher, re-index) must keep its created_at/updated_at
// instead of re-dating the document to the moment it was indexed.

var (
	datesCreated = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	datesUpdated = time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
)

const datedDoc = "" +
	"---\n" +
	"created_at: 2020-01-01T00:00:00Z\n" +
	"updated_at: 2020-01-02T00:00:00Z\n" +
	"tags:\n" +
	"  - probe\n" +
	"---\n" +
	"# Dated document\n"

func mustGetDocument(t *testing.T, store *KBStore, path string) *Document {
	t.Helper()
	doc, err := store.GetDocument(context.Background(), path)
	if err != nil {
		t.Fatalf("GetDocument(%q) error = %v", path, err)
	}
	if doc == nil {
		t.Fatalf("GetDocument(%q): document not indexed", path)
	}
	return doc
}

func assertDocumentDates(t *testing.T, doc *Document, created, updated time.Time) {
	t.Helper()
	if !doc.CreatedAt.Equal(created) {
		t.Errorf("created_at = %v, want %v", doc.CreatedAt, created)
	}
	if !doc.UpdatedAt.Equal(updated) {
		t.Errorf("updated_at = %v, want %v", doc.UpdatedAt, updated)
	}
}

func syncDir(t *testing.T, store *KBStore, dir string) SyncStats {
	t.Helper()
	stats, err := store.SyncDirectoryWithStats(context.Background(), dir, true)
	if err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}
	return stats
}

func TestSyncKeepsFrontMatterDates(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	writeKBFile(t, dir, "dated.md", datedDoc, time.Now().Add(-time.Hour))

	syncDir(t, store, dir)

	doc := mustGetDocument(t, store, "dated.md")
	assertDocumentDates(t, doc, datesCreated, datesUpdated)
	if tags := doc.Tags; len(tags) != 1 || tags[0] != "probe" {
		t.Errorf("tags = %v, want [probe]", tags)
	}
}

func TestWatcherEditAndTouchKeepFrontMatterDates(t *testing.T) {
	ctx := context.Background()
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	absPath := writeKBFile(t, dir, "dated.md", datedDoc, time.Now().Add(-2*time.Hour))
	syncDir(t, store, dir)

	// An edit of the body that leaves the front matter alone.
	writeKBFile(t, dir, "dated.md", datedDoc+"\nMore text.\n", time.Now().Add(-time.Hour))
	store.handleWatchEvent(ctx, dir, fsnotify.Event{Name: absPath, Op: fsnotify.Write})
	doc := mustGetDocument(t, store, "dated.md")
	if doc.Content == "# Dated document\n" {
		t.Fatalf("watcher did not re-index the edited file")
	}
	assertDocumentDates(t, doc, datesCreated, datesUpdated)

	// A touch: only the mtime changes.
	now := time.Now()
	if err := os.Chtimes(absPath, now, now); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	store.handleWatchEvent(ctx, dir, fsnotify.Event{Name: absPath, Op: fsnotify.Write})
	assertDocumentDates(t, mustGetDocument(t, store, "dated.md"), datesCreated, datesUpdated)
}

func TestReindexKeepsFrontMatterDates(t *testing.T) {
	dir := t.TempDir()
	absPath := writeKBFile(t, dir, "dated.md", datedDoc, time.Now().Add(-2*time.Hour))

	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	syncDir(t, store, dir)

	// The mtime changes (git checkout, pull, touch): the next sync updates it.
	later := time.Now().Add(-time.Hour)
	if err := os.Chtimes(absPath, later, later); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	if stats := syncDir(t, store, dir); stats.Updated != 1 {
		t.Fatalf("second sync updated %d documents, want 1", stats.Updated)
	}
	assertDocumentDates(t, mustGetDocument(t, store, "dated.md"), datesCreated, datesUpdated)

	// The index is lost: a fresh store re-imports the directory from scratch.
	fresh := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	syncDir(t, fresh, dir)
	assertDocumentDates(t, mustGetDocument(t, fresh, "dated.md"), datesCreated, datesUpdated)
}

func TestUpdateWithoutFrontMatterDatesKeepsStoredCreatedAt(t *testing.T) {
	ctx := context.Background()
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	absPath := writeKBFile(t, dir, "dated.md", datedDoc, time.Now().Add(-time.Hour))
	syncDir(t, store, dir)

	// The file loses its front matter; the document keeps its creation time.
	writeKBFile(t, dir, "dated.md", "# No front matter any more\n", time.Now())
	before := time.Now().UTC().Add(-time.Second)
	store.handleWatchEvent(ctx, dir, fsnotify.Event{Name: absPath, Op: fsnotify.Write})
	doc := mustGetDocument(t, store, "dated.md")
	if !doc.CreatedAt.Equal(datesCreated) {
		t.Errorf("created_at = %v, want the stored %v", doc.CreatedAt, datesCreated)
	}
	if doc.UpdatedAt.Before(before) {
		t.Errorf("updated_at = %v, want now (after %v)", doc.UpdatedAt, before)
	}

	// The plain UpdateDocument (API, memory, tools) keeps it too.
	if err := store.UpdateDocument(ctx, "dated.md", "# Updated through the API\n", nil); err != nil {
		t.Fatalf("UpdateDocument() error = %v", err)
	}
	doc = mustGetDocument(t, store, "dated.md")
	if !doc.CreatedAt.Equal(datesCreated) {
		t.Errorf("after UpdateDocument created_at = %v, want %v", doc.CreatedAt, datesCreated)
	}
}

func TestFrontMatterDatesAreStoredInUTC(t *testing.T) {
	ctx := context.Background()
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	writeKBFile(t, dir, "offset.md", ""+
		"---\n"+
		"created_at: 2021-03-04T05:06:07.123456789+02:00\n"+
		"updated_at: 2021-03-05T05:06:07.5-05:00\n"+
		"---\n"+
		"# Offset dates\n", time.Now().Add(-time.Hour))
	syncDir(t, store, dir)

	wantCreated := time.Date(2021, 3, 4, 3, 6, 7, 123456789, time.UTC)
	wantUpdated := time.Date(2021, 3, 5, 10, 6, 7, 500000000, time.UTC)
	doc := mustGetDocument(t, store, "offset.md")
	assertDocumentDates(t, doc, wantCreated, wantUpdated)
	if doc.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at location = %v, want UTC", doc.CreatedAt.Location())
	}

	// The stored value itself is UTC, not just equal as an instant.
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT CAST(created_at AS TEXT) FROM kb_documents WHERE file_path = ?`, "offset.md").Scan(&raw); err != nil {
		t.Fatalf("read raw created_at error = %v", err)
	}
	parsed, err := parseStoredTime(raw)
	if err != nil {
		t.Fatalf("stored created_at %q does not parse: %v", raw, err)
	}
	if _, offset := parsed.Zone(); offset != 0 {
		t.Errorf("stored created_at %q has offset %d, want UTC", raw, offset)
	}
}

// parseStoredTime parses the textual form the SQLite driver stores times in.
func parseStoredTime(raw string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		time.RFC3339Nano,
	}
	var err error
	for _, layout := range layouts {
		var parsed time.Time
		if parsed, err = time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, err
}

func TestFrontMatterCreatedAtWithoutUpdatedAt(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	writeKBFile(t, dir, "created-only.md", ""+
		"---\n"+
		"created_at: 2020-01-01T00:00:00Z\n"+
		"---\n"+
		"# Created only\n", time.Now().Add(-time.Hour))
	writeKBFile(t, dir, "backwards.md", ""+
		"---\n"+
		"created_at: 2020-01-02T00:00:00Z\n"+
		"updated_at: 2020-01-01T00:00:00Z\n"+
		"---\n"+
		"# Updated before created\n", time.Now().Add(-time.Hour))

	before := time.Now().UTC().Add(-time.Second)
	syncDir(t, store, dir)

	// No updated_at: it is the import time.
	doc := mustGetDocument(t, store, "created-only.md")
	if !doc.CreatedAt.Equal(datesCreated) {
		t.Errorf("created_at = %v, want %v", doc.CreatedAt, datesCreated)
	}
	if doc.UpdatedAt.Before(before) {
		t.Errorf("updated_at = %v, want the import time (after %v)", doc.UpdatedAt, before)
	}

	// updated_at earlier than created_at is raised to created_at.
	assertDocumentDates(t, mustGetDocument(t, store, "backwards.md"), datesUpdated, datesUpdated)
}

func TestMirrorSourceMetadataOnlyForMarkdown(t *testing.T) {
	store := NewKBStore(openTestKBDB(t), fakeEmbedder{}, 0, 0)
	dir := t.TempDir()
	if err := store.ConfigureFilesystemMirror(dir); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}
	for _, p := range []string{"notes/a.md", "notes/todo"} {
		if err := store.WriteDocumentToFilesystem(p, "body\n"); err != nil {
			t.Fatalf("WriteDocumentToFilesystem(%q) error = %v", p, err)
		}
	}

	meta := store.MirrorSourceMetadata("notes/a.md")
	if meta["source_path"] != filepath.Join(dir, "notes", "a.md") || meta["source_format"] != "md" {
		t.Errorf("MirrorSourceMetadata(notes/a.md) = %v", meta)
	}
	fi, err := os.Stat(filepath.Join(dir, "notes", "a.md"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if meta["source_mtime_unix"] != fi.ModTime().Unix() {
		t.Errorf("source_mtime_unix = %v, want %d", meta["source_mtime_unix"], fi.ModTime().Unix())
	}
	// The sync never visits a non-markdown path, so it must not get a
	// source_path its delete-missing pass would act on.
	if meta := store.MirrorSourceMetadata("notes/todo"); meta != nil {
		t.Errorf("MirrorSourceMetadata(notes/todo) = %v, want nil", meta)
	}
}

func TestSerializeFrontMatterWithExtraRoundTrips(t *testing.T) {
	fm := FrontMatter{CreatedAt: datesCreated, UpdatedAt: datesUpdated, Tags: []string{"probe"}, Aliases: []string{"alt"}}
	extra := map[string]interface{}{"status": "draft", "project": "pando", "tags": []string{"ignored"}}
	out := SerializeFrontMatterWithExtra(fm, extra, "# Body\n")

	parsed, raw, body, err := ParseFrontMatterWithRaw(out)
	if err != nil {
		t.Fatalf("ParseFrontMatterWithRaw() error = %v\n%s", err, out)
	}
	// ParseFrontMatterWithRaw trims trailing whitespace from the document.
	if strings.TrimSpace(body) != "# Body" {
		t.Errorf("body = %q", body)
	}
	if !parsed.CreatedAt.Equal(datesCreated) || !parsed.UpdatedAt.Equal(datesUpdated) {
		t.Errorf("dates = %v/%v", parsed.CreatedAt, parsed.UpdatedAt)
	}
	if len(parsed.Tags) != 1 || parsed.Tags[0] != "probe" || len(parsed.Aliases) != 1 {
		t.Errorf("tags/aliases = %v/%v, want [probe]/[alt]", parsed.Tags, parsed.Aliases)
	}
	if raw["status"] != "draft" || raw["project"] != "pando" {
		t.Errorf("extra keys lost: %v", raw)
	}
}
