package fileutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// buildSymlinkTree lays out:
//
//	real/note.txt
//	real/sub/target.go
//	real/loopback -> proj      (closes a cycle through proj/linkdir)
//	proj/linkdir -> real
//	proj/linkfile.txt -> real/note.txt
//	proj/broken -> nowhere
//
// and returns the proj directory.
func buildSymlinkTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	real := filepath.Join(root, "real")
	proj := filepath.Join(root, "proj")

	if err := os.MkdirAll(filepath.Join(real, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "note.txt"), []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "sub", "target.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(proj, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, "note.txt"), filepath.Join(proj, "linkfile.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(proj, filepath.Join(real, "loopback")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(proj, "broken")); err != nil {
		t.Fatal(err)
	}
	return proj
}

func collectWalk(t *testing.T, root string) []string {
	t.Helper()
	var got []string
	err := WalkFollowSymlinks(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			rel += "/"
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(got)
	return got
}

func TestWalkFollowSymlinksDescendsIntoLinkedDirectories(t *testing.T) {
	proj := buildSymlinkTree(t)
	got := collectWalk(t, proj)

	want := map[string]bool{
		"linkdir/":              true,
		"linkdir/note.txt":      true,
		"linkdir/sub/":          true,
		"linkdir/sub/target.go": true,
		"linkfile.txt":          true,
	}
	for path := range want {
		found := false
		for _, g := range got {
			if g == path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in walk output: %v", path, got)
		}
	}
}

func TestWalkFollowSymlinksStopsAtCycle(t *testing.T) {
	proj := buildSymlinkTree(t)
	got := collectWalk(t, proj)

	// The loop (proj -> linkdir -> real -> loopback -> proj) must be reported
	// once as an entry but never descended into a second time.
	for _, path := range got {
		if path == "linkdir/loopback/linkdir/" {
			t.Fatalf("walk descended through a symlink cycle: %v", got)
		}
	}
	if len(got) > 32 {
		t.Fatalf("walk produced %d entries, suspiciously deep: %v", len(got), got)
	}
}

func TestWalkFollowSymlinksLinkedRoot(t *testing.T) {
	proj := buildSymlinkTree(t)
	got := collectWalk(t, filepath.Join(proj, "linkdir"))

	for _, want := range []string{"note.txt", "sub/target.go"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("walking a symlinked root missed %q: %v", want, got)
		}
	}
}

func TestWalkFollowSymlinksBrokenLinkIsReportedNotFatal(t *testing.T) {
	proj := buildSymlinkTree(t)
	got := collectWalk(t, proj)

	found := false
	for _, g := range got {
		if g == "broken" {
			found = true
		}
	}
	if !found {
		t.Errorf("broken symlink not reported: %v", got)
	}
}

func TestIsDirEntryResolvesSymlinks(t *testing.T) {
	proj := buildSymlinkTree(t)
	entries, err := os.ReadDir(proj)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"linkdir":      true,
		"linkfile.txt": false,
		"broken":       false,
	}
	for _, entry := range entries {
		expected, ok := want[entry.Name()]
		if !ok {
			continue
		}
		if got := IsDirEntry(proj, entry); got != expected {
			t.Errorf("IsDirEntry(%q) = %v, want %v", entry.Name(), got, expected)
		}
	}
}
