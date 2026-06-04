package snapshots

import "github.com/digiogithub/pando/internal/agentvcs"

// SessionRow is a lightweight representation of a session used for table rendering.
type SessionRow struct {
	SessionID    string
	CommitCount  int
	LatestAt     int64
	LatestCommit string
	Description  string
}

// SessionListMsg is sent to populate the session table.
type SessionListMsg struct {
	Sessions []SessionRow
}

// SelectedSessionMsg is published when the user selects a session row.
type SelectedSessionMsg struct {
	SessionID string
}

// CommitRow is a lightweight representation of a commit used for table rendering.
type CommitRow struct {
	ID          string
	ShortID     string
	ParentID    string
	SessionID   string
	Type        string
	Description string
	FileCount   int
	TotalSize   int64
	CreatedAt   int64
}

// CommitListMsg is sent to populate the commit table for a selected session.
type CommitListMsg struct {
	SessionID string
	Commits   []CommitRow
}

// SelectedCommitMsg is published when the user selects a commit row.
type SelectedCommitMsg struct {
	Commit CommitRow
}

// CommitDetailsMsg carries full commit details and diff for rendering in the detail panel.
type CommitDetailsMsg struct {
	Commit agentvcs.Commit
	Diff   []agentvcs.DiffEntry
	Err    error
}

// RevertCommitMsg requests that the given commit be reverted to disk.
type RevertCommitMsg struct {
	CommitID string
}
