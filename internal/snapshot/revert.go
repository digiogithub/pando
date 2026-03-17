package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/digiogithub/pando/internal/logging"
)

// Revert restores the working directory to the exact state captured in the
// given snapshot. A safety snapshot of the current state is created first so
// that the operation can be undone.
func (s *service) Revert(ctx context.Context, snapshotID string) error {
	// 1. Load the manifest for the target snapshot.
	manifest, err := s.storage.LoadManifest(snapshotID)
	if err != nil {
		return fmt.Errorf("snapshot: revert load manifest %s: %w", snapshotID, err)
	}

	// 2. Create a safety snapshot of the current state before reverting.
	safetySnap, err := s.Create(ctx, manifest.Snapshot.SessionID, SnapshotTypeManual,
		"Auto-backup before revert to "+snapshotID)
	if err != nil {
		return fmt.Errorf("snapshot: revert create safety snapshot: %w", err)
	}
	logging.Info("created safety snapshot before revert", "safetyID", safetySnap.ID)

	// 3. Build a map of the snapshot's non-directory files (relative path -> SnapshotFile).
	snapshotFiles := make(map[string]SnapshotFile, len(manifest.Files))
	for _, f := range manifest.Files {
		if !f.IsDir {
			snapshotFiles[f.Path] = f
		}
	}

	// 4. Scan the current working directory so we know what files exist right now.
	currentFiles, err := s.scanner.Scan(manifest.Snapshot.WorkingDir)
	if err != nil {
		return fmt.Errorf("snapshot: revert scan working dir: %w", err)
	}
	currentMap := make(map[string]SnapshotFile, len(currentFiles))
	for _, f := range currentFiles {
		if !f.IsDir {
			currentMap[f.Path] = f
		}
	}

	// 5. Restore every file recorded in the snapshot.
	for path, sf := range snapshotFiles {
		content, err := s.storage.LoadBlob(sf.Hash)
		if err != nil {
			return fmt.Errorf("snapshot: revert load blob (hash=%s, path=%s): %w", sf.Hash, path, err)
		}

		absPath := filepath.Join(manifest.Snapshot.WorkingDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("snapshot: revert create parent dir for %s: %w", path, err)
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return fmt.Errorf("snapshot: revert write file %s: %w", path, err)
		}
	}

	// 6. Delete files that exist now but were NOT in the snapshot.
	for path := range currentMap {
		if _, exists := snapshotFiles[path]; !exists {
			absPath := filepath.Join(manifest.Snapshot.WorkingDir, filepath.FromSlash(path))
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				logging.Error("snapshot: revert remove extra file", "path", path, "error", err)
			} else {
				removeEmptyParents(absPath, manifest.Snapshot.WorkingDir)
			}
		}
	}

	logging.Info("reverted to snapshot", "snapshotID", snapshotID, "filesRestored", len(snapshotFiles))
	return nil
}

// Apply applies the delta between fromSnapshotID and toSnapshotID to the
// working directory recorded in fromSnapshotID. A safety snapshot is created
// before any files are modified.
func (s *service) Apply(ctx context.Context, fromSnapshotID, toSnapshotID string) error {
	// 1. Load both manifests.
	fromManifest, err := s.storage.LoadManifest(fromSnapshotID)
	if err != nil {
		return fmt.Errorf("snapshot: apply load from-manifest %s: %w", fromSnapshotID, err)
	}
	toManifest, err := s.storage.LoadManifest(toSnapshotID)
	if err != nil {
		return fmt.Errorf("snapshot: apply load to-manifest %s: %w", toSnapshotID, err)
	}

	// 2. Create a safety snapshot before applying the diff.
	safetySnap, err := s.Create(ctx, toManifest.Snapshot.SessionID, SnapshotTypeManual,
		"Auto-backup before apply "+fromSnapshotID+"→"+toSnapshotID)
	if err != nil {
		return fmt.Errorf("snapshot: apply create safety snapshot: %w", err)
	}
	logging.Info("created safety snapshot before apply", "safetyID", safetySnap.ID)

	// 3. Compute the diff between the two snapshots.
	diffs := diffManifests(fromManifest, toManifest)

	// 4. Build an index of files in the "to" manifest for quick hash lookup.
	toFiles := make(map[string]SnapshotFile, len(toManifest.Files))
	for _, f := range toManifest.Files {
		if !f.IsDir {
			toFiles[f.Path] = f
		}
	}

	// 5. Apply each diff entry to the working directory of the "from" snapshot.
	workingDir := fromManifest.Snapshot.WorkingDir
	for _, diff := range diffs {
		absPath := filepath.Join(workingDir, filepath.FromSlash(diff.Path))

		switch diff.Type {
		case DiffAdded, DiffModified:
			sf, ok := toFiles[diff.Path]
			if !ok {
				logging.Error("snapshot: apply missing file in to-manifest", "path", diff.Path)
				continue
			}
			content, err := s.storage.LoadBlob(sf.Hash)
			if err != nil {
				return fmt.Errorf("snapshot: apply load blob (hash=%s, path=%s): %w", sf.Hash, diff.Path, err)
			}
			if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
				return fmt.Errorf("snapshot: apply create parent dir for %s: %w", diff.Path, err)
			}
			if err := os.WriteFile(absPath, content, 0o644); err != nil {
				return fmt.Errorf("snapshot: apply write file %s: %w", diff.Path, err)
			}

		case DiffDeleted:
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				logging.Error("snapshot: apply remove deleted file", "path", diff.Path, "error", err)
			} else {
				removeEmptyParents(absPath, workingDir)
			}
		}
	}

	logging.Info("applied snapshot diff", "from", fromSnapshotID, "to", toSnapshotID, "changes", len(diffs))
	return nil
}

// removeEmptyParents removes empty parent directories up to (but not
// including) root. It stops as soon as it encounters a non-empty directory or
// reaches the filesystem boundary.
func removeEmptyParents(path, root string) {
	// Normalise both paths so the comparison is reliable.
	root = filepath.Clean(root)
	dir := filepath.Dir(filepath.Clean(path))

	for dir != root && dir != "." && dir != "/" {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(dir); err != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
}
