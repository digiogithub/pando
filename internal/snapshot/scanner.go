package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/search"
)

const (
	// defaultMaxFileSize is the file size limit above which a file is skipped
	// during scanning (256 MiB).
	defaultMaxFileSize = 256 * 1024 * 1024
)

// scanner walks a directory tree and produces a list of SnapshotFile entries.
type scanner struct {
	maxFileSize int64
}

// newScanner creates a scanner with the default max file size limit.
func newScanner() *scanner {
	return &scanner{maxFileSize: defaultMaxFileSize}
}

// newScannerWithLimit creates a scanner with a custom max file size limit.
func newScannerWithLimit(maxFileSize int64) *scanner {
	return &scanner{maxFileSize: maxFileSize}
}

// alwaysSkipDirs contains directory names that are always excluded from
// scanning regardless of ignore file rules.
var alwaysSkipDirs = []string{
	".pando",
	".git",
}

// Scan walks rootDir recursively and returns a SnapshotFile for every file
// that passes the ignore rules. Directories are recorded without hashing.
func (sc *scanner) Scan(rootDir string) ([]SnapshotFile, error) {
	ignoreMatcher, err := search.LoadIgnoreFiles(rootDir)
	if err != nil {
		// Non-fatal: proceed without ignore rules.
		logging.Error("scanner: failed to load ignore files", "dir", rootDir, "error", err)
		ignoreMatcher = &search.IgnoreMatcher{}
	}

	var files []SnapshotFile

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			// Log and skip paths that cannot be stat-ed.
			logging.Error("scanner: walk error", "path", path, "error", walkErr)
			return nil
		}

		// Skip the root itself.
		if path == rootDir {
			return nil
		}

		name := info.Name()
		isDir := info.IsDir()

		// Skip always-ignored directories.
		if isDir && isAlwaysSkipped(name) {
			logging.Debug("scanner: skipping directory", "path", path)
			return filepath.SkipDir
		}

		// Apply .gitignore / .pandoignore rules.
		if ignoreMatcher.Matches(path, isDir) {
			logging.Debug("scanner: ignored by ignore rules", "path", path)
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}

		// Compute relative path from rootDir for portability.
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			relPath = path
		}
		relPath = filepath.ToSlash(relPath)

		if isDir {
			files = append(files, SnapshotFile{
				Path:    relPath,
				ModTime: info.ModTime().Unix(),
				IsDir:   true,
			})
			return nil
		}

		// Skip files that exceed the size limit.
		if info.Size() > sc.maxFileSize {
			logging.Debug("scanner: skipping large file", "path", path, "size", info.Size(), "limit", sc.maxFileSize)
			return nil
		}

		hash, err := hashFile(path)
		if err != nil {
			logging.Error("scanner: failed to hash file", "path", path, "error", err)
			return nil
		}

		files = append(files, SnapshotFile{
			Path:    relPath,
			Hash:    hash,
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			IsDir:   false,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanner: walk %s: %w", rootDir, err)
	}

	logging.Debug("scanner: scan complete", "dir", rootDir, "files", len(files))
	return files, nil
}

// isAlwaysSkipped returns true when the directory name should unconditionally
// be excluded from snapshots.
func isAlwaysSkipped(name string) bool {
	for _, skip := range alwaysSkipDirs {
		if strings.EqualFold(name, skip) {
			return true
		}
	}
	return false
}

// hashFile computes the SHA-256 digest of a file and returns it as a hex string.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash file open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
