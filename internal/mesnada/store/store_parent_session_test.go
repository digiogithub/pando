package store

import (
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// TestListByParentSession verifies filtering tasks by parent session id, both
// via the dedicated helper and via ListFilter.ParentSessionID.
func TestListByParentSession(t *testing.T) {
	fs, err := NewFileStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })

	tasks := []*models.Task{
		{ID: "a", Status: models.TaskStatusCompleted, ParentSessionID: "sess-1"},
		{ID: "b", Status: models.TaskStatusRunning, ParentSessionID: "sess-1"},
		{ID: "c", Status: models.TaskStatusPending, ParentSessionID: "sess-2"},
		{ID: "d", Status: models.TaskStatusPending}, // no parent session
	}
	for _, tk := range tasks {
		if err := fs.Save(tk); err != nil {
			t.Fatalf("save %s: %v", tk.ID, err)
		}
	}

	got, err := fs.ListByParentSession("sess-1")
	if err != nil {
		t.Fatalf("ListByParentSession: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByParentSession(sess-1) = %d tasks, want 2", len(got))
	}
	for _, tk := range got {
		if tk.ParentSessionID != "sess-1" {
			t.Errorf("got task %s with parent %q, want sess-1", tk.ID, tk.ParentSessionID)
		}
	}

	// Same result via ListFilter.
	viaFilter, err := fs.List(ListFilter{ParentSessionID: "sess-2"})
	if err != nil {
		t.Fatalf("List filter: %v", err)
	}
	if len(viaFilter) != 1 || viaFilter[0].ID != "c" {
		t.Fatalf("List(ParentSessionID=sess-2) = %+v, want [c]", viaFilter)
	}

	// Empty filter must not filter on parent session (returns all).
	all, err := fs.List(ListFilter{})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("List(empty) = %d tasks, want 4", len(all))
	}

	// Unknown parent session yields nothing.
	none, err := fs.ListByParentSession("missing")
	if err != nil {
		t.Fatalf("ListByParentSession(missing): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("ListByParentSession(missing) = %d, want 0", len(none))
	}
}
