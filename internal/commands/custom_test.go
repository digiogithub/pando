package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestContentReadsProjectCommand(t *testing.T) {
	dataDir := t.TempDir()
	writeCommand(t, filepath.Join(dataDir, "commands"), "review.md", "Review the diff.\n")

	body, ok := Content(dataDir, "project:review")
	if !ok {
		t.Fatal("project command not found")
	}
	if body != "Review the diff.\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestContentReadsNestedCommand(t *testing.T) {
	dataDir := t.TempDir()
	// AllCommands maps nested files to "dir:file" ids; Content must map back.
	writeCommand(t, filepath.Join(dataDir, "commands"), filepath.Join("go", "lint.md"), "Run the linter.\n")

	body, ok := Content(dataDir, "project:go:lint")
	if !ok {
		t.Fatal("nested project command not found")
	}
	if body != "Run the linter.\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestContentRejectsNonCustomNames(t *testing.T) {
	if _, ok := Content(t.TempDir(), "caveman"); ok {
		t.Fatal("a built-in command name must not resolve to a file")
	}
}

func TestContentRejectsPathTraversal(t *testing.T) {
	dataDir := t.TempDir()
	writeCommand(t, dataDir, "secret.md", "top secret\n")
	writeCommand(t, filepath.Join(dataDir, "commands"), "ok.md", "fine\n")

	// "project:..:secret" would escape <dataDir>/commands if ids were trusted.
	if _, ok := Content(dataDir, "project:..:secret"); ok {
		t.Fatal("command ids must not escape the commands directory")
	}
}

func TestContentMissingFile(t *testing.T) {
	if _, ok := Content(t.TempDir(), "project:nope"); ok {
		t.Fatal("a missing command file must report false")
	}
}

func TestAllCommandsIncludesProjectCommands(t *testing.T) {
	dataDir := t.TempDir()
	writeCommand(t, filepath.Join(dataDir, "commands"), "review.md", "body")

	var found bool
	for _, cmd := range AllCommands(dataDir) {
		if cmd.Name == "project:review" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("AllCommands did not list the project command")
	}
}
