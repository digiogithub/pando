package store

import (
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// TestFileStoreSharedFileKeepsOtherProcessesTasks simulates the parent and a
// subagent process opening the same tasks.json: each save must keep the tasks
// the other wrote, and a stale copy of a task it does not own must never
// overwrite the owner's newer state.
func TestFileStoreSharedFileKeepsOtherProcessesTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")

	parent, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.Save(&models.Task{ID: "parent-task", Status: models.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := parent.ForceSave(); err != nil {
		t.Fatal(err)
	}

	// The subagent loads the parent's running task, then writes its own.
	child, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := child.Save(&models.Task{ID: "child-task", Status: models.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}

	// The parent finishes its task; the child then saves its stale copy view.
	if err := parent.UpdateStatus("parent-task", models.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := parent.ForceSave(); err != nil {
		t.Fatal(err)
	}
	if err := child.ForceSave(); err != nil {
		t.Fatal(err)
	}

	reader, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	got, err := reader.Get("parent-task")
	if err != nil {
		t.Fatalf("parent task lost: %v", err)
	}
	if got.Status != models.TaskStatusCompleted {
		t.Errorf("parent task status = %q, want completed", got.Status)
	}
	if _, err := reader.Get("child-task"); err != nil {
		t.Errorf("child task lost: %v", err)
	}
}

// TestFileStoreDeleteSurvivesMerge: a task deleted by this process is not
// brought back by the merge with the file it was read from.
func TestFileStoreDeleteSurvivesMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if err := fs.Save(&models.Task{ID: "gone"}); err != nil {
		t.Fatal(err)
	}
	if err := fs.ForceSave(); err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete("gone"); err != nil {
		t.Fatal(err)
	}
	if err := fs.ForceSave(); err != nil {
		t.Fatal(err)
	}

	reader, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.Get("gone"); err == nil {
		t.Error("deleted task came back after save")
	}
}
