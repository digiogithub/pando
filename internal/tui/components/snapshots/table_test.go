package snapshots

import "testing"

func TestSessionListSelectsFirstRow(t *testing.T) {
	c := NewSessionsTable().(*sessionsTableCmp)
	c.SetSize(80, 10)

	rows := []SessionRow{
		{SessionID: "s1", CommitCount: 3, LatestAt: 10, Description: "first"},
		{SessionID: "s2", CommitCount: 5, LatestAt: 20, Description: "second"},
	}

	_, cmd := c.Update(SessionListMsg{Sessions: rows})
	if len(c.rows) != len(rows) {
		t.Fatalf("rows = %d, want %d", len(c.rows), len(rows))
	}

	msg := cmd()
	selected, ok := msg.(SelectedSessionMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want SelectedSessionMsg", msg)
	}
	if selected.SessionID != "s2" {
		t.Fatalf("selected session = %q, want %q", selected.SessionID, "s2")
	}
}

func TestEmptySessionListClearsSelection(t *testing.T) {
	c := NewSessionsTable().(*sessionsTableCmp)
	c.SetSize(80, 10)

	_, cmd := c.Update(SessionListMsg{})
	msg := cmd()
	selected, ok := msg.(SelectedSessionMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want SelectedSessionMsg", msg)
	}
	if selected.SessionID != "" {
		t.Fatalf("selected session = %q, want empty", selected.SessionID)
	}
}

func TestCommitListSelectsFirstRow(t *testing.T) {
	c := NewCommitsTable().(*commitsTableCmp)
	c.SetSize(80, 10)

	rows := []CommitRow{
		{ID: "commit-1", ShortID: "commit-1", SessionID: "s1", Type: "delta", FileCount: 3, TotalSize: 128, CreatedAt: 10},
		{ID: "commit-2", ShortID: "commit-2", SessionID: "s1", Type: "delta", FileCount: 5, TotalSize: 256, CreatedAt: 20},
	}

	_, cmd := c.Update(CommitListMsg{SessionID: "s1", Commits: rows})
	if len(c.rows) != len(rows) {
		t.Fatalf("rows = %d, want %d", len(c.rows), len(rows))
	}

	msg := cmd()
	selected, ok := msg.(SelectedCommitMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want SelectedCommitMsg", msg)
	}
	if selected.Commit.ID != "commit-2" {
		t.Fatalf("selected ID = %q, want %q", selected.Commit.ID, "commit-2")
	}
}
