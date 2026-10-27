package client

import (
	"context"
	"fmt"
)

// AgentVCS types mirror web-ui/packages/pando-client/src/stores/agentVcsStore.ts.

// VCSSession is a session that has agent-vcs commits.
type VCSSession struct {
	SessionID   string `json:"session_id"`
	CommitCount int    `json:"commit_count"`
}

// VCSCommit is a commit of the session log.
type VCSCommit struct {
	ID               string `json:"id"`
	ShortID          string `json:"short_id"`
	ParentID         string `json:"parent_id"`
	SessionID        string `json:"session_id"`
	Description      string `json:"description"`
	FileCount        int    `json:"file_count"`
	TotalSize        int64  `json:"total_size"`
	ChangedFiles     int    `json:"changed_files"`
	ChangedTotalSize int64  `json:"changed_total_size"`
	IsBaseline       bool   `json:"is_baseline"`
	CreatedAt        string `json:"created_at"`
}

// VCSDiffEntry is one changed file of a commit.
type VCSDiffEntry struct {
	Path    string `json:"path"`
	Type    string `json:"type"` // added | modified | deleted
	OldHash string `json:"old_hash"`
	NewHash string `json:"new_hash"`
	OldSize int64  `json:"old_size"`
	NewSize int64  `json:"new_size"`
}

// VCSCommitInfo is the header of a commit detail.
type VCSCommitInfo struct {
	ID         string `json:"id"`
	ShortID    string `json:"short_id"`
	Name       string `json:"name"`
	SessionID  string `json:"session_id"`
	ParentID   string `json:"parent_id"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	Size       int64  `json:"size"`
	FilesCount int    `json:"files_count"`
}

// VCSCommitDetail is GET /api/v1/agentvcs/commits/{id}.
type VCSCommitDetail struct {
	Commit VCSCommitInfo  `json:"commit"`
	Diff   []VCSDiffEntry `json:"diff"`
}

// VCSSessions lists sessions with commits.
func (c *Client) VCSSessions(ctx context.Context) ([]VCSSession, error) {
	var out struct {
		Sessions []VCSSession `json:"sessions"`
	}
	err := c.Get(ctx, "/api/v1/agentvcs/sessions", &out)
	return out.Sessions, err
}

// VCSLog lists the commits of a session, oldest first.
func (c *Client) VCSLog(ctx context.Context, sessionID string) ([]VCSCommit, error) {
	var out struct {
		Commits []VCSCommit `json:"commits"`
	}
	err := c.Get(ctx, "/api/v1/agentvcs/sessions/"+pathEscape(sessionID)+"/log", &out)
	return out.Commits, err
}

// VCSCommitDetail returns a commit with its changed files.
func (c *Client) VCSCommitDetail(ctx context.Context, commitID string) (VCSCommitDetail, error) {
	var out VCSCommitDetail
	err := c.Get(ctx, "/api/v1/agentvcs/commits/"+pathEscape(commitID), &out)
	return out, err
}

// VCSBlob returns the content of a stored file version.
func (c *Client) VCSBlob(ctx context.Context, hash string) (string, error) {
	b, err := c.ReadRaw(ctx, "/api/v1/agentvcs/blobs/"+pathEscape(hash))
	return string(b), err
}

// VCSRevert restores the whole working directory to a commit.
func (c *Client) VCSRevert(ctx context.Context, commitID string) error {
	return c.Post(ctx, "/api/v1/agentvcs/commits/"+pathEscape(commitID)+"/revert", map[string]any{}, nil)
}

// VCSRevertFiles restores some files to their state in a commit and returns
// those restored.
func (c *Client) VCSRevertFiles(ctx context.Context, commitID string, files []string) ([]string, error) {
	var out struct {
		Restored []string `json:"restored"`
	}
	err := c.Post(ctx, "/api/v1/agentvcs/commits/"+pathEscape(commitID)+"/revert-files", map[string]any{"files": files}, &out)
	return out.Restored, err
}

// Snapshot is a named project snapshot (GET /api/v1/snapshots).
type Snapshot struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SessionID  string `json:"session_id"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	Size       int64  `json:"size"`
	FilesCount int    `json:"files_count"`
}

// Snapshots lists the snapshots.
func (c *Client) Snapshots(ctx context.Context) ([]Snapshot, error) {
	var out struct {
		Snapshots []Snapshot `json:"snapshots"`
	}
	err := c.Get(ctx, "/api/v1/snapshots", &out)
	return out.Snapshots, err
}

// CreateSnapshot creates a snapshot called name.
func (c *Client) CreateSnapshot(ctx context.Context, name string) error {
	return c.Post(ctx, "/api/v1/snapshots", map[string]string{"name": name}, nil)
}

// SnapshotAction runs "apply" or "revert" on a snapshot.
func (c *Client) SnapshotAction(ctx context.Context, id, action string) error {
	if action != "apply" && action != "revert" {
		return fmt.Errorf("pando: unknown snapshot action %q", action)
	}
	return c.Post(ctx, "/api/v1/snapshots/"+pathEscape(id)+"/"+action, map[string]any{}, nil)
}

// DeleteSnapshot removes a snapshot.
func (c *Client) DeleteSnapshot(ctx context.Context, id string) error {
	return c.Delete(ctx, "/api/v1/snapshots/"+pathEscape(id), nil)
}
