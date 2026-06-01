package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestIsSafeWorkingDirectory_HomeUnsafe verifies that the home directory is
// never considered safe regardless of OS.
func TestIsSafeWorkingDirectory_HomeUnsafe(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory:", err)
	}
	if IsSafeWorkingDirectory(home) {
		t.Errorf("home directory %q should not be safe", home)
	}
}

// TestIsSafeWorkingDirectory_HomeParentUnsafe verifies that parent directories
// of the home directory are also considered unsafe.
func TestIsSafeWorkingDirectory_HomeParentUnsafe(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory:", err)
	}
	parent := filepath.Dir(home)
	if parent == home {
		t.Skip("home directory has no parent (root?)")
	}
	if IsSafeWorkingDirectory(parent) {
		t.Errorf("parent of home %q should not be safe", parent)
	}
}

// TestIsSafeWorkingDirectory_RootUnsafe verifies that filesystem roots are
// unsafe on all platforms.
func TestIsSafeWorkingDirectory_RootUnsafe(t *testing.T) {
	roots := []string{"/"}
	if runtime.GOOS == "windows" {
		roots = []string{`C:\`, `D:\`}
	}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue // root doesn't exist on this machine, skip
		}
		if IsSafeWorkingDirectory(root) {
			t.Errorf("filesystem root %q should not be safe", root)
		}
	}
}

// TestIsSafeWorkingDirectory_ProjectSafe verifies that a directory containing
// a project marker is considered safe.
func TestIsSafeWorkingDirectory_ProjectSafe(t *testing.T) {
	// Create a temp dir that is NOT inside home (use os.TempDir which is
	// typically /tmp on Unix or %TEMP% on Windows, outside the home tree).
	tmp, err := os.MkdirTemp("", "pando-safe-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	// Without any marker → not safe.
	if IsSafeWorkingDirectory(tmp) {
		// It might still be inside home on some CI environments; skip rather than fail.
		home, _ := os.UserHomeDir()
		if isAncestorOrEqual(tmp, home) {
			t.Skipf("temp dir %q is inside home %q; skipping", tmp, home)
		}
		t.Errorf("directory without marker %q should not be safe", tmp)
	}

	// Add a .git marker → should become safe.
	gitDir := filepath.Join(tmp, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Also place it outside home if needed (already is, tmp is /tmp/…).
	if !IsSafeWorkingDirectory(tmp) {
		home, _ := os.UserHomeDir()
		if isAncestorOrEqual(tmp, home) {
			t.Skipf("temp dir %q is inside home %q; skipping", tmp, home)
		}
		t.Errorf("directory with .git marker %q should be safe", tmp)
	}
}

// TestIFSRoot covers isFSRoot for both Unix and Windows roots.
func TestIFSRoot(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/home", false},
		{"/home/user", false},
	}
	if runtime.GOOS == "windows" {
		cases = []struct {
			path string
			want bool
		}{
			{`C:\`, true},
			{`D:\`, true},
			{`C:\Users`, false},
			{`C:\Users\someone`, false},
		}
	}
	for _, c := range cases {
		got := isFSRoot(c.path)
		if got != c.want {
			t.Errorf("isFSRoot(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// isAncestorOrEqual is a helper used only in tests.
func isAncestorOrEqual(child, ancestor string) bool {
	child = filepath.Clean(child)
	ancestor = filepath.Clean(ancestor)
	current := child
	for {
		if sameDir(current, ancestor) {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}
