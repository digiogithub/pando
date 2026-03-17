package snapshot

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
)

// storage manages the on-disk layout for snapshots.
//
// Layout under the snapshots root:
//
//	snapshots/
//	├── {snapshotID}/
//	│   └── manifest.json
//	├── blobs/
//	│   └── {hash[0:2]}/{hash[2:4]}/{hash}   (gzip-compressed file content)
//	└── index.json                             (ordered list of snapshot IDs)
type storage struct {
	root     string // absolute path of the snapshots directory
	blobsDir string // absolute path of the blobs subdirectory
	indexPath string // absolute path of index.json
}

// newStorage initialises the storage layer rooted at dir.
func newStorage(dir string) (*storage, error) {
	s := &storage{
		root:      dir,
		blobsDir:  filepath.Join(dir, "blobs"),
		indexPath: filepath.Join(dir, "index.json"),
	}

	if err := os.MkdirAll(s.blobsDir, 0o755); err != nil {
		return nil, fmt.Errorf("storage: create blobs dir: %w", err)
	}

	// Initialise the index if it does not exist yet.
	if _, err := os.Stat(s.indexPath); os.IsNotExist(err) {
		if err := s.writeIndex(nil); err != nil {
			return nil, fmt.Errorf("storage: init index: %w", err)
		}
	}

	return s, nil
}

// SaveSnapshot persists the manifest and stores the file blobs.
// Blobs are content-addressable and are skipped when already present.
func (s *storage) SaveSnapshot(m Manifest) error {
	snapDir := filepath.Join(s.root, m.Snapshot.ID)
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		return fmt.Errorf("storage: create snapshot dir: %w", err)
	}

	// Write manifest.json.
	manifestPath := filepath.Join(snapDir, "manifest.json")
	if err := writeJSON(manifestPath, m); err != nil {
		return fmt.Errorf("storage: write manifest: %w", err)
	}

	// Store blobs for non-directory files.
	workingDir := m.Snapshot.WorkingDir
	for _, f := range m.Files {
		if f.IsDir || f.Hash == "" {
			continue
		}
		blobPath := s.blobPath(f.Hash)
		// Content-addressable: skip if the blob already exists.
		if _, err := os.Stat(blobPath); err == nil {
			logging.Debug("storage: blob already exists, skipping", "hash", f.Hash)
			continue
		}

		srcPath := filepath.Join(workingDir, filepath.FromSlash(f.Path))
		if err := s.storeBlob(srcPath, blobPath); err != nil {
			logging.Error("storage: failed to store blob", "hash", f.Hash, "path", f.Path, "error", err)
			// Non-fatal: continue storing remaining blobs.
		}
	}

	// Append this snapshot ID to the index.
	ids, err := s.ListSnapshots()
	if err != nil {
		ids = nil
	}
	ids = append(ids, m.Snapshot.ID)
	if err := s.writeIndex(ids); err != nil {
		return fmt.Errorf("storage: update index after save: %w", err)
	}

	return nil
}

// LoadManifest reads and returns the manifest for the given snapshot ID.
func (s *storage) LoadManifest(snapshotID string) (Manifest, error) {
	manifestPath := filepath.Join(s.root, snapshotID, "manifest.json")
	var m Manifest
	if err := readJSON(manifestPath, &m); err != nil {
		return Manifest{}, fmt.Errorf("storage: load manifest %s: %w", snapshotID, err)
	}
	return m, nil
}

// LoadBlob reads and decompresses a blob by its SHA-256 hash.
func (s *storage) LoadBlob(hash string) ([]byte, error) {
	blobPath := s.blobPath(hash)
	f, err := os.Open(blobPath)
	if err != nil {
		return nil, fmt.Errorf("storage: open blob %s: %w", hash, err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("storage: gzip reader for blob %s: %w", hash, err)
	}
	defer gr.Close()

	data, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("storage: read blob %s: %w", hash, err)
	}
	return data, nil
}

// DeleteSnapshot removes the snapshot directory for the given ID and updates
// the index. Blobs are shared and must be cleaned separately via CleanOrphanBlobs.
func (s *storage) DeleteSnapshot(snapshotID string) error {
	snapDir := filepath.Join(s.root, snapshotID)
	if err := os.RemoveAll(snapDir); err != nil {
		return fmt.Errorf("storage: delete snapshot dir %s: %w", snapshotID, err)
	}

	ids, err := s.ListSnapshots()
	if err != nil {
		ids = nil
	}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != snapshotID {
			filtered = append(filtered, id)
		}
	}
	if err := s.writeIndex(filtered); err != nil {
		return fmt.Errorf("storage: update index after delete: %w", err)
	}
	return nil
}

