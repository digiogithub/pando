// Package agentvcs implements a minimal jj-inspired version control system
// for tracking file changes across agent sessions. Each session produces a
// linear chain of immutable commits, enabling per-session changelogs and
// the ability to review or revert incremental work.
package agentvcs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Commit is an immutable point-in-time snapshot of the working directory.
// Commits form a singly-linked list via ParentID within a session.
type Commit struct {
	ID               string `json:"id"`                           // Content-derived hash
	ParentID         string `json:"parent_id"`                    // Previous commit (empty for root)
	SessionID        string `json:"session_id"`                   // Owning agent session
	TreeID           string `json:"tree_id"`                      // Hash of the Tree object
	Description      string `json:"description"`                  // Human-readable label
	Author           string `json:"author"`                       // "agent" or user identifier
	CreatedAt        int64  `json:"created_at"`                   // Unix timestamp
	FileCount        int    `json:"file_count"`                   // Number of tracked files in the resulting tree
	TotalSize        int64  `json:"total_size"`                   // Sum of file sizes in the resulting tree
	ChangedFileCount int    `json:"changed_file_count,omitempty"` // Files changed vs parent for this commit
	ChangedTotalSize int64  `json:"changed_total_size,omitempty"` // Aggregate size of changed files for this commit
}

// Tree represents the full file listing for a commit. It is stored
// separately so that identical trees across commits are deduplicated.
type Tree struct {
	ID      string      `json:"id"`      // SHA-256 of the serialised entries
	Entries []TreeEntry `json:"entries"` // Sorted list of tracked files
}

// TreeEntry is a single file (or directory marker) inside a Tree.
type TreeEntry struct {
	Path    string `json:"path"`             // Slash-separated relative path
	Hash    string `json:"hash,omitempty"`   // SHA-256 of file content
	Size    int64  `json:"size,omitempty"`   // File size in bytes
	ModTime int64  `json:"mod_time"`         // Last modification (Unix)
	IsDir   bool   `json:"is_dir,omitempty"` // Directory marker
}

// SessionLog tracks the ordered list of commit IDs for one session.
type SessionLog struct {
	SessionID string   `json:"session_id"`
	Commits   []string `json:"commits"` // Oldest first
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

// DiffType describes a change between two commits.
type DiffType string

const (
	DiffAdded    DiffType = "added"
	DiffModified DiffType = "modified"
	DiffDeleted  DiffType = "deleted"
)

// DiffEntry represents a single file change between two commits.
type DiffEntry struct {
	Path    string   `json:"path"`
	Type    DiffType `json:"type"`
	OldHash string   `json:"old_hash,omitempty"`
	NewHash string   `json:"new_hash,omitempty"`
	OldSize int64    `json:"old_size,omitempty"`
	NewSize int64    `json:"new_size,omitempty"`
}

// CommitSummary is a lightweight view of a commit used in log listings.
type CommitSummary struct {
	ID               string `json:"id"`
	ShortID          string `json:"short_id"` // First 12 hex chars
	ParentID         string `json:"parent_id,omitempty"`
	SessionID        string `json:"session_id"`
	Description      string `json:"description"`
	FileCount        int    `json:"file_count"`
	TotalSize        int64  `json:"total_size"`
	CreatedAt        int64  `json:"created_at"`
	ChangedFiles     int    `json:"changed_files"`      // Files changed vs parent
	ChangedTotalSize int64  `json:"changed_total_size"` // Aggregate size of changed files
}

// computeCommitID derives a deterministic ID from the commit's content fields.
func computeCommitID(parentID, treeID, sessionID, description string, createdAt int64) string {
	payload := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", parentID, treeID, sessionID, description, createdAt)
	h := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(h[:])
}

// computeTreeID derives a deterministic ID from the tree entries.
func computeTreeID(entries []TreeEntry) (string, error) {
	data, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("agentvcs: marshal tree entries: %w", err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// shortID returns the first 12 characters of a hex ID.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// toSummary converts a Commit to a lightweight CommitSummary.
func (c *Commit) toSummary() CommitSummary {
	return CommitSummary{
		ID:               c.ID,
		ShortID:          shortID(c.ID),
		ParentID:         c.ParentID,
		SessionID:        c.SessionID,
		Description:      c.Description,
		FileCount:        c.FileCount,
		TotalSize:        c.TotalSize,
		CreatedAt:        c.CreatedAt,
		ChangedFiles:     c.ChangedFileCount,
		ChangedTotalSize: c.ChangedTotalSize,
	}
}

// nowUnix returns the current Unix timestamp.
func nowUnix() int64 { return time.Now().Unix() }
