package agentvcs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
)

// Service defines the public API for agent-vcs.
type Service interface {
	pubsub.Suscriber[Commit]

	// NewChange creates the initial commit for a session, capturing the current
	// working directory state. Analogous to `jj new`.
	NewChange(ctx context.Context, sessionID, description string) (Commit, error)

	// Record snapshots the working directory and creates a new commit in the
	// session's chain if any files changed since the last commit. Returns the
	// new commit or the previous HEAD if nothing changed.
	Record(ctx context.Context, sessionID, description string) (Commit, error)

	// Log returns the ordered list of commits for a session (oldest first).
	Log(ctx context.Context, sessionID string) ([]CommitSummary, error)

	// GetCommit retrieves a single commit by ID.
	GetCommit(ctx context.Context, id string) (Commit, error)

	// GetTree retrieves a tree object by ID.
	GetTree(ctx context.Context, id string) (Tree, error)

	// Diff computes the file-level changes between two commits.
	Diff(ctx context.Context, commitID1, commitID2 string) ([]DiffEntry, error)

	// DiffFromParent computes the file-level changes between a commit and its parent.
	// The session baseline (root commit) has no parent and therefore no changes.
	DiffFromParent(ctx context.Context, commitID string) ([]DiffEntry, error)

	// SessionDiff computes the aggregate changes made during a session: the
	// difference between the session baseline (first commit) and its HEAD.
	SessionDiff(ctx context.Context, sessionID string) ([]DiffEntry, error)

	// GetFileContent retrieves blob content by hash.
	GetFileContent(ctx context.Context, hash string) ([]byte, error)

	// ListSessions returns all session IDs that have at least one commit.
	ListSessions(ctx context.Context) ([]string, error)

	// SessionCommitCount returns the number of commits in a session.
	SessionCommitCount(ctx context.Context, sessionID string) (int, error)

	// Revert restores the working directory to the state captured in a commit.
	Revert(ctx context.Context, commitID string) error

	// Cleanup removes old sessions and orphan blobs.
	Cleanup(ctx context.Context, maxAgeDays, maxSessions int) error
}

type service struct {
	*pubsub.Broker[Commit]
	storage *storage
	scanner *scanner
}

// NewService creates a new agent-vcs service. It stores data alongside the
// old snapshots directory under the configured data directory.
func NewService() (Service, error) {
	cfg := config.Get()
	dataDir := cfg.Data.Directory
	vcsDir := filepath.Join(dataDir, "agent-vcs")

	if err := os.MkdirAll(vcsDir, 0o755); err != nil {
		return nil, fmt.Errorf("agentvcs: create dir: %w", err)
	}

	stor, err := newStorage(vcsDir)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: init storage: %w", err)
	}

	maxFileSize := cfg.Snapshots.ParseMaxFileSize()
	excludePatterns := cfg.Snapshots.ExcludePatterns

	scan := newScanner(maxFileSize, excludePatterns)

	svc := &service{
		Broker:  pubsub.NewBroker[Commit](),
		storage: stor,
		scanner: scan,
	}

	logging.Debug("agent-vcs service initialised", "dir", vcsDir)
	return svc, nil
}

// NewChange captures the initial state of the working directory for a session.
func (s *service) NewChange(ctx context.Context, sessionID, description string) (Commit, error) {
	// Check if session already has commits.
	if s.storage.SessionLogExists(sessionID) {
		sl, err := s.storage.LoadSessionLog(sessionID)
		if err == nil && len(sl.Commits) > 0 {
			// Session already initialised — return the latest commit.
			return s.storage.LoadCommit(sl.Commits[len(sl.Commits)-1])
		}
	}

	return s.createCommit(ctx, sessionID, "", description)
}

