package kb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteDocumentToFilesystemCreatesFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	store := &KBStore{}
	if err := store.ConfigureFilesystemMirror(tmp); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}

	docPath := "docs/guide.md"
	content := "# hello\n"
	if err := store.WriteDocumentToFilesystem(docPath, content); err != nil {
		t.Fatalf("WriteDocumentToFilesystem() error = %v", err)
	}

	full := filepath.Join(tmp, "docs", "guide.md")
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", full, err)
	}
	if string(got) != content {
		t.Fatalf("mirrored file content mismatch: got %q want %q", string(got), content)
	}
}

func TestWriteDocumentToFilesystemRejectsTraversal(t *testing.T) {
	t.Parallel()

	store := &KBStore{}
	if err := store.ConfigureFilesystemMirror(t.TempDir()); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}

	if err := store.WriteDocumentToFilesystem("../escape.md", "x"); err == nil {
		t.Fatalf("expected path traversal error, got nil")
	}
}

func TestDeleteDocumentFromFilesystemIgnoresMissing(t *testing.T) {
	t.Parallel()

	store := &KBStore{}
	if err := store.ConfigureFilesystemMirror(t.TempDir()); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}

	if err := store.DeleteDocumentFromFilesystem("missing.md"); err != nil {
		t.Fatalf("DeleteDocumentFromFilesystem() error = %v", err)
	}
}
