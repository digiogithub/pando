package agentvcs

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestStorage creates a temp directory with storage and scanner for testing.
func setupTestStorage(t *testing.T) (*storage, *scanner, string) {
	t.Helper()
	root := t.TempDir()
	stor, err := newStorage(root)
	if err != nil {
		t.Fatalf("newStorage: %v", err)
	}
	scan := newScanner(defaultMaxFileSize, nil)
	return stor, scan, root
}

// createTestWorkDir sets up a working directory with some files.
func createTestWorkDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create a .git marker so IsSafeWorkingDirectory passes.
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, "README.md", "# Test\n")
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	writeFile(t, dir, "pkg/lib.go", "package pkg\n")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestStorageCommitRoundTrip(t *testing.T) {
	stor, _, _ := setupTestStorage(t)

	c := Commit{
		ID:          "abc123",
		ParentID:    "",
		SessionID:   "session-1",
		TreeID:      "tree-1",
		Description: "initial",
		Author:      "agent",
		CreatedAt:   1000,
		FileCount:   2,
		TotalSize:   100,
	}

	if err := stor.SaveCommit(c); err != nil {
		t.Fatalf("SaveCommit: %v", err)
	}

	if !stor.CommitExists("abc123") {
		t.Fatal("CommitExists returned false after save")
	}

	loaded, err := stor.LoadCommit("abc123")
	if err != nil {
		t.Fatalf("LoadCommit: %v", err)
	}

	if loaded.ID != c.ID || loaded.SessionID != c.SessionID || loaded.Description != c.Description {
		t.Fatalf("loaded commit mismatch: got %+v", loaded)
	}
}

func TestStorageTreeDeduplication(t *testing.T) {
	stor, _, _ := setupTestStorage(t)

	entries := []TreeEntry{
		{Path: "a.go", Hash: "h1", Size: 10, ModTime: 1000},
		{Path: "b.go", Hash: "h2", Size: 20, ModTime: 1000},
	}
	treeID, err := computeTreeID(entries)
	if err != nil {
		t.Fatal(err)
	}

	tree := Tree{ID: treeID, Entries: entries}

	// Save twice — should not error.
	if err := stor.SaveTree(tree); err != nil {
		t.Fatalf("first SaveTree: %v", err)
	}
	if err := stor.SaveTree(tree); err != nil {
		t.Fatalf("second SaveTree (dedup): %v", err)
	}

	loaded, err := stor.LoadTree(treeID)
	if err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("loaded tree has %d entries, want 2", len(loaded.Entries))
	}
}

func TestStorageBlobRoundTrip(t *testing.T) {
	stor, _, _ := setupTestStorage(t)

	// Create a temp source file.
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "test.txt")
	content := "hello world\n"
	os.WriteFile(srcPath, []byte(content), 0o644)

	hash := "testhash1234"
	if err := stor.StoreBlob(srcPath, hash); err != nil {
		t.Fatalf("StoreBlob: %v", err)
	}

	if !stor.BlobExists(hash) {
		t.Fatal("BlobExists returned false after store")
	}

	data, err := stor.LoadBlob(hash)
	if err != nil {
		t.Fatalf("LoadBlob: %v", err)
	}
	if string(data) != content {
		t.Fatalf("blob content = %q, want %q", string(data), content)
	}

	// Store again (dedup) — should not error.
	if err := stor.StoreBlob(srcPath, hash); err != nil {
		t.Fatalf("StoreBlob dedup: %v", err)
	}
}

