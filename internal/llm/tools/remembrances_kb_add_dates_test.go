package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/rag/kb"
)

// kb_add_document rewrites a document's file. It must keep what the file
// records (created_at, aliases, host-defined keys) instead of rebuilding the
// front matter from the index alone, and it must stamp the index with the
// file's mtime so the next startup sync does not re-import it.

var addDatesCreated = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

const addDatesFile = "" +
	"---\n" +
	"created_at: 2020-01-01T00:00:00Z\n" +
	"updated_at: 2020-01-02T00:00:00Z\n" +
	"tags:\n" +
	"  - probe\n" +
	"aliases:\n" +
	"  - dated-alias\n" +
	"status: draft\n" +
	"owner:\n" +
	"  team: kb\n" +
	"---\n" +
	"# Original body\n"

func openKBAddDatesStore(t *testing.T) (*kb.KBStore, string) {
	t.Helper()
	store, _ := openKBSearchTestStore(t, kbSearchTestEmbedder{dim: 3})
	dir := t.TempDir()
	if err := store.ConfigureFilesystemMirror(dir); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}
	return store, dir
}

func writeAddDatesFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(full, old, old); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	return full
}

func runKBAdd(t *testing.T, store *kb.KBStore, input map[string]any) ToolResponse {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	resp, err := NewKBAddDocumentTool(store).Run(context.Background(), ToolCall{Input: string(raw)})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.IsError {
		t.Fatalf("kb_add_document error response: %s", resp.Content)
	}
	return resp
}

// assertDatedFileKept checks both the index and the rewritten file keep the
// 2020 creation time, the aliases and the host-defined keys.
func assertDatedFileKept(t *testing.T, store *kb.KBStore, dir string, writeStart time.Time) {
	t.Helper()
	doc, err := store.GetDocument(context.Background(), "dated.md")
	if err != nil || doc == nil {
		t.Fatalf("GetDocument() = %v, %v", doc, err)
	}
	if !doc.CreatedAt.Equal(addDatesCreated) {
		t.Errorf("index created_at = %v, want %v", doc.CreatedAt, addDatesCreated)
	}
	if doc.UpdatedAt.Before(writeStart) {
		t.Errorf("index updated_at = %v, want the write time (after %v)", doc.UpdatedAt, writeStart)
	}
	if strings.TrimSpace(doc.Content) != "# New body" {
		t.Errorf("index content = %q", doc.Content)
	}
	if tags := doc.Tags; len(tags) != 1 || tags[0] != "probe" {
		t.Errorf("index tags = %v, want [probe]", tags)
	}
	if aliases := kb.ExtractAliasesFromMetadata(doc.Metadata); len(aliases) != 1 || aliases[0] != "dated-alias" {
		t.Errorf("index aliases = %v, want [dated-alias]", aliases)
	}
	if doc.Metadata["status"] != "draft" {
		t.Errorf("index status = %v, want draft", doc.Metadata["status"])
	}

	absPath := filepath.Join(dir, "dated.md")
	raw, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	fm, rawFM, body, err := kb.ParseFrontMatterWithRaw(string(raw))
	if err != nil {
		t.Fatalf("rewritten file front matter does not parse: %v\n%s", err, raw)
	}
	if !fm.CreatedAt.Equal(addDatesCreated) {
		t.Errorf("file created_at = %v, want %v", fm.CreatedAt, addDatesCreated)
	}
	if !fm.UpdatedAt.Equal(doc.UpdatedAt) {
		t.Errorf("file updated_at = %v, want the index's %v", fm.UpdatedAt, doc.UpdatedAt)
	}
	if len(fm.Tags) != 1 || fm.Tags[0] != "probe" || len(fm.Aliases) != 1 || fm.Aliases[0] != "dated-alias" {
		t.Errorf("file tags/aliases = %v/%v", fm.Tags, fm.Aliases)
	}
	if rawFM["status"] != "draft" {
		t.Errorf("file status = %v, want draft", rawFM["status"])
	}
	if owner, ok := rawFM["owner"].(map[string]interface{}); !ok || owner["team"] != "kb" {
		t.Errorf("file owner = %#v, want {team: kb}", rawFM["owner"])
	}
	if strings.TrimSpace(body) != "# New body" {
		t.Errorf("file body = %q", body)
	}

	// The index records the file it wrote, so a startup sync skips it.
	fi, err := os.Stat(absPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if doc.Metadata["source_path"] != absPath {
		t.Errorf("source_path = %v, want %s", doc.Metadata["source_path"], absPath)
	}
	if mtime, _ := doc.Metadata["source_mtime_unix"].(float64); int64(mtime) != fi.ModTime().Unix() {
		t.Errorf("source_mtime_unix = %v, want %d", doc.Metadata["source_mtime_unix"], fi.ModTime().Unix())
	}
}

func TestKBAddDocumentUpdateKeepsFileFrontMatter(t *testing.T) {
	store, dir := openKBAddDatesStore(t)
	writeAddDatesFile(t, dir, "dated.md", addDatesFile)
	if _, err := store.SyncDirectoryWithStats(context.Background(), dir, true); err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}

	writeStart := time.Now().UTC().Add(-time.Second)
	resp := runKBAdd(t, store, map[string]any{"file_path": "dated.md", "content": "# New body\n"})
	if !strings.Contains(resp.Content, "Document updated") {
		t.Errorf("response = %q, want an update", resp.Content)
	}
	assertDatedFileKept(t, store, dir, writeStart)

	// The next startup sync sees the file as unchanged.
	stats, err := store.SyncDirectoryWithStats(context.Background(), dir, true)
	if err != nil {
		t.Fatalf("second SyncDirectoryWithStats() error = %v", err)
	}
	if stats.Updated != 0 || stats.Added != 0 || stats.Deleted != 0 {
		t.Errorf("second sync stats = %+v, want the written file unchanged", stats)
	}
}