// Record creates a new commit only if the working directory changed since the
// last commit in the session. This is the main entry point for incremental
// snapshots during the agent loop.
func (s *service) Record(ctx context.Context, sessionID, description string) (Commit, error) {
	sl, err := s.storage.LoadSessionLog(sessionID)
	if err != nil || len(sl.Commits) == 0 {
		// No previous commits — treat as NewChange.
		return s.NewChange(ctx, sessionID, description)
	}

	lastCommitID := sl.Commits[len(sl.Commits)-1]
	lastCommit, err := s.storage.LoadCommit(lastCommitID)
	if err != nil {
		return Commit{}, fmt.Errorf("agentvcs: load last commit: %w", err)
	}

	// Scan current state.
	workingDir, err := os.Getwd()
	if err != nil {
		return Commit{}, fmt.Errorf("agentvcs: get working dir: %w", err)
	}

	// Reuse the parent tree's hashes for files whose size and modtime are
	// unchanged, so recording a delta costs a stat walk instead of re-hashing
	// the whole working directory.
	var baseline map[string]TreeEntry
	if parentTree, terr := s.storage.LoadTree(lastCommit.TreeID); terr == nil {
		baseline = indexEntries(parentTree.Entries)
		// Modification times only have second resolution, so a file rewritten
		// again within the same second the parent commit was recorded can keep
		// both its size and its modtime. Drop those entries from the cache so
		// they are always re-hashed instead of silently reusing a stale hash.
		for path, e := range baseline {
			if !e.IsDir && e.ModTime >= lastCommit.CreatedAt-1 {
				delete(baseline, path)
			}
		}
	}

	entries, err := s.scanner.ScanWithBaseline(workingDir, baseline)
	if err != nil {
		return Commit{}, fmt.Errorf("agentvcs: scan: %w", err)
	}

	treeID, err := computeTreeID(entries)
	if err != nil {
		return Commit{}, err
	}

	// If tree hasn't changed, return the last commit without creating a new one.
	if treeID == lastCommit.TreeID {
		logging.Debug("agentvcs: no changes detected, skipping commit", "session", sessionID)
		return lastCommit, nil
	}

	return s.createCommitFromEntries(ctx, sessionID, lastCommitID, description, entries, treeID, workingDir)
}

// createCommit scans the working directory and creates a full commit.
func (s *service) createCommit(_ context.Context, sessionID, parentID, description string) (Commit, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return Commit{}, fmt.Errorf("agentvcs: get working dir: %w", err)
	}

	entries, err := s.scanner.Scan(workingDir)
	if err != nil {
		return Commit{}, fmt.Errorf("agentvcs: scan: %w", err)
	}

	treeID, err := computeTreeID(entries)
	if err != nil {
		return Commit{}, err
	}

	return s.createCommitFromEntries(nil, sessionID, parentID, description, entries, treeID, workingDir)
}

