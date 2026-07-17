package filetree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadFileTreeExpandedKeepsOpenDirectoriesAndSeesNewFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}

	expanded := map[string]bool{"pkg": true, "pkg/inner": true}
	msg := LoadFileTreeExpanded(root, LoadOptions{}, expanded, nil)()

	refresh, ok := msg.(FileTreeRefreshMsg)
	if !ok {
		t.Fatalf("expected FileTreeRefreshMsg, got %T", msg)
	}
	if refresh.Err != nil {
		t.Fatalf("refresh error: %v", refresh.Err)
	}

	pkg := findChild(refresh.Root, "pkg")
	if pkg == nil {
		t.Fatal("pkg directory missing from the reloaded tree")
	}
	if !pkg.IsExpanded || !pkg.Loaded {
		t.Errorf("pkg should stay expanded and loaded after a reload, got expanded=%v loaded=%v", pkg.IsExpanded, pkg.Loaded)
	}
	if findChild(pkg, "a.go") == nil {
		t.Error("a.go missing from the expanded pkg directory")
	}
	inner := findChild(pkg, "inner")
	if inner == nil || !inner.IsExpanded {
		t.Error("nested expanded directory was not restored")
	}

	// A file created after the first load must appear on the next refresh.
	if err := os.WriteFile(filepath.Join(root, "pkg", "b.go"), []byte("package b"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg = LoadFileTreeExpanded(root, LoadOptions{}, expanded, nil)()
	refresh = msg.(FileTreeRefreshMsg)
	if findChild(findChild(refresh.Root, "pkg"), "b.go") == nil {
		t.Error("newly created file did not show up in the refreshed tree")
	}
}

// The tree must work in directories that are not git repositories and on
// machines with no git binary at all: git only decorates the listing.
func TestLoadFileTreeWithoutGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", filepath.Join("pkg", "b.txt")} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// An empty PATH removes the git binary from reach.
	t.Setenv("PATH", t.TempDir())

	msg := LoadFileTreeExpanded(root, LoadOptions{}, map[string]bool{"pkg": true}, nil)()
	refresh, ok := msg.(FileTreeRefreshMsg)
	if !ok {
		t.Fatalf("expected FileTreeRefreshMsg, got %T", msg)
	}
	if refresh.Err != nil {
		t.Fatalf("tree failed to load without git: %v", refresh.Err)
	}
	if findChild(refresh.Root, "a.txt") == nil {
		t.Error("a.txt missing from a tree loaded without git")
	}
	if findChild(findChild(refresh.Root, "pkg"), "b.txt") == nil {
		t.Error("pkg/b.txt missing from a tree loaded without git")
	}

	statuses := loadGitStatuses(root)
	if len(statuses) != 0 {
		t.Errorf("expected no git statuses without git, got %v", statuses)
	}
}

// A symlinked directory cannot be handed to "git check-ignore" with a trailing
// slash: git answers "pathspec is beyond a symbolic link" and aborts the whole
// batch, which used to leave every candidate listed after it unchecked (and so
// wrongly visible) while the ones before it were hidden — the tree depended on
// listing order. .gitignore must be applied to symlinks like to anything else.
func TestReadDirectoryAppliesGitignoreToSymlinksRegardlessOfOrder(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable or failing (%v): %s", err, out)
		}
	}
	runGit("init")

	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, "code.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "a_ignored" sorts before "z_kept": with the old batch-aborting behavior the
	// fatal on the first symlink hid it and left the second one visible.
	for _, name := range []string{"a_ignored", "z_kept"} {
		if err := os.Symlink(linked, filepath.Join(repo, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "kept.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("a_ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	nodes, err := readDirectory(repo, ".", 1, LoadOptions{}, nil)
	if err != nil {
		t.Fatalf("readDirectory: %v", err)
	}

	listed := map[string]bool{}
	for _, node := range nodes {
		listed[node.Name] = true
	}
	if listed["a_ignored"] {
		t.Error("a gitignored symlink must be hidden like any other ignored entry")
	}
	if !listed["z_kept"] {
		t.Error("a symlink that is not ignored must stay visible: an earlier symlink aborted the check-ignore batch")
	}
	if !listed["kept.txt"] {
		t.Error("kept.txt disappeared: the check-ignore batch was corrupted")
	}
}

func TestReadDirectoryTreatsSymlinkedDirectoryAsDirectory(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	proj := filepath.Join(root, "proj")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "note.txt"), []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(proj, "linkdir")); err != nil {
		t.Fatal(err)
	}

	nodes, err := readDirectory(proj, ".", 1, LoadOptions{}, nil)
	if err != nil {
		t.Fatalf("readDirectory: %v", err)
	}

	var link *FileNode
	for _, node := range nodes {
		if node.Name == "linkdir" {
			link = node
		}
	}
	if link == nil {
		t.Fatal("linkdir missing from the listing")
	}
	if !link.IsDir {
		t.Error("a symlink pointing at a directory should be listed as a directory")
	}

	children, err := readDirectory(proj, "linkdir", 2, LoadOptions{}, nil)
	if err != nil {
		t.Fatalf("readDirectory through symlink: %v", err)
	}
	if len(children) != 1 || children[0].Name != "note.txt" {
		t.Errorf("expected note.txt through the symlinked directory, got %v", children)
	}
}

func findChild(parent *FileNode, name string) *FileNode {
	if parent == nil {
		return nil
	}
	for _, child := range parent.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}
