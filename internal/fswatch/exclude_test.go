package fswatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	root := t.TempDir()
	e := NewExcluder(root, nil)
	for _, n := range []string{"DerivedData", "Pods", "node_modules", ".git", "App.xcarchive", "x.dSYM", "build"} {
		if !e.ShouldSkipDir(filepath.Join(root, "a", n)) {
			t.Errorf("%s should be skipped", n)
		}
	}
	if e.ShouldSkipDir(filepath.Join(root, "src")) {
		t.Error("src should not be skipped")
	}
	if e.ShouldSkipDir(root) {
		t.Error("root must never be skipped")
	}
	if len(DefaultExcludedDirs()) == 0 {
		t.Error("empty defaults")
	}
}

func TestRootDotDirNotSkipped(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".hidden", "build")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if NewExcluder(root, nil).ShouldSkipDir(root) {
		t.Error("root must never be skipped")
	}
}

func TestGitignoreRootAndNested(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, ".gitignore"), "gen-out/\n")
	write(filepath.Join(root, "pkg", ".gitignore"), "local-cache\n")
	e := NewExcluder(root, nil)
	if !e.ShouldSkipDir(filepath.Join(root, "gen-out")) {
		t.Error("root gitignore dir should be skipped")
	}
	if e.ShouldSkipDir(filepath.Join(root, "pkg")) {
		t.Fatal("pkg should not be skipped")
	}
	if !e.ShouldSkipDir(filepath.Join(root, "pkg", "local-cache")) {
		t.Error("nested gitignore dir should be skipped")
	}
	if e.ShouldSkipDir(filepath.Join(root, "other", "local-cache")) {
		t.Error("nested gitignore must not leak to siblings")
	}
}

func TestWatchExclude(t *testing.T) {
	root := t.TempDir()
	e := NewExcluder(root, []string{"mobile-app/www/svg", "**/generated", "tmpdir/"})
	cases := map[string]bool{
		"mobile-app/www/svg":     true,
		"mobile-app/www/css":     false,
		"a/b/generated":          true,
		"generated":              true,
		"tmpdir":                 true,
		"x/tmpdir":               true,
		"mobile-app/www/svg/sub": false, // parent skip prevents descent; exact match only
	}
	for rel, want := range cases {
		if got := e.ShouldSkipDir(filepath.Join(root, filepath.FromSlash(rel))); got != want {
			t.Errorf("%s: got %v want %v", rel, got, want)
		}
	}
}

func TestAncestorIgnoreOutsideRepoIsIgnored(t *testing.T) {
	home := t.TempDir()
	// A dotfiles-style ignore-everything file above the project, with no .git
	// between it and the project: it must not hide the workspace.
	if err := os.WriteFile(filepath.Join(home, ".gitignore"), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "proj")
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if NewExcluder(root, nil).ShouldSkipDir(src) {
		t.Fatalf("ancestor .gitignore outside the repo must not apply")
	}
}

func TestAncestorIgnoreInsideRepoApplies(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("generated/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repo, "app")
	gen := filepath.Join(root, "generated")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	if !NewExcluder(root, nil).ShouldSkipDir(gen) {
		t.Fatalf("repo-root .gitignore must apply to a subfolder workspace")
	}
}
