package snapshots

// SelectedSnapshotMsg is published when the user selects a row in the snapshot table.
type SelectedSnapshotMsg struct {
	ID          string
	SessionID   string
	Type        string
	Description string
	WorkingDir  string
	FileCount   int
	TotalSize   int64
	CreatedAt   int64
}

// SnapshotRow is a lightweight representation of a snapshot used for table rendering.
type SnapshotRow struct {
	ID           string
	SessionID    string
	SessionTitle string
	Type         string
	Description  string
	WorkingDir   string
	FileCount    int
	TotalSize    int64
	CreatedAt    int64
}

// SnapshotListMsg is sent to populate the table with a fresh snapshot list.
type SnapshotListMsg struct {
	Snapshots []SnapshotRow
}

// RevertSnapshotMsg requests that the given snapshot be reverted to disk.
type RevertSnapshotMsg struct {
	SnapshotID string
}

// DeleteSnapshotMsg requests that the given snapshot be deleted.
type DeleteSnapshotMsg struct {
	SnapshotID string
}
