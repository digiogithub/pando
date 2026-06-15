package fileutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/sahilm/fuzzy"
)

// projectMarkers are files/dirs that indicate a directory is a software project root.
var projectMarkers = []string{
	".git",
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"setup.py",
	"pom.xml",
	"build.gradle",
	"CMakeLists.txt",
	".pando",
	"Makefile",
	"composer.json",
	"mix.exs",
	"pubspec.yaml",
}

// isFSRoot returns true when dir is the root of a filesystem.
// On Unix this is "/"; on Windows it is the volume root ("C:\", "\\server\share\").
func isFSRoot(dir string) bool {
	if dir == "/" {
		return true
	}
	vol := filepath.VolumeName(dir)
	if vol == "" {
		return false
	}
	// After removing the volume name, only a single separator should remain.
	rest := dir[len(vol):]
	return rest == string(filepath.Separator)
}

// sameDir reports whether a and b refer to the same directory.
// It uses os.SameFile when both paths are stat-able (handles symlinks and
// Windows/macOS case-insensitive filesystems via inode/file-index comparison).
// Falls back to case-insensitive string comparison on Windows and macOS, and
// exact comparison on Linux.
func sameDir(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ia, ib)
	}
	// os.Stat failed for at least one path; compare strings.
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// IsSafeWorkingDirectory returns true when dir is safe to scan recursively.
// It returns false (unsafe) when dir is the filesystem root, the user's home
// directory, or any ancestor of the home directory, or when the directory
// contains no recognised project-marker file/folder.
//
// The check is cross-platform: it uses os.SameFile for equality (handles
// symlinks and case-insensitive filesystems on Windows and macOS) and walks up
// from the home directory to detect ancestor paths correctly.
func IsSafeWorkingDirectory(dir string) bool {
	dir = filepath.Clean(dir)

	// Always unsafe: filesystem root (/, C:\, \\server\share\, …).
	if isFSRoot(dir) {
		return false
	}

	// Always unsafe: home directory or any of its ancestors.
	home, err := os.UserHomeDir()
	if err == nil {
		home = filepath.Clean(home)

		// Walk upward from home toward the root. If any ancestor equals dir,
		// then dir is the home dir itself or a parent of it → unsafe.
		// Home directories are typically only 2–4 levels deep so this is fast.
		current := home
		for {
			if sameDir(current, dir) {
				return false
			}
			parent := filepath.Dir(current)
			if parent == current {
				// Reached the filesystem root without a match.
				break
			}
			current = parent
		}
	}

	// Safe only when at least one project marker is present.
	for _, marker := range projectMarkers {
		if _, err := os.Lstat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}

	return false
}

// FuzzyFilter filters items by query using the same scoring algorithm as fzf.
// Results are sorted by match quality (best matches first).
func FuzzyFilter(query string, items []string) []string {
	if query == "" {
		return items
	}
	matches := fuzzy.Find(query, items)
	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.Str
	}
	return result
}

type FileInfo struct {
	Path    string
	ModTime time.Time
}

func SkipHidden(path string) bool {
	// Check for hidden files (starting with a dot)
	base := filepath.Base(path)
	if base != "." && strings.HasPrefix(base, ".") {
		return true
	}

	commonIgnoredDirs := map[string]bool{
		".pando":           true,
		"node_modules":     true,
		"vendor":           true,
		"dist":             true,
		"build":            true,
		"target":           true,
		".git":             true,
		".idea":            true,
		".vscode":          true,
		"__pycache__":      true,
		"bin":              true,
		"obj":              true,
		"out":              true,
		"coverage":         true,
		"tmp":              true,
		"temp":             true,
		"logs":             true,
		"generated":        true,
		"bower_components": true,
		"jspm_packages":    true,
	}

	parts := strings.Split(path, string(os.PathSeparator))
	for _, part := range parts {
		if commonIgnoredDirs[part] {
			return true
		}
	}
	return false
}

func GlobWithDoublestar(pattern, searchPath string, limit int) ([]string, bool, error) {
	fsys := os.DirFS(searchPath)
	relPattern := strings.TrimPrefix(pattern, "/")
	var matches []FileInfo

	err := doublestar.GlobWalk(fsys, relPattern, func(path string, d fs.DirEntry) error {
		if d.IsDir() {
			return nil
		}
		if SkipHidden(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		absPath := path
		if !strings.HasPrefix(absPath, searchPath) && searchPath != "." {
			absPath = filepath.Join(searchPath, absPath)
		} else if !strings.HasPrefix(absPath, "/") && searchPath == "." {
			absPath = filepath.Join(searchPath, absPath) // Ensure relative paths are joined correctly
		}

		matches = append(matches, FileInfo{Path: absPath, ModTime: info.ModTime()})
		if limit > 0 && len(matches) >= limit*2 {
			return fs.SkipAll
		}
		return nil
	})
	// fs.SkipAll is the sentinel returned by the callback to stop the walk early
	// once the limit is reached; GlobWalk surfaces it as an error, so it must be
	// treated as normal completion rather than a failure. Otherwise large
	// projects (more than limit*2 files) would yield zero results.
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return nil, false, fmt.Errorf("glob walk error: %w", err)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].ModTime.After(matches[j].ModTime)
	})

	truncated := false
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
		truncated = true
	}

	results := make([]string, len(matches))
	for i, m := range matches {
		results[i] = m.Path
	}
	return results, truncated, nil
}