// createCommitFromEntries persists a commit with pre-computed entries and tree ID.
func (s *service) createCommitFromEntries(_ context.Context, sessionID, parentID, description string, entries []TreeEntry, treeID, workingDir string) (Commit, error) {
	now := nowUnix()

	// Persist tree.
	tree := Tree{ID: treeID, Entries: entries}
	if err := s.storage.SaveTree(tree); err != nil {
		return Commit{}, fmt.Errorf("agentvcs: save tree: %w", err)
	}

	// The root commit of a session is a *baseline*: it records the state the
	// working directory was already in when the session started. Reporting its
	// whole tree as "added" made every session look like it had changed 100% of
	// the project, so a baseline carries no change set at all. Real changes are
	// the delta commits recorded on top of it.
	isBaseline := parentID == ""

	var changedFiles []DiffEntry
	if !isBaseline {
		var err error
		changedFiles, err = s.diffTreeAgainstParent(parentID, tree)
		if err != nil {
			return Commit{}, err
		}
	}

	changedPaths := make(map[string]struct{}, len(changedFiles))
	var changedTotalSize int64
	for _, diff := range changedFiles {
		changedPaths[diff.Path] = struct{}{}
		switch diff.Type {
		case DiffDeleted:
			changedTotalSize += diff.OldSize
		default:
			changedTotalSize += diff.NewSize
		}
	}

	// Store blobs for the files this commit needs to be self-contained: the
	// baseline needs the full tree (so later diffs and reverts have a reference),
	// delta commits only need what they introduced or modified.
	for _, e := range entries {
		if e.IsDir || e.Hash == "" {
			continue
		}
		if _, ok := changedPaths[e.Path]; !ok && !isBaseline {
			continue
		}
		if s.storage.BlobExists(e.Hash) {
			continue
		}
		srcPath := filepath.Join(workingDir, filepath.FromSlash(e.Path))
		if err := s.storage.StoreBlob(srcPath, e.Hash); err != nil {
			logging.Error("agentvcs: store blob failed", "path", e.Path, "error", err)
		}
	}

	// Compute file stats.
	var totalSize int64
	fileCount := 0
	for _, e := range entries {
		if !e.IsDir {
			fileCount++
			totalSize += e.Size
		}
	}

	commitID := computeCommitID(parentID, treeID, sessionID, description, now)

	commit := Commit{
		ID:               commitID,
		ParentID:         parentID,
		SessionID:        sessionID,
		TreeID:           treeID,
		Description:      description,
		Author:           "agent",
		CreatedAt:        now,
		FileCount:        fileCount,
		TotalSize:        totalSize,
		ChangedFileCount: len(changedFiles),
		ChangedTotalSize: changedTotalSize,
		IsBaseline:       isBaseline,
	}

	if err := s.storage.SaveCommit(commit); err != nil {
		return Commit{}, fmt.Errorf("agentvcs: save commit: %w", err)
	}

	// Update session log.
	sl, _ := s.storage.LoadSessionLog(sessionID)
	if sl.SessionID == "" {
		sl.SessionID = sessionID
		sl.CreatedAt = now
	}
	sl.Commits = append(sl.Commits, commitID)
	sl.UpdatedAt = now
	if err := s.storage.SaveSessionLog(sl); err != nil {
		return Commit{}, fmt.Errorf("agentvcs: save session log: %w", err)
	}

	s.Publish(pubsub.CreatedEvent, commit)
	logging.Info("agentvcs: commit created",
		"id", shortID(commitID),
		"session", sessionID,
		"files", fileCount,
		"changed", len(changedFiles),
		"baseline", isBaseline,
		"parent", shortID(parentID),
		"desc", description,
	)

	return commit, nil
}

// Log returns the commit history for a session.
func (s *service) Log(_ context.Context, sessionID string) ([]CommitSummary, error) {
	sl, err := s.storage.LoadSessionLog(sessionID)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: load session log: %w", err)
	}

	summaries := make([]CommitSummary, 0, len(sl.Commits))
	for _, cid := range sl.Commits {
		c, err := s.storage.LoadCommit(cid)
		if err != nil {
			logging.Error("agentvcs: load commit for log", "id", cid, "error", err)
			continue
		}
		summaries = append(summaries, c.toSummary())
	}
	return summaries, nil
}

// GetCommit retrieves a single commit.
func (s *service) GetCommit(_ context.Context, id string) (Commit, error) {
	return s.storage.LoadCommit(id)
}

// GetTree retrieves a tree object.
func (s *service) GetTree(_ context.Context, id string) (Tree, error) {
	return s.storage.LoadTree(id)
}

// Diff computes file-level changes between two commits.
func (s *service) Diff(_ context.Context, commitID1, commitID2 string) ([]DiffEntry, error) {
	return s.diffCommits(commitID1, commitID2)
}

// DiffFromParent computes changes between a commit and its parent.
func (s *service) DiffFromParent(_ context.Context, commitID string) ([]DiffEntry, error) {
	c, err := s.storage.LoadCommit(commitID)
	if err != nil {
		return nil, err
	}
	if c.ParentID == "" {
		// Session baseline: it is the reference state, not a change set.
		return []DiffEntry{}, nil
	}
	return s.diffCommits(c.ParentID, commitID)
}

// SessionDiff returns the aggregate changes recorded during a session, i.e. the
// difference between its baseline commit and its current HEAD.
func (s *service) SessionDiff(_ context.Context, sessionID string) ([]DiffEntry, error) {
	sl, err := s.storage.LoadSessionLog(sessionID)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: load session log: %w", err)
	}
	if len(sl.Commits) < 2 {
		return []DiffEntry{}, nil
	}
	return s.diffCommits(sl.Commits[0], sl.Commits[len(sl.Commits)-1])
}

