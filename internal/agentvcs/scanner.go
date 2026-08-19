package agentvcs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/fileutil"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/search"
)

const defaultMaxFileSize int64 = 256 * 1024 * 1024 // 256 MiB

// alwaysSkipDirs are excluded regardless of ignore rules.
var alwaysSkipDirs = []string{".pando", ".git", ".jj"}

// scanner walks a directory tree and produces TreeEntry slices.
type scanner struct {
	maxFileSize     int64
	excludePatterns []string // extra patterns from config
}

func newScanner(maxFileSize int64, excludePatterns []string) *scanner {
	if maxFileSize <= 0 {
		maxFileSize = defaultMaxFileSize
	}
	return &scanner{maxFileSize: maxFileSize, excludePatterns: excludePatterns}
}

// Scan walks rootDir and returns a sorted slice of TreeEntry.
// It respects .gitignore, .pandoignore, and config exclude patterns.
func (sc *scanner) Scan(rootDir string) ([]TreeEntry, error) {
	return sc.ScanWithBaseline(rootDir, nil)
}

// ScanWithBaseline behaves like Scan but reuses the content hash of a previous
// scan whenever a file's size and modification time are unchanged. Hashing the
// whole working tree on every commit is prohibitively expensive on large
// projects, so incremental recordings pass the parent commit's tree here and
// only re-hash the files that actually moved.
func (sc *scanner) ScanWithBaseline(rootDir string, baseline map[string]TreeEntry) ([]TreeEntry, error) {
	if !fileutil.IsSafeWorkingDirectory(rootDir) {
		logging.Debug("agentvcs scanner: skipping – not a project directory", "dir", rootDir)
		return nil, nil
	}

	ignoreMatcher, err := search.LoadIgnoreFiles(rootDir)
	if err != nil {
		logging.Error("agentvcs scanner: failed to load ignore files", "error", err)
		ignoreMatcher = &search.IgnoreMatcher{}
	}

	var entries []TreeEntry

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			logging.Error("agentvcs scanner: walk error", "path", path, "error", walkErr)
			return nil
		}
		if path == rootDir {
			return nil
		}

		name := info.Name()
		isDir := info.IsDir()

		if isDir && isAlwaysSkipped(name) {
			return filepath.SkipDir
		}

		if ignoreMatcher.Matches(path, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply config exclude patterns.
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			relPath = path
		}
		relPath = filepath.ToSlash(relPath)

		if sc.matchesExclude(relPath, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}

		if isDir {
			entries = append(entries, TreeEntry{
				Path:    relPath,
				ModTime: info.ModTime().Unix(),
				IsDir:   true,
			})
			return nil
		}

		if info.Size() > sc.maxFileSize {
			logging.Debug("agentvcs scanner: skipping large file", "path", path, "size", info.Size())
			return nil
		}

		hash := ""
		if prev, ok := baseline[relPath]; ok && !prev.IsDir &&
			prev.Hash != "" && prev.Size == info.Size() && prev.ModTime == info.ModTime().Unix() {
			hash = prev.Hash
		} else {
			var hashErr error
			hash, hashErr = hashFile(path)
			if hashErr != nil {
				logging.Error("agentvcs scanner: hash failed", "path", path, "error", hashErr)
				return nil
			}
		}

		entries = append(entries, TreeEntry{
			Path:    relPath,
			Hash:    hash,
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("agentvcs scanner: walk %s: %w", rootDir, err)
	}

	return entries, nil
}

func (sc *scanner) matchesExclude(relPath string, isDir bool) bool {
	for _, pat := range sc.excludePatterns {
		dirOnly := strings.HasSuffix(pat, "/")
		if dirOnly && !isDir {
			continue
		}
		p := strings.TrimSuffix(pat, "/")
		// Simple suffix/prefix matching for config patterns.
		if strings.HasPrefix(p, "*.") {
			ext := p[1:] // e.g. ".log"
			if strings.HasSuffix(relPath, ext) {
				return true
			}
			continue
		}
		if strings.Contains(relPath, p) {
			return true
		}
	}
	return false
}

func isAlwaysSkipped(name string) bool {
	for _, skip := range alwaysSkipDirs {
		if strings.EqualFold(name, skip) {
			return true
		}
	}
	return false
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
