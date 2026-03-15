package search

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// FileMatch is a file that matched a search query.
type FileMatch struct {
	Path    string
	ModTime time.Time
	Lines   []LineMatch // empty for file-only searches
	Count   int         // for OutputModeCount
}

// WalkOptions configures a file walk and optional content search.
type WalkOptions struct {
	RootPath      string
	Pattern       *regexp.Regexp // nil = list files only (no content search)
	IncludeGlob   string         // file name glob, e.g. "*.go"
	TypeFilter    string         // type name, e.g. "go" (expands via DefaultTypes)
	IgnoreMatcher *IgnoreMatcher // nil = no ignore filtering
	SearchOpts    SearchOptions
	MaxResults    int // 0 = unlimited
	Concurrency   int // 0 = runtime.NumCPU()
}

// SearchFiles walks rootPath and returns matching files, sorted by modification time (newest first).
func SearchFiles(ctx context.Context, opts WalkOptions) ([]FileMatch, bool, error) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}

	// Resolve type filter to globs
	var typeGlobs []string
	if opts.TypeFilter != "" {
		typeGlobs, _ = TypeToGlobs(opts.TypeFilter)
	}

	// Channel for file paths to process
	pathCh := make(chan string, 256)
	var resultMu sync.Mutex
	var results []FileMatch

	// Producer: walk the directory tree
	go func() {
		defer close(pathCh)
		_ = filepath.WalkDir(opts.RootPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			base := filepath.Base(path)
			// Skip hidden files and directories
			if base != "." && strings.HasPrefix(base, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			// Apply ignore matcher
			if opts.IgnoreMatcher != nil && opts.IgnoreMatcher.Matches(path, d.IsDir()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			// Apply include glob filter
			if opts.IncludeGlob != "" {
				ok, _ := doublestar.Match(opts.IncludeGlob, base)
				if !ok {
					return nil
				}
			}
			// Apply type filter
			if len(typeGlobs) > 0 {
				matched := false
				for _, g := range typeGlobs {
					ok, _ := doublestar.Match(g, base)
					if ok {
						matched = true
						break
					}
				}
				if !matched {
					return nil
				}
			}
			select {
			case pathCh <- path:
			case <-ctx.Done():
				return filepath.SkipAll
			}
			return nil
		})
	}()

	// Consumers: N workers process files
	var wg sync.WaitGroup
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range pathCh {
				if ctx.Err() != nil {
					return
				}
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				fm := FileMatch{Path: path, ModTime: info.ModTime()}

				if opts.Pattern != nil {
					switch opts.SearchOpts.OutputMode {
					case OutputModeCount:
						count, err := CountMatches(path, opts.Pattern)
						if err != nil || count == 0 {
							continue
						}
						fm.Count = count
					default:
						lines, err := SearchFile(ctx, path, opts.SearchOpts)
						if err != nil || len(lines) == 0 {
							continue
						}
						fm.Lines = lines
					}
				}

				resultMu.Lock()
				results = append(results, fm)
				resultMu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Sort by modification time, newest first
	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime.After(results[j].ModTime)
	})

	truncated := false
	if opts.MaxResults > 0 && len(results) > opts.MaxResults {
		results = results[:opts.MaxResults]
		truncated = true
	}

	return results, truncated, nil
}