func (s *service) diffCommits(id1, id2 string) ([]DiffEntry, error) {
	c1, err := s.storage.LoadCommit(id1)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: load commit %s: %w", id1, err)
	}
	c2, err := s.storage.LoadCommit(id2)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: load commit %s: %w", id2, err)
	}

	t1, err := s.storage.LoadTree(c1.TreeID)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: load tree %s: %w", c1.TreeID, err)
	}
	t2, err := s.storage.LoadTree(c2.TreeID)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: load tree %s: %w", c2.TreeID, err)
	}

	return diffTrees(t1, t2), nil
}

func (s *service) diffTreeAgainstParent(parentID string, tree Tree) ([]DiffEntry, error) {
	parent, err := s.storage.LoadCommit(parentID)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: load parent commit %s: %w", parentID, err)
	}
	parentTree, err := s.storage.LoadTree(parent.TreeID)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: load tree %s: %w", parent.TreeID, err)
	}
	return diffTrees(parentTree, tree), nil
}

// diffTrees computes file-level changes between two trees.
func diffTrees(t1, t2 Tree) []DiffEntry {
	idx1 := indexEntries(t1.Entries)
	idx2 := indexEntries(t2.Entries)

	var diffs []DiffEntry

	for path, e1 := range idx1 {
		if e1.IsDir {
			continue
		}
		e2, ok := idx2[path]
		if !ok {
			diffs = append(diffs, DiffEntry{
				Path: path, Type: DiffDeleted,
				OldHash: e1.Hash, OldSize: e1.Size,
			})
			continue
		}
		if e1.Hash != e2.Hash {
			diffs = append(diffs, DiffEntry{
				Path: path, Type: DiffModified,
				OldHash: e1.Hash, NewHash: e2.Hash,
				OldSize: e1.Size, NewSize: e2.Size,
			})
		}
	}

	for path, e2 := range idx2 {
		if e2.IsDir {
			continue
		}
		if _, ok := idx1[path]; !ok {
			diffs = append(diffs, DiffEntry{
				Path: path, Type: DiffAdded,
				NewHash: e2.Hash, NewSize: e2.Size,
			})
		}
	}

	// Sort for deterministic output.
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs
}

func indexEntries(entries []TreeEntry) map[string]TreeEntry {
	m := make(map[string]TreeEntry, len(entries))
	for _, e := range entries {
		m[e.Path] = e
	}
	return m
}

// GetFileContent retrieves decompressed blob content.
func (s *service) GetFileContent(_ context.Context, hash string) ([]byte, error) {
	return s.storage.LoadBlob(hash)
}

// ListSessions returns all session IDs with commit history.
func (s *service) ListSessions(_ context.Context) ([]string, error) {
	return s.storage.ListSessionLogs()
}

// SessionCommitCount returns the number of commits in a session.
func (s *service) SessionCommitCount(_ context.Context, sessionID string) (int, error) {
	sl, err := s.storage.LoadSessionLog(sessionID)
	if err != nil {
		return 0, err
	}
	return len(sl.Commits), nil
}

