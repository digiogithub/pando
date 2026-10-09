package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/fileutil"
	"github.com/digiogithub/pando/internal/fswatch"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/fsnotify/fsnotify"
)

// startLSPBootstrapWatcher launches a single, lightweight workspace-wide file
// watcher whose only job is to lazily activate language servers on demand.
//
// On-demand activation is normally driven by the agent's file tools and by the
// TUI editor/file-tree. Those cover files Pando itself touches, but a developer
// may edit files with an external editor, or a build step may regenerate them.
// This watcher closes that gap: when any file is written or created it calls
// EnsureLSPForFileTrigger, which is idempotent and cheap (servers already
// running, spawning, or known-broken are skipped before any PATH lookup).
//
// It is opt-in: only LSPActivateOn="workspace" starts it, because it can spawn
// servers for languages Pando itself never touches during the session.
func (app *App) startLSPBootstrapWatcher(ctx context.Context) {
	root := config.WorkingDirectory()
	if !fileutil.IsSafeWorkingDirectory(root) {
		logging.Debug("LSP bootstrap watcher skipped; unsafe working directory", "dir", root)
		return
	}

	sub := app.workspaceHub().Subscribe()
	if sub == nil {
		return
	}

	watchCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		defer sub.Close()
		defer logging.RecoverPanic("lsp-bootstrap-watcher", nil)

		logging.Info("LSP bootstrap watcher started", "root", root)
		for {
			select {
			case <-watchCtx.Done():
				logging.Debug("LSP bootstrap watcher stopped")
				return
			case event, ok := <-sub.Events:
				if !ok {
					return
				}
				app.handleBootstrapEvent(watchCtx, event)
			}
		}
	}()
}

// handleBootstrapEvent reacts to a single fsnotify event from the shared
// workspace watcher and triggers lazy LSP activation for edited files. The hub
// itself keeps the watch set in sync as directories appear.
func (app *App) handleBootstrapEvent(ctx context.Context, event fsnotify.Event) {
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
	// A freshly created directory is not a file to activate a server for.
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			return
		}
	}
	app.EnsureLSPForFileTrigger(ctx, event.Name, config.LSPTriggerWorkspace)
}

// newWatchExcluder builds the shared directory excluder for a workspace watcher
// from the configured WatchExclude patterns.
func newWatchExcluder(root string) *fswatch.Excluder {
	var extra []string
	if cfg := config.Get(); cfg != nil {
		extra = cfg.WatchExclude
	}
	return fswatch.NewExcluder(root, extra)
}
