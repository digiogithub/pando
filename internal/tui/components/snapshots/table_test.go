package snapshots

import "testing"

func TestSnapshotListSelectsFirstRow(t *testing.T) {
	c := NewSnapshotsTable().(*tableCmp)
	c.SetSize(80, 10)

	rows := []SnapshotRow{
		{ID: "snap-1", SessionID: "s1", Type: "manual", FileCount: 3, TotalSize: 128, CreatedAt: 10},
		{ID: "snap-2", SessionID: "s2", Type: "end", FileCount: 5, TotalSize: 256, CreatedAt: 20},
	}

	_, cmd := c.Update(SnapshotListMsg{Snapshots: rows})
	if len(c.rows) != len(rows) {
		t.Fatalf("rows = %d, want %d", len(c.rows), len(rows))
	}

	msg := cmd()
	selected, ok := msg.(SelectedSnapshotMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want SelectedSnapshotMsg", msg)
	}
	if selected.ID != "snap-2" {
		t.Fatalf("selected ID = %q, want %q", selected.ID, "snap-2")
	}
}

func TestEmptySnapshotListClearsSelection(t *testing.T) {
	c := NewSnapshotsTable().(*tableCmp)
	c.SetSize(80, 10)

	_, cmd := c.Update(SnapshotListMsg{})
	msg := cmd()
	selected, ok := msg.(SelectedSnapshotMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want SelectedSnapshotMsg", msg)
	}
	if selected.ID != "" {
		t.Fatalf("selected ID = %q, want empty", selected.ID)
	}
}
