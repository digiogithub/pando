package search

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSearchFilesFollowsSymlinkedDirectories covers the grep/glob path: a
// directory reachable only through a symlink must still be searched.
func TestSearchFilesFollowsSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	proj := filepath.Join(root, "proj")

	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "linked.go"), []byte("package needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(proj, "linkdir")); err != nil {
		t.Fatal(err)
	}

	matches, _, err := SearchFiles(context.Background(), WalkOptions{
		RootPath: proj,
		Pattern:  regexp.MustCompile("needle"),
		SearchOpts: SearchOptions{
			Pattern:    regexp.MustCompile("needle"),
			OutputMode: OutputModeContent,
		},
	})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no match found through the symlinked directory")
	}
	if !strings.Contains(matches[0].Path, "linkdir") {
		t.Errorf("match reported under the resolved path %q, expected the logical linkdir path", matches[0].Path)
	}
}
