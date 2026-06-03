package agentvcs

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

// storage manages the on-disk layout for agent-vcs.
//
// Layout:
//
//	agent-vcs/
//	├── commits/{commitID}.json
//	├── trees/{treeID}.json
//	├── blobs/{h[0:2]}/{h[2:4]}/{h}   (gzip-compressed)
//	├── sessions/{sessionID}.json
//	└── config.json
type storage struct {
	root        string
	commitsDir  string
	treesDir    string
	blobsDir    string
	sessionsDir string
}

// newStorage initialises the storage directories under root.
func newStorage(root string) (*storage, error) {
	s := &storage{
		root:        root,
		commitsDir:  filepath.Join(root, "commits"),
		treesDir:    filepath.Join(root, "trees"),
		blobsDir:    filepath.Join(root, "blobs"),
		sessionsDir: filepath.Join(root, "sessions"),
	}
	for _, dir := range []string{s.commitsDir, s.treesDir, s.blobsDir, s.sessionsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("agentvcs storage: mkdir %s: %w", dir, err)
		}
	}
	return s, nil
}

// --- Commit persistence ---

func (s *storage) SaveCommit(c Commit) error {
	path := filepath.Join(s.commitsDir, c.ID+".json")
	return writeJSON(path, c)
}

func (s *storage) LoadCommit(id string) (Commit, error) {
	path, err := s.safePath(s.commitsDir, id+".json")
	if err != nil {
		return Commit{}, err
	}
	var c Commit
	if err := readJSON(path, &c); err != nil {
		return Commit{}, fmt.Errorf("agentvcs: load commit %s: %w", id, err)
	}
	return c, nil
}

func (s *storage) CommitExists(id string) bool {
	path := filepath.Join(s.commitsDir, id+".json")
	_, err := os.Stat(path)
	return err == nil
}

// --- Tree persistence ---

func (s *storage) SaveTree(t Tree) error {
	path := filepath.Join(s.treesDir, t.ID+".json")
	// Trees are content-addressable; skip if already stored.
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return writeJSON(path, t)
}

func (s *storage) LoadTree(id string) (Tree, error) {
	path, err := s.safePath(s.treesDir, id+".json")
	if err != nil {
		return Tree{}, err
	}
	var t Tree
	if err := readJSON(path, &t); err != nil {
		return Tree{}, fmt.Errorf("agentvcs: load tree %s: %w", id, err)
	}
	return t, nil
}

// --- Session log persistence ---

func (s *storage) SaveSessionLog(sl SessionLog) error {
	path := filepath.Join(s.sessionsDir, sl.SessionID+".json")
	return writeJSON(path, sl)
}

func (s *storage) LoadSessionLog(sessionID string) (SessionLog, error) {
	path, err := s.safePath(s.sessionsDir, sessionID+".json")
	if err != nil {
		return SessionLog{}, err
	}
	var sl SessionLog
	if err := readJSON(path, &sl); err != nil {
		if os.IsNotExist(err) {
			return SessionLog{SessionID: sessionID}, nil
		}
		return SessionLog{}, fmt.Errorf("agentvcs: load session log %s: %w", sessionID, err)
	}
	return sl, nil
}

func (s *storage) SessionLogExists(sessionID string) bool {
	path := filepath.Join(s.sessionsDir, sessionID+".json")
	_, err := os.Stat(path)
	return err == nil
}

func (s *storage) ListSessionLogs() ([]string, error) {
	entries, err := os.ReadDir(s.sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("agentvcs: list sessions: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	return ids, nil
}

// --- Blob persistence (content-addressable, gzip) ---

func (s *storage) blobPath(hash string) string {
	if len(hash) < 4 {
		return filepath.Join(s.blobsDir, hash)
	}
	return filepath.Join(s.blobsDir, hash[0:2], hash[2:4], hash)
}

func (s *storage) BlobExists(hash string) bool {
	_, err := os.Stat(s.blobPath(hash))
	return err == nil
}

func (s *storage) StoreBlob(srcPath, hash string) error {
	blobPath := s.blobPath(hash)
	// Content-addressable: skip if already stored.
	if _, err := os.Stat(blobPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		return fmt.Errorf("agentvcs: create blob dir: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("agentvcs: open source %s: %w", srcPath, err)
	}
	defer src.Close()

	tmpPath := blobPath + ".tmp"
	dst, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("agentvcs: create temp blob: %w", err)
	}

	gw := gzip.NewWriter(dst)
	if _, err := io.Copy(gw, src); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("agentvcs: compress blob: %w", err)
	}
	if err := gw.Close(); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("agentvcs: flush gzip: %w", err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("agentvcs: close temp blob: %w", err)
	}
	if err := os.Rename(tmpPath, blobPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("agentvcs: rename blob: %w", err)
	}
	return nil
}

func (s *storage) LoadBlob(hash string) ([]byte, error) {
	f, err := os.Open(s.blobPath(hash))
	if err != nil {
		return nil, fmt.Errorf("agentvcs: open blob %s: %w", hash, err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: gzip reader %s: %w", hash, err)
	}
	defer gr.Close()

	data, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("agentvcs: read blob %s: %w", hash, err)
	}
	return data, nil
}

// DeleteBlob removes a single blob file.
func (s *storage) DeleteBlob(hash string) error {
	return os.Remove(s.blobPath(hash))
}

// ListAllBlobHashes walks the blobs directory and returns every stored hash.
func (s *storage) ListAllBlobHashes() (map[string]struct{}, error) {
	hashes := make(map[string]struct{})
	err := filepath.Walk(s.blobsDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		hashes[info.Name()] = struct{}{}
		return nil
	})
	return hashes, err
}

// --- Cleanup ---

// DeleteSessionLog removes a session log file.
func (s *storage) DeleteSessionLog(sessionID string) error {
	path := filepath.Join(s.sessionsDir, sessionID+".json")
	return os.Remove(path)
}

// DeleteCommit removes a commit file.
func (s *storage) DeleteCommit(id string) error {
	path := filepath.Join(s.commitsDir, id+".json")
	return os.Remove(path)
}

// DeleteTree removes a tree file.
func (s *storage) DeleteTree(id string) error {
	path := filepath.Join(s.treesDir, id+".json")
	return os.Remove(path)
}

// --- Helpers ---

func (s *storage) safePath(dir, name string) (string, error) {
	p := filepath.Clean(filepath.Join(dir, name))
	d := filepath.Clean(dir)
	if p == d || !strings.HasPrefix(p, d+string(os.PathSeparator)) {
		return "", fmt.Errorf("agentvcs: invalid path %q", name)
	}
	return p, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("agentvcs: marshal json: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("agentvcs: write temp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("agentvcs: rename %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("agentvcs: unmarshal %s: %w", path, err)
	}
	return nil
}

// referencedBlobHashes collects all blob hashes referenced by existing trees.
func (s *storage) referencedBlobHashes() (map[string]struct{}, error) {
	refs := make(map[string]struct{})
	entries, err := os.ReadDir(s.treesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return refs, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		treeID := strings.TrimSuffix(e.Name(), ".json")
		t, err := s.LoadTree(treeID)
		if err != nil {
			logging.Error("agentvcs: load tree for GC", "id", treeID, "error", err)
			continue
		}
		for _, entry := range t.Entries {
			if entry.Hash != "" {
				refs[entry.Hash] = struct{}{}
			}
		}
	}
	return refs, nil
}
