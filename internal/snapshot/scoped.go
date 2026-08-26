package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
)

// SnapshotTypeScoped marks a snapshot whose root is a sub-directory of the
// project instead of the whole working directory.
const SnapshotTypeScoped = "scoped"

// CreateScoped captures a snapshot rooted at rootDir instead of the working
// directory. Every path in the resulting manifest is relative to rootDir, and
// Snapshot.WorkingDir records rootDir, so RevertScoped and Compare operate on
// that sub-tree alone: files outside it are never read, restored or deleted.
//
// This is what design artifacts use for versioning — a checkout of an old
// artifact version must never revert unrelated work in the repository.
func (s *service) CreateScoped(_ context.Context, sessionID, description, rootDir string) (Snapshot, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: scoped abs path %s: %w", rootDir, err)
	}
	absRoot = filepath.Clean(absRoot)

	files, err := s.scanner.ScanScoped(absRoot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: scoped scan %s: %w", absRoot, err)
	}

	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}

	snap := Snapshot{
		ID:          uuid.New().String(),
		SessionID:   sessionID,
		Type:        SnapshotTypeScoped,
		Description: description,
		WorkingDir:  absRoot,
		FileCount:   len(files),
		TotalSize:   totalSize,
		CreatedAt:   time.Now().Unix(),
	}

	if err := s.storage.SaveSnapshot(Manifest{Snapshot: snap, Files: files}); err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: scoped save: %w", err)
	}

	s.Publish(pubsub.CreatedEvent, snap)
	logging.Info("scoped snapshot created", "id", snap.ID, "root", absRoot, "files", snap.FileCount)
	return snap, nil
}

// RevertScoped restores the sub-tree captured by a scoped snapshot. Only files
// under the snapshot's root are touched: recorded files are rewritten, and
// files that appeared inside the root since the snapshot are removed. A safety
// snapshot of the same scope is taken first.
func (s *service) RevertScoped(ctx context.Context, snapshotID string) error {
	manifest, err := s.storage.LoadManifest(snapshotID)
	if err != nil {
		return fmt.Errorf("snapshot: scoped revert load manifest %s: %w", snapshotID, err)
	}
	if manifest.Snapshot.Type != SnapshotTypeScoped {
		return fmt.Errorf("snapshot: %s is not a scoped snapshot", snapshotID)
	}

	root := manifest.Snapshot.WorkingDir
	if root == "" {
		return fmt.Errorf("snapshot: scoped revert %s has no root", snapshotID)
	}

	if _, err := s.CreateScoped(ctx, manifest.Snapshot.SessionID,
		"Auto-backup before scoped revert to "+snapshotID, root); err != nil {
		return fmt.Errorf("snapshot: scoped revert safety snapshot: %w", err)
	}

	wanted := make(map[string]SnapshotFile, len(manifest.Files))
	for _, f := range manifest.Files {
		if !f.IsDir {
			wanted[f.Path] = f
		}
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("snapshot: scoped revert create root %s: %w", root, err)
	}

	for path, sf := range wanted {
		content, err := s.storage.LoadBlob(sf.Hash)
		if err != nil {
			return fmt.Errorf("snapshot: scoped revert load blob (hash=%s, path=%s): %w", sf.Hash, path, err)
		}
		absPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("snapshot: scoped revert parent dir for %s: %w", path, err)
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return fmt.Errorf("snapshot: scoped revert write %s: %w", path, err)
		}
	}

	current, err := s.scanner.ScanScoped(root)
	if err != nil {
		return fmt.Errorf("snapshot: scoped revert rescan %s: %w", root, err)
	}
	for _, f := range current {
		if f.IsDir {
			continue
		}
		if _, keep := wanted[f.Path]; keep {
			continue
		}
		absPath := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			logging.Error("snapshot: scoped revert remove extra file", "path", f.Path, "error", err)
			continue
		}
		removeEmptyParents(absPath, root)
	}

	logging.Info("scoped snapshot reverted", "id", snapshotID, "root", root, "files", len(wanted))
	return nil
}
