package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/fileutil"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/fsnotify/fsnotify"
)

// bootstrapExcludedDirs lists directory names the bootstrap watcher never
// descends into. Per-client LSP watchers maintain their own (richer) exclusion
// logic; this set only needs to keep the lightweight bootstrap watcher from
// drowning in churn-heavy build/dependency folders.
var bootstrapExcludedDirs = map[string]struct{}{
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	"target":       {},
	"out":          {},
	".git":         {},
}

// startLSPBootstrapWatcher launches a single, lightweight workspace-wide file
// watcher whose only job is to lazily activate language servers on demand.
//
// On-demand activation is normally driven by the agent's file tools and by the
// TUI editor/file-tree. Those cover files Pando itself touches, but a developer
// may edit files with an external editor, or a build step may regenerate them.
// This watcher closes that gap: when any file is written or created it calls
// EnsureLSPForFile, which is idempotent and cheap (servers already running,
// spawning, or known-broken are skipped before any PATH lookup). It is only
// started when LSPAutoActivate is enabled.
func (app *App) startLSPBootstrapWatcher(ctx context.Context) {
	root := config.WorkingDirectory()
	if !fileutil.IsSafeWorkingDirectory(root) {
		logging.Debug("LSP bootstrap watcher skipped; unsafe working directory", "dir", root)
		return
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		logging.Error("Failed to create LSP bootstrap watcher", "error", err)
		return
	}

	// Recursively register the workspace directories.
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries, keep walking
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && bootstrapShouldExcludeDir(path) {
			return filepath.SkipDir
		}
		if addErr := fsWatcher.Add(path); addErr != nil {
			logging.Debug("LSP bootstrap watcher could not watch dir", "path", path, "error", addErr)
		}
		return nil
	}); err != nil {
		logging.Debug("LSP bootstrap watcher walk error", "error", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		defer fsWatcher.Close()
		defer logging.RecoverPanic("lsp-bootstrap-watcher", nil)

		logging.Info("LSP bootstrap watcher started", "root", root)
		for {
			select {
			case <-watchCtx.Done():
				logging.Debug("LSP bootstrap watcher stopped")
				return
			case event, ok := <-fsWatcher.Events:
				if !ok {
					return
				}
				app.handleBootstrapEvent(watchCtx, fsWatcher, event)
			case err, ok := <-fsWatcher.Errors:
				if !ok {
					return
				}
				logging.Debug("LSP bootstrap watcher error", "error", err)
			}
		}
	}()
}

// handleBootstrapEvent reacts to a single fsnotify event: it keeps the watch set
// in sync as directories appear and triggers lazy LSP activation for edited
// files.
func (app *App) handleBootstrapEvent(ctx context.Context, fsWatcher *fsnotify.Watcher, event fsnotify.Event) {
	// Track newly created directories so freshly added subtrees are watched too.
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if !bootstrapShouldExcludeDir(event.Name) {
				_ = fsWatcher.Add(event.Name)
			}
			return
		}
	}

	// Only writes and creates of files can introduce a new language to activate.
	if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}
	if strings.HasSuffix(event.Name, "~") {
		return
	}
	if filepath.Ext(event.Name) == "" {
		return
	}
	app.EnsureLSPForFile(ctx, event.Name)
}

// bootstrapShouldExcludeDir reports whether the bootstrap watcher should skip a
// directory (hidden directories and well-known build/dependency folders).
func bootstrapShouldExcludeDir(path string) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") {
		return true
	}
	_, excluded := bootstrapExcludedDirs[name]
	return excluded
}
