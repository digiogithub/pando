// Package fswatch provides shared helpers for Pando's recursive filesystem
// watchers. On macOS fsnotify uses kqueue, which costs one file descriptor per
// watched directory and per file inside it, so every watcher must skip heavy,
// generated and ignored directories BEFORE calling fsnotify's Add.
package fswatch

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/digiogithub/pando/internal/search"
)

// defaultExcludedDirs lists directory base names that are never watched.
var defaultExcludedDirs = map[string]struct{}{
	// VCS / editors / caches
	".git": {}, ".idea": {}, ".vscode": {}, ".cache": {},
	// Dependencies and build output
	"node_modules": {}, "vendor": {}, "dist": {}, "build": {}, "out": {},
	"bin": {}, "target": {}, "coverage": {}, "__pycache__": {},
	// Generated / tool state
	".gradle": {}, ".next": {}, ".nuxt": {}, ".dart_tool": {},
	".venv": {}, "venv": {},
	// Xcode / iOS / Apple
	"DerivedData": {}, "Pods": {}, "Carthage": {}, "xcuserdata": {},
	".build": {}, ".swiftpm": {},
}

// defaultExcludedDirSuffixes lists directory name suffixes that are never watched.
var defaultExcludedDirSuffixes = []string{".xcarchive", ".dSYM", ".xcresult"}

// DefaultExcludedDirs returns the sorted built-in directory names and suffix
// globs (for example "*.dSYM") that are excluded from watching.
func DefaultExcludedDirs() []string {
	out := make([]string, 0, len(defaultExcludedDirs)+len(defaultExcludedDirSuffixes))
	for n := range defaultExcludedDirs {
		out = append(out, n)
	}
	for _, s := range defaultExcludedDirSuffixes {
		out = append(out, "*"+s)
	}
	sort.Strings(out)
	return out
}

// Excluder decides which directories a watcher must not register.
// It is safe for concurrent use.
type Excluder struct {
	root   string
	extra  []string // doublestar patterns relative to root, slash separated
	mu     sync.Mutex
	ignore *search.IgnoreMatcher
	loaded map[string]struct{} // dirs whose nested ignore files were loaded
}

// NewExcluder builds an Excluder for the watch root. extra holds user patterns
// (the WatchExclude config): plain directory names or doublestar globs relative
// to root, e.g. "mobile-app/www/svg" or "**/generated".
func NewExcluder(root string, extra []string) *Excluder {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	e := &Excluder{root: root, loaded: map[string]struct{}{root: {}}}
	for _, p := range extra {
		p = strings.TrimSpace(filepath.ToSlash(p))
		p = strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")
		p = strings.TrimPrefix(p, "/")
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") && !strings.ContainsAny(p, "*?[{") {
			p = "**/" + p // bare name: match at any depth
		}
		e.extra = append(e.extra, p)
	}
	e.ignore = &search.IgnoreMatcher{}
	for _, dir := range ignoreScopeDirs(root) {
		e.ignore.AddDir(dir)
	}
	return e
}

// ignoreScopeDirs returns, outermost first, the directories whose ignore files
// apply to root: root and its ancestors up to the enclosing git repository
// root. When root is not inside a git repository only root itself is used, so
// an unrelated ignore file higher up (for example a dotfiles repository in
// $HOME whose .gitignore is "*") can never hide the whole workspace.
func ignoreScopeDirs(root string) []string {
	dirs := []string{root}
	for dir := root; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dirs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return []string{root}
		}
		dir = parent
		dirs = append([]string{dir}, dirs...)
	}
}

// ShouldSkipDir reports whether the directory at absPath must not be watched.
// The root itself is never skipped. When a directory is kept, its own
// .gitignore/.pandoignore are loaded so they apply to its children.
func (e *Excluder) ShouldSkipDir(absPath string) bool {
	if abs, err := filepath.Abs(absPath); err == nil {
		absPath = abs
	}
	if absPath == e.root {
		return false
	}
	name := filepath.Base(absPath)
	if strings.HasPrefix(name, ".") {
		return true
	}
	if _, ok := defaultExcludedDirs[name]; ok {
		return true
	}
	for _, s := range defaultExcludedDirSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	rel, err := filepath.Rel(e.root, absPath)
	if err == nil && !strings.HasPrefix(rel, "..") {
		rel = filepath.ToSlash(rel)
		for _, p := range e.extra {
			if ok, _ := doublestar.Match(p, rel); ok {
				return true
			}
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ignore.Matches(absPath, true) {
		return true
	}
	if _, done := e.loaded[absPath]; !done {
		e.loaded[absPath] = struct{}{}
		e.ignore.AddDir(absPath)
	}
	return false
}