func TestKBAddDocumentOverUnindexedFileKeepsItsFrontMatter(t *testing.T) {
	// The index was lost (or the file was never imported): the file is the
	// only record of the document's history.
	store, dir := openKBAddDatesStore(t)
	writeAddDatesFile(t, dir, "dated.md", addDatesFile)

	writeStart := time.Now().UTC().Add(-time.Second)
	resp := runKBAdd(t, store, map[string]any{"file_path": "dated.md", "content": "# New body\n"})
	if !strings.Contains(resp.Content, "Document added") {
		t.Errorf("response = %q, want an add", resp.Content)
	}
	assertDatedFileKept(t, store, dir, writeStart)
}

func TestKBAddDocumentNewDocumentAndTagReplacement(t *testing.T) {
	store, dir := openKBAddDatesStore(t)
	before := time.Now().UTC().Add(-time.Second)
	runKBAdd(t, store, map[string]any{"file_path": "notes/new.md", "content": "# New\n", "tags": []string{"a"}})

	doc, err := store.GetDocument(context.Background(), "notes/new.md")
	if err != nil || doc == nil {
		t.Fatalf("GetDocument() = %v, %v", doc, err)
	}
	if doc.CreatedAt.Before(before) || !doc.UpdatedAt.Equal(doc.CreatedAt) {
		t.Errorf("new document dates = %v/%v, want both now", doc.CreatedAt, doc.UpdatedAt)
	}
	created := doc.CreatedAt

	// An update with new tags replaces them in the index and the file, and
	// keeps the creation time.
	runKBAdd(t, store, map[string]any{"file_path": "notes/new.md", "content": "# New 2\n", "tags": []string{"b"}})
	doc, _ = store.GetDocument(context.Background(), "notes/new.md")
	if !doc.CreatedAt.Equal(created) {
		t.Errorf("created_at after update = %v, want %v", doc.CreatedAt, created)
	}
	if len(doc.Tags) != 1 || doc.Tags[0] != "b" {
		t.Errorf("tags after update = %v, want [b]", doc.Tags)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "notes", "new.md"))
	fm, _, _ := kb.ParseFrontMatter(string(raw))
	if len(fm.Tags) != 1 || fm.Tags[0] != "b" || !fm.CreatedAt.Equal(created) {
		t.Errorf("file front matter = %+v, want tags [b] and created_at %v", fm, created)
	}

	// A path the sync does not index gets no source stamp, or the sync's
	// delete-missing pass would remove it.
	runKBAdd(t, store, map[string]any{"file_path": "notes/todo", "content": "plain note"})
	doc, _ = store.GetDocument(context.Background(), "notes/todo")
	if _, ok := doc.Metadata["source_path"]; ok {
		t.Errorf("non-markdown document got source_path %v", doc.Metadata["source_path"])
	}
	if _, err := store.SyncDirectoryWithStats(context.Background(), dir, true); err != nil {
		t.Fatalf("SyncDirectoryWithStats() error = %v", err)
	}
	if doc, _ := store.GetDocument(context.Background(), "notes/todo"); doc == nil {
		t.Errorf("sync deleted the non-markdown document")
	}
}