// ListSnapshots returns the ordered list of snapshot IDs from the index.
func (s *storage) ListSnapshots() ([]string, error) {
	var ids []string
	if err := readJSON(s.indexPath, &ids); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: read index: %w", err)
	}
	return ids, nil
}

// UpdateIndex rebuilds index.json by scanning the snapshot directories on disk.
// This is useful for recovery after manual edits or partial failures.
func (s *storage) UpdateIndex() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return fmt.Errorf("storage: read root for index rebuild: %w", err)
	}

	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip the blobs directory.
		if name == "blobs" {
			continue
		}
		// Verify that a manifest exists.
		manifestPath := filepath.Join(s.root, name, "manifest.json")
		if _, err := os.Stat(manifestPath); err == nil {
			ids = append(ids, name)
		}
	}

	if err := s.writeIndex(ids); err != nil {
		return fmt.Errorf("storage: write rebuilt index: %w", err)
	}
	logging.Info("storage: index rebuilt", "count", len(ids))
	return nil
}

// CleanOrphanBlobs removes blobs that are not referenced by any existing manifest.
func (s *storage) CleanOrphanBlobs() error {
	ids, err := s.ListSnapshots()
	if err != nil {
		return fmt.Errorf("storage: clean orphans list snapshots: %w", err)
	}

	// Collect the set of referenced hashes.
	referenced := make(map[string]struct{})
	for _, id := range ids {
		m, err := s.LoadManifest(id)
		if err != nil {
			logging.Error("storage: clean orphans load manifest", "id", id, "error", err)
			continue
		}
		for _, f := range m.Files {
			if f.Hash != "" {
				referenced[f.Hash] = struct{}{}
			}
		}
	}

	// Walk the blobs directory and remove unreferenced blobs.
	removed := 0
	err = filepath.Walk(s.blobsDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		// The blob file name is the full hash.
		hash := info.Name()
		if _, ok := referenced[hash]; !ok {
			if rmErr := os.Remove(path); rmErr != nil {
				logging.Error("storage: remove orphan blob", "hash", hash, "error", rmErr)
			} else {
				removed++
				logging.Debug("storage: removed orphan blob", "hash", hash)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("storage: walk blobs for orphan cleanup: %w", err)
	}

	logging.Info("storage: orphan blob cleanup complete", "removed", removed)
	return nil
}

// blobPath returns the absolute path for a blob identified by its hash.
// The layout is: blobs/{hash[0:2]}/{hash[2:4]}/{hash}
func (s *storage) blobPath(hash string) string {
	if len(hash) < 4 {
		return filepath.Join(s.blobsDir, hash)
	}
	return filepath.Join(s.blobsDir, hash[0:2], hash[2:4], hash)
}

// storeBlob reads srcPath, compresses its content with gzip, and writes it to
// blobPath, creating intermediate directories as needed.
func (s *storage) storeBlob(srcPath, blobPath string) error {
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		return fmt.Errorf("storage: create blob dir: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("storage: open source file %s: %w", srcPath, err)
	}
	defer src.Close()

	// Write to a temp file first, then rename for atomicity.
	tmpPath := blobPath + ".tmp"
	dst, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("storage: create temp blob %s: %w", tmpPath, err)
	}

	gw := gzip.NewWriter(dst)
	if _, err := io.Copy(gw, src); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("storage: compress blob %s: %w", srcPath, err)
	}
	if err := gw.Close(); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("storage: flush gzip writer for %s: %w", srcPath, err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("storage: close temp blob %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, blobPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("storage: rename blob %s -> %s: %w", tmpPath, blobPath, err)
	}
	return nil
}

// writeIndex atomically writes the slice of IDs as JSON to index.json.
func (s *storage) writeIndex(ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	return writeJSON(s.indexPath, ids)
}

// writeJSON marshals v and writes it to path, creating or truncating the file.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json for %s: %w", path, err)
	}
	// Write to temp file then rename for atomicity.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp json %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename json %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

// readJSON reads path and unmarshals the JSON content into v.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Tolerate empty files as empty arrays/objects.
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal json from %s: %w", path, err)
	}
	return nil
}