func TestStorageSessionLogRoundTrip(t *testing.T) {
	stor, _, _ := setupTestStorage(t)

	sl := SessionLog{
		SessionID: "s1",
		Commits:   []string{"c1", "c2"},
		CreatedAt: 1000,
		UpdatedAt: 2000,
	}

	if err := stor.SaveSessionLog(sl); err != nil {
		t.Fatalf("SaveSessionLog: %v", err)
	}

	loaded, err := stor.LoadSessionLog("s1")
	if err != nil {
		t.Fatalf("LoadSessionLog: %v", err)
	}
	if len(loaded.Commits) != 2 || loaded.Commits[0] != "c1" {
		t.Fatalf("loaded session log mismatch: %+v", loaded)
	}

	ids, err := stor.ListSessionLogs()
	if err != nil {
		t.Fatalf("ListSessionLogs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "s1" {
		t.Fatalf("ListSessionLogs = %v, want [s1]", ids)
	}
}

func TestComputeTreeIDDeterministic(t *testing.T) {
	entries := []TreeEntry{
		{Path: "a.go", Hash: "h1", Size: 10},
		{Path: "b.go", Hash: "h2", Size: 20},
	}

	id1, err := computeTreeID(entries)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := computeTreeID(entries)
	if err != nil {
		t.Fatal(err)
	}

	if id1 != id2 {
		t.Fatalf("non-deterministic tree ID: %s != %s", id1, id2)
	}

	if len(id1) != 64 {
		t.Fatalf("tree ID length = %d, want 64 (sha256 hex)", len(id1))
	}
}

func TestComputeTreeIDChangeOnDiff(t *testing.T) {
	entries1 := []TreeEntry{{Path: "a.go", Hash: "h1", Size: 10}}
	entries2 := []TreeEntry{{Path: "a.go", Hash: "h2", Size: 10}}

	id1, _ := computeTreeID(entries1)
	id2, _ := computeTreeID(entries2)

	if id1 == id2 {
		t.Fatal("different entries should produce different tree IDs")
	}
}

func TestDiffTrees(t *testing.T) {
	t1 := Tree{Entries: []TreeEntry{
		{Path: "a.go", Hash: "h1", Size: 10},
		{Path: "b.go", Hash: "h2", Size: 20},
		{Path: "c.go", Hash: "h3", Size: 30},
	}}
	t2 := Tree{Entries: []TreeEntry{
		{Path: "a.go", Hash: "h1", Size: 10},   // unchanged
		{Path: "b.go", Hash: "h2x", Size: 25},  // modified
		{Path: "d.go", Hash: "h4", Size: 40},    // added
		// c.go deleted
	}}

	diffs := diffTrees(t1, t2)

	byPath := make(map[string]DiffEntry)
	for _, d := range diffs {
		byPath[d.Path] = d
	}

	if len(diffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d: %+v", len(diffs), diffs)
	}
	if byPath["b.go"].Type != DiffModified {
		t.Fatalf("b.go should be modified, got %s", byPath["b.go"].Type)
	}
	if byPath["c.go"].Type != DiffDeleted {
		t.Fatalf("c.go should be deleted, got %s", byPath["c.go"].Type)
	}
	if byPath["d.go"].Type != DiffAdded {
		t.Fatalf("d.go should be added, got %s", byPath["d.go"].Type)
	}
}

func TestScannerRespectGitignore(t *testing.T) {
	dir := createTestWorkDir(t)

	// Add a .gitignore that ignores *.log files.
	writeFile(t, dir, ".gitignore", "*.log\n")
	writeFile(t, dir, "debug.log", "log data")
	writeFile(t, dir, "src/app.go", "package src\n")

	scan := newScanner(defaultMaxFileSize, nil)
	entries, err := scan.Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for _, e := range entries {
		if e.Path == "debug.log" {
			t.Fatal("debug.log should be ignored by .gitignore")
		}
	}

	// Verify .gitignore itself is tracked.
	found := false
	for _, e := range entries {
		if e.Path == ".gitignore" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal(".gitignore should be tracked")
	}
}

func TestScannerExcludePatterns(t *testing.T) {
	dir := createTestWorkDir(t)
	os.MkdirAll(filepath.Join(dir, "node_modules", "dep"), 0o755)
	writeFile(t, dir, "node_modules/dep/index.js", "module.exports = {}")

	scan := newScanner(defaultMaxFileSize, []string{"node_modules/"})
	entries, err := scan.Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for _, e := range entries {
		if e.Path == "node_modules" || len(e.Path) > 13 && e.Path[:13] == "node_modules/" {
			t.Fatalf("node_modules should be excluded, found: %s", e.Path)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("abcdef1234567890"); got != "abcdef123456" {
		t.Fatalf("shortID = %q, want abcdef123456", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Fatalf("shortID short = %q, want abc", got)
	}
}

func TestCommitToSummary(t *testing.T) {
	c := Commit{
		ID:        "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		SessionID: "s1",
		FileCount: 5,
		TotalSize: 1000,
		CreatedAt: 12345,
	}
	s := c.toSummary(3)
	if s.ShortID != "abcdef123456" {
		t.Fatalf("ShortID = %q", s.ShortID)
	}
	if s.ChangedFiles != 3 {
		t.Fatalf("ChangedFiles = %d, want 3", s.ChangedFiles)
	}
}

func TestStorageCleanup(t *testing.T) {
	stor, _, _ := setupTestStorage(t)

	// Create a session log and commits.
	sl := SessionLog{SessionID: "s1", Commits: []string{"c1"}, CreatedAt: 1000, UpdatedAt: 1000}
	stor.SaveSessionLog(sl)

	c := Commit{ID: "c1", SessionID: "s1", TreeID: "t1", CreatedAt: 1000}
	stor.SaveCommit(c)

	tree := Tree{ID: "t1", Entries: []TreeEntry{{Path: "a.go", Hash: "blob1"}}}
	stor.SaveTree(tree)

	// Delete commit and tree manually.
	stor.DeleteCommit("c1")
	if stor.CommitExists("c1") {
		t.Fatal("commit should be deleted")
	}

	stor.DeleteTree("t1")
	stor.DeleteSessionLog("s1")
	if stor.SessionLogExists("s1") {
		t.Fatal("session log should be deleted")
	}
}
