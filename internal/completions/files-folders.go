package completions

import (
	"context"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/fileutil"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
)

// maxCompletionFiles is the maximum number of files returned by the file
// completion provider.  Keeping this small avoids UI freezes in large trees.
const maxCompletionFiles = 500

// indexedLookupTimeout bounds the indexed (database) lookup so a slow query can
// never block the completion popup.
const indexedLookupTimeout = 500 * time.Millisecond

// IndexedFileLister provides fast access to already-indexed project files.
// It is implemented by the code remembrances indexer. When available, indexed
// results are served instantly while the full filesystem scan runs in the
// background.
type IndexedFileLister interface {
	// ListIndexedFiles returns up to limit indexed file paths (relative to the
	// project root) for the project rooted at rootPath, optionally pre-filtered
	// by query. It returns (nil, nil) when the project is not indexed.
	ListIndexedFiles(ctx context.Context, rootPath, query string, limit int) ([]string, error)
}

type filesAndFoldersContextGroup struct {
	prefix string
	lister IndexedFileLister

	mu          sync.Mutex
	gen         int           // bumped by Reset; stale scan goroutines ignore their writes
	globStarted bool
	globReady   bool
	mergedFiles []string      // glob ∪ indexed, populated once the scan finishes
	globDone    chan struct{} // closed when the background scan completes
}

func (cg *filesAndFoldersContextGroup) GetId() string {
	return cg.prefix
}

func (cg *filesAndFoldersContextGroup) GetEntry() dialog.CompletionItemI {
	return dialog.NewCompletionItem(dialog.CompletionItem{
		Title: "Files & Folders",
		Value: "files",
	})
}

// workingDir returns the project working directory used both as the glob root
// and as the key for resolving the indexed project.
func (cg *filesAndFoldersContextGroup) workingDir() string {
	if cfg := config.Get(); cfg != nil && cfg.WorkingDir != "" {
		return cfg.WorkingDir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// runGlob performs the (potentially slow) recursive filesystem scan and returns
// the unfiltered, visible file list. It returns nil for unsafe directories
// (home, root, …) to avoid scanning huge trees.
func (cg *filesAndFoldersContextGroup) runGlob() []string {
	cwd, err := os.Getwd()
	if err != nil {
		logging.Debug("file completions: getwd failed", "error", err)
		return nil
	}

	if !fileutil.IsSafeWorkingDirectory(cwd) {
		logging.Debug("file completions: skipping glob – not a project directory", "cwd", cwd)
		return nil
	}

	files, _, err := fileutil.GlobWithDoublestar("**/*", ".", maxCompletionFiles)
	if err != nil {
		logging.Debug("file completions: glob failed", "error", err)
		return nil
	}

	allFiles := make([]string, 0, len(files))
	for _, file := range files {
		if !fileutil.SkipHidden(file) {
			allFiles = append(allFiles, file)
		}
	}
	return allFiles
}

// ensureGlobStarted launches the background filesystem scan exactly once.
func (cg *filesAndFoldersContextGroup) ensureGlobStarted() {
	cg.mu.Lock()
	if cg.globStarted {
		cg.mu.Unlock()
		return
	}
	cg.globStarted = true
	cg.globReady = false
	cg.globDone = make(chan struct{})
	done := cg.globDone
	gen := cg.gen
	cg.mu.Unlock()

	go func() {
		globFiles := cg.runGlob()
		merged := mergeUnique(globFiles, cg.indexedFiles(""))

		cg.mu.Lock()
		// Ignore this result if Reset() ran while the scan was in flight.
		if cg.gen == gen {
			cg.mergedFiles = merged
			cg.globReady = true
		}
		cg.mu.Unlock()

		close(done)
	}()
}

// Reset implements dialog.ResettableCompletionProvider. The dialog calls it when
// the popup is dismissed so the next "@" invocation re-scans the filesystem and
// reflects any files created or removed during the session.
func (cg *filesAndFoldersContextGroup) Reset() {
	cg.mu.Lock()
	cg.gen++
	cg.globStarted = false
	cg.globReady = false
	cg.mergedFiles = nil
	cg.globDone = nil
	cg.mu.Unlock()
}

// indexedFiles returns indexed files for query, or nil when no indexer is wired
// or the current project is not indexed.
func (cg *filesAndFoldersContextGroup) indexedFiles(query string) []string {
	if cg.lister == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), indexedLookupTimeout)
	defer cancel()

	files, err := cg.lister.ListIndexedFiles(ctx, cg.workingDir(), query, maxCompletionFiles)
	if err != nil {
		logging.Debug("file completions: indexed lookup failed", "error", err)
		return nil
	}
	return files
}

// getFiles returns candidate paths for query. While the background scan is
// running it serves fast indexed results; once the scan finishes it serves the
// merged (glob ∪ indexed) set. Results are always fuzzy-ranked by query.
func (cg *filesAndFoldersContextGroup) getFiles(query string) []string {
	cg.ensureGlobStarted()

	cg.mu.Lock()
	ready := cg.globReady
	merged := cg.mergedFiles
	cg.mu.Unlock()

	if ready {
		return fileutil.FuzzyFilter(query, merged)
	}

	// Background scan still running: serve the indexed preview if we have one.
	return fileutil.FuzzyFilter(query, cg.indexedFiles(query))
}

func (cg *filesAndFoldersContextGroup) GetChildEntries(query string) ([]dialog.CompletionItemI, error) {
	matches := cg.getFiles(query)

	items := make([]dialog.CompletionItemI, 0, len(matches))
	for _, file := range matches {
		item := dialog.NewCompletionItem(dialog.CompletionItem{
			Title: file,
			Value: file,
		})
		items = append(items, item)
	}

	return items, nil
}

// RefreshCmd implements dialog.AsyncCompletionProvider. It returns a command
// that blocks until the background filesystem scan completes, then signals the
// dialog to re-query so the listing is updated with the full result set. It
// returns nil when the scan has already finished (nothing to wait for).
func (cg *filesAndFoldersContextGroup) RefreshCmd() tea.Cmd {
	cg.mu.Lock()
	ready := cg.globReady
	done := cg.globDone
	cg.mu.Unlock()

	if ready || done == nil {
		return nil
	}

	return func() tea.Msg {
		<-done
		return dialog.CompletionRefreshMsg{}
	}
}

// mergeUnique returns the union of a and b preserving order (a first), skipping
// duplicates.
func mergeUnique(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range b {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// NewFileAndFolderContextGroup builds the "@" file completion provider. lister
// may be nil, in which case completions fall back to a filesystem scan only.
func NewFileAndFolderContextGroup(lister IndexedFileLister) dialog.CompletionProvider {
	return &filesAndFoldersContextGroup{
		prefix: "file",
		lister: lister,
	}
}
