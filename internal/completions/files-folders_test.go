package completions

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/digiogithub/pando/internal/tui/components/dialog"
)

// fakeLister returns a fixed set of "indexed" files, ignoring the query so the
// tests can assert on the merge/preview behaviour deterministically. It is
// queried both from the caller and the background scan goroutine, so the call
// counter is atomic.
type fakeLister struct {
	files   []string
	calls   atomic.Int64
	lastErr error
}

func (f *fakeLister) ListIndexedFiles(_ context.Context, _ string, _ string, _ int) ([]string, error) {
	f.calls.Add(1)
	if f.lastErr != nil {
		return nil, f.lastErr
	}
	return f.files, nil
}

// newProjectDir creates a temporary directory that looks like a project root
// (so IsSafeWorkingDirectory allows the glob) populated with a few source files,
// and chdirs into it for the duration of the test.
func newProjectDir(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	// go.mod is a recognised project marker, making the dir "safe" to scan.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return dir
}

// waitForGlob runs the provider's RefreshCmd (if any) to block until the
// background scan completes, asserting it yields a CompletionRefreshMsg.
func waitForGlob(t *testing.T, p dialog.CompletionProvider) {
	t.Helper()
	async, ok := p.(dialog.AsyncCompletionProvider)
	if !ok {
		t.Fatal("provider does not implement AsyncCompletionProvider")
	}
	// Ensure the background scan has been kicked off before we wait on it.
	if _, err := p.GetChildEntries(""); err != nil {
		t.Fatalf("GetChildEntries: %v", err)
	}
	cmd := async.RefreshCmd()
	if cmd == nil {
		return // scan already finished
	}
	if _, ok := cmd().(dialog.CompletionRefreshMsg); !ok {
		t.Fatal("RefreshCmd did not return CompletionRefreshMsg")
	}
}

func titles(items []dialog.CompletionItemI) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, it := range items {
		out[it.GetValue()] = true
	}
	return out
}

func TestFileCompletion_IndexedPreviewThenGlobMerge(t *testing.T) {
	newProjectDir(t, "main.go", "internal/util.go")

	// "indexed-only.go" exists only in the index (not on disk) to prove the merge
	// keeps indexed entries the filesystem scan would never produce.
	lister := &fakeLister{files: []string{"indexed-only.go", "main.go"}}
	p := NewFileAndFolderContextGroup(lister)

	// Immediate call: glob is still running, so we get the indexed preview.
	preview, err := p.GetChildEntries("")
	if err != nil {
		t.Fatalf("GetChildEntries: %v", err)
	}
	if got := titles(preview); !got["indexed-only.go"] || !got["main.go"] {
		t.Fatalf("expected indexed preview, got %v", got)
	}
	if lister.calls.Load() == 0 {
		t.Fatal("expected the indexer to be queried for the preview")
	}

	// Wait for the background scan, then the merged set must contain both the
	// on-disk files and the indexed-only file.
	waitForGlob(t, p)
	merged, err := p.GetChildEntries("")
	if err != nil {
		t.Fatalf("GetChildEntries after glob: %v", err)
	}
	got := titles(merged)
	for _, want := range []string{"main.go", "internal/util.go", "indexed-only.go"} {
		if !got[want] {
			t.Fatalf("merged set missing %q; got %v", want, got)
		}
	}
}

func TestFileCompletion_FuzzyFiltering(t *testing.T) {
	newProjectDir(t, "main.go", "internal/util.go", "internal/server.go")
	p := NewFileAndFolderContextGroup(nil) // glob-only
	waitForGlob(t, p)

	res, err := p.GetChildEntries("util")
	if err != nil {
		t.Fatalf("GetChildEntries: %v", err)
	}
	got := titles(res)
	if !got["internal/util.go"] {
		t.Fatalf("expected util.go to match query, got %v", got)
	}
	if got["main.go"] {
		t.Fatalf("did not expect main.go for query 'util', got %v", got)
	}
}

func TestFileCompletion_NilListerFallsBackToGlob(t *testing.T) {
	newProjectDir(t, "alpha.go", "beta.go")
	p := NewFileAndFolderContextGroup(nil)

	// With no indexer, the preview before the glob finishes is empty (no panic).
	if items, err := p.GetChildEntries(""); err != nil {
		t.Fatalf("GetChildEntries: %v", err)
	} else if len(items) != 0 {
		t.Fatalf("expected empty preview without indexer, got %d", len(items))
	}

	waitForGlob(t, p)
	got := titles(mustEntries(t, p, ""))
	if !got["alpha.go"] || !got["beta.go"] {
		t.Fatalf("glob fallback missing files, got %v", got)
	}
}

func TestFileCompletion_ResetReglobs(t *testing.T) {
	newProjectDir(t, "one.go")
	p := NewFileAndFolderContextGroup(nil)
	async := p.(dialog.AsyncCompletionProvider)
	res := p.(dialog.ResettableCompletionProvider)

	p.GetChildEntries("")
	waitForGlob(t, p)
	if async.RefreshCmd() != nil {
		t.Fatal("expected nil RefreshCmd once the scan is ready")
	}

	// After Reset, a new scan must be scheduled on the next access.
	res.Reset()
	p.GetChildEntries("")
	if async.RefreshCmd() == nil {
		t.Fatal("expected Reset to re-trigger the background scan")
	}
}

func TestFileCompletion_IndexerErrorIsTolerated(t *testing.T) {
	newProjectDir(t, "x.go")
	lister := &fakeLister{lastErr: context.DeadlineExceeded}
	p := NewFileAndFolderContextGroup(lister)

	// A failing indexer must not break completion; preview is just empty.
	if items, err := p.GetChildEntries(""); err != nil {
		t.Fatalf("GetChildEntries should swallow indexer errors, got %v", err)
	} else if len(items) != 0 {
		t.Fatalf("expected empty preview on indexer error, got %d", len(items))
	}

	waitForGlob(t, p)
	if got := titles(mustEntries(t, p, "")); !got["x.go"] {
		t.Fatalf("glob results missing after indexer error, got %v", got)
	}
}

func mustEntries(t *testing.T, p dialog.CompletionProvider, q string) []dialog.CompletionItemI {
	t.Helper()
	items, err := p.GetChildEntries(q)
	if err != nil {
		t.Fatalf("GetChildEntries(%q): %v", q, err)
	}
	return items
}
