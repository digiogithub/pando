package search

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ignorePattern represents a single pattern from a .gitignore or .pandoignore file.
type ignorePattern struct {
	pattern string // doublestar-compatible pattern, always relative to root
	negate  bool   // true if the pattern starts with '!'
	dirOnly bool   // true if the pattern ends with '/'
	root    string // absolute directory this pattern is anchored to
}

// IgnoreMatcher holds compiled ignore patterns from one or more ignore files.
// Patterns are applied in order; last match wins (like git).
type IgnoreMatcher struct {
	patterns []ignorePattern
}

// Matches returns true if the given absolute path should be ignored.
// isDir should be true when the path is a directory.
func (m *IgnoreMatcher) Matches(absPath string, isDir bool) bool {
	if len(m.patterns) == 0 {
		return false
	}
	matched := false
	for _, p := range m.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		rel, err := filepath.Rel(p.root, absPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		ok, _ := doublestar.Match(p.pattern, rel)
		if !ok {
			// try matching just the base name for unanchored patterns
			base := filepath.Base(rel)
			ok, _ = doublestar.Match(p.pattern, base)
		}
		if ok {
			matched = !p.negate
		}
	}
	return matched
}

// parseIgnoreFile parses a single .gitignore-format file and appends patterns to the matcher.
func parseIgnoreFile(path string, root string, m *IgnoreMatcher) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Strip trailing carriage return
		line = strings.TrimRight(line, "\r")
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = line[1:]
		}
		dirOnly := strings.HasSuffix(line, "/")
		if dirOnly {
			line = strings.TrimSuffix(line, "/")
		}
		// Anchored: pattern contains '/' (not at end, already stripped)
		// Unanchored: prefix with '**/' so doublestar matches at any depth
		pattern := line
		if !strings.Contains(line, "/") {
			pattern = "**/" + line
		} else {
			pattern = strings.TrimPrefix(pattern, "/")
		}
		m.patterns = append(m.patterns, ignorePattern{
			pattern: pattern,
			negate:  negate,
			dirOnly: dirOnly,
			root:    root,
		})
	}
	return scanner.Err()
}

// LoadIgnoreFiles loads .gitignore and .pandoignore files starting from rootPath
// and walking up to the filesystem root. Returns an IgnoreMatcher with all patterns.
func LoadIgnoreFiles(rootPath string) (*IgnoreMatcher, error) {
	m := &IgnoreMatcher{}
	// Walk from rootPath up to root, collect gitignore files
	dir := rootPath
	var dirs []string
	for {
		dirs = append([]string{dir}, dirs...)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for _, d := range dirs {
		for _, name := range []string{".gitignore", ".pandoignore"} {
			p := filepath.Join(d, name)
			_ = parseIgnoreFile(p, d, m) // ignore file-not-found errors
		}
	}
	return m, nil
}