// Revert restores the working directory to a commit's state.
func (s *service) Revert(ctx context.Context, commitID string) error {
	commit, err := s.storage.LoadCommit(commitID)
	if err != nil {
		return fmt.Errorf("agentvcs: revert load commit: %w", err)
	}

	tree, err := s.storage.LoadTree(commit.TreeID)
	if err != nil {
		return fmt.Errorf("agentvcs: revert load tree: %w", err)
	}

	// Create safety commit before reverting.
	_, err = s.Record(ctx, commit.SessionID, "Auto-backup before revert to "+shortID(commitID))
	if err != nil {
		logging.Error("agentvcs: failed to create safety commit", "error", err)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("agentvcs: get working dir: %w", err)
	}

	// Build target file set.
	targetFiles := make(map[string]TreeEntry, len(tree.Entries))
	for _, e := range tree.Entries {
		if !e.IsDir {
			targetFiles[e.Path] = e
		}
	}

	// Scan current state to find files to delete.
	currentEntries, err := s.scanner.Scan(workingDir)
	if err != nil {
		return fmt.Errorf("agentvcs: revert scan: %w", err)
	}

	// Restore all files from the target commit.
	for path, e := range targetFiles {
		content, err := s.storage.LoadBlob(e.Hash)
		if err != nil {
			return fmt.Errorf("agentvcs: revert load blob %s: %w", path, err)
		}
		absPath := filepath.Join(workingDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("agentvcs: revert mkdir %s: %w", path, err)
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return fmt.Errorf("agentvcs: revert write %s: %w", path, err)
		}
	}

	// Delete files not in the target.
	for _, e := range currentEntries {
		if e.IsDir {
			continue
		}
		if _, ok := targetFiles[e.Path]; !ok {
			absPath := filepath.Join(workingDir, filepath.FromSlash(e.Path))
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				logging.Error("agentvcs: revert remove", "path", e.Path, "error", err)
			}
		}
	}

	logging.Info("agentvcs: reverted to commit", "id", shortID(commitID))
	return nil
}

// Cleanup removes sessions older than maxAgeDays and caps total sessions at
// maxSessions. Orphan blobs are collected afterwards.
func (s *service) Cleanup(_ context.Context, maxAgeDays, maxSessions int) error {
	sessionIDs, err := s.storage.ListSessionLogs()
	if err != nil {
		return fmt.Errorf("agentvcs: cleanup list sessions: %w", err)
	}

	type sessionAge struct {
		id        string
		updatedAt int64
	}

	var sessions []sessionAge
	for _, sid := range sessionIDs {
		sl, err := s.storage.LoadSessionLog(sid)
		if err != nil {
			continue
		}
		sessions = append(sessions, sessionAge{id: sid, updatedAt: sl.UpdatedAt})
	}

	// Sort newest first.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].updatedAt > sessions[j].updatedAt
	})

	now := time.Now().Unix()
	toDelete := make(map[string]bool)

	// Age-based pruning.
	if maxAgeDays > 0 {
		cutoff := now - int64(maxAgeDays)*86400
		for _, sa := range sessions {
			if sa.updatedAt < cutoff {
				toDelete[sa.id] = true
			}
		}
	}

	// Count-based pruning.
	if maxSessions > 0 && len(sessions) > maxSessions {
		for _, sa := range sessions[maxSessions:] {
			toDelete[sa.id] = true
		}
	}

	// Delete selected sessions and their commits/trees.
	for sid := range toDelete {
		s.deleteSession(sid)
	}

	// Orphan blob cleanup.
	refs, err := s.storage.referencedBlobHashes()
	if err != nil {
		logging.Error("agentvcs: cleanup refs", "error", err)
		return nil
	}

	allBlobs, err := s.storage.ListAllBlobHashes()
	if err != nil {
		logging.Error("agentvcs: cleanup list blobs", "error", err)
		return nil
	}

	removed := 0
	for hash := range allBlobs {
		if _, ok := refs[hash]; !ok {
			if err := s.storage.DeleteBlob(hash); err == nil {
				removed++
			}
		}
	}

	logging.Info("agentvcs: cleanup complete", "sessionsRemoved", len(toDelete), "blobsRemoved", removed)
	return nil
}

// deleteSession removes all data for a session.
func (s *service) deleteSession(sessionID string) {
	sl, err := s.storage.LoadSessionLog(sessionID)
	if err != nil {
		return
	}
	// Collect tree IDs before deleting commits.
	treeIDs := make(map[string]bool)
	for _, cid := range sl.Commits {
		c, err := s.storage.LoadCommit(cid)
		if err == nil {
			treeIDs[c.TreeID] = true
		}
		s.storage.DeleteCommit(cid)
	}
	for tid := range treeIDs {
		s.storage.DeleteTree(tid)
	}
	s.storage.DeleteSessionLog(sessionID)
	logging.Debug("agentvcs: deleted session", "id", sessionID, "commits", len(sl.Commits))
}
