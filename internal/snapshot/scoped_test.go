package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/pubsub"
)

// newTestService builds a snapshot service backed by a temporary blob store,
// bypassing the config-dependent constructor.
func newTestService(t *testing.T) *service {
	t.Helper()
	stor, err := newStorage(t.TempDir())
	if err != nil {
		t.Fatalf("newStorage: %v", err)
	}
	return &service{
		Broker:  pubsub.NewBroker[Snapshot](),
		storage: stor,
		scanner: newScanner(),
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestCreateScopedOnlyCapturesSubtree proves a scoped snapshot ignores every
// file outside its root, which is what keeps design versioning from swallowing
// the whole repository.
func TestCreateScopedOnlyCapturesSubtree(t *testing.T) {
	svc := newTestService(t)
	project := t.TempDir()
	scoped := filepath.Join(project, "designer", "landing")

	writeTestFile(t, filepath.Join(project, "main.go"), "package main\n")
	writeTestFile(t, filepath.Join(scoped, "index.html"), "<h1>v1</h1>")
	writeTestFile(t, filepath.Join(scoped, "assets", "app.css"), "body{}")

	snap, err := svc.CreateScoped(context.Background(), "session-1", "v1", scoped)
	if err != nil {
		t.Fatalf("CreateScoped: %v", err)
	}
	if snap.Type != SnapshotTypeScoped {
		t.Fatalf("type = %q, want %q", snap.Type, SnapshotTypeScoped)
	}

	manifest, err := svc.GetManifest(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	for _, f := range manifest.Files {
		if f.Path == "main.go" || filepath.IsAbs(f.Path) {
			t.Fatalf("scoped snapshot captured out-of-scope path %q", f.Path)
		}
	}

	paths := map[string]bool{}
	for _, f := range manifest.Files {
		if !f.IsDir {
			paths[f.Path] = true
		}
	}
	for _, want := range []string{"index.html", "assets/app.css"} {
		if !paths[want] {
			t.Fatalf("scoped snapshot missing %q, got %v", want, paths)
		}
	}
}

// TestRevertScopedLeavesOutsideFilesAlone is the regression guard for the
// design checkout contract: restoring an old artifact version must not revert
// unrelated work.
func TestRevertScopedLeavesOutsideFilesAlone(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	project := t.TempDir()
	scoped := filepath.Join(project, "designer", "landing")

	outside := filepath.Join(project, "main.go")
	writeTestFile(t, outside, "package main // v1\n")
	writeTestFile(t, filepath.Join(scoped, "index.html"), "<h1>v1</h1>")

	snap, err := svc.CreateScoped(ctx, "session-1", "v1", scoped)
	if err != nil {
		t.Fatalf("CreateScoped: %v", err)
	}

	// Move both the artifact and an unrelated file forward.
	writeTestFile(t, filepath.Join(scoped, "index.html"), "<h1>v2</h1>")
	writeTestFile(t, filepath.Join(scoped, "extra.html"), "<p>added later</p>")
	writeTestFile(t, outside, "package main // v2\n")

	if err := svc.RevertScoped(ctx, snap.ID); err != nil {
		t.Fatalf("RevertScoped: %v", err)
	}

	if got := readTestFile(t, filepath.Join(scoped, "index.html")); got != "<h1>v1</h1>" {
		t.Fatalf("artifact not restored: %q", got)
	}
	if _, err := os.Stat(filepath.Join(scoped, "extra.html")); !os.IsNotExist(err) {
		t.Fatalf("file added after the snapshot survived the scoped revert (err=%v)", err)
	}
	if got := readTestFile(t, outside); got != "package main // v2\n" {
		t.Fatalf("scoped revert touched an out-of-scope file: %q", got)
	}
}

// TestRevertScopedRejectsFullSnapshots keeps the two snapshot flavours from
// being mixed: a whole-working-directory snapshot must not be revertible
// through the scoped path.
func TestRevertScopedRejectsFullSnapshots(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	project := t.TempDir()
	writeTestFile(t, filepath.Join(project, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeTestFile(t, filepath.Join(project, "main.go"), "package main\n")

	t.Chdir(project)

	snap, err := svc.Create(ctx, "session-1", SnapshotTypeManual, "full")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.RevertScoped(ctx, snap.ID); err == nil {
		t.Fatal("RevertScoped accepted a full snapshot")
	}
}
