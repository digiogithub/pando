package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/fswatch"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/logging"
	rag "github.com/digiogithub/pando/internal/rag"
)

func (app *App) initRemembrancesProjectIndexing(ctx context.Context, svc *rag.RemembrancesService, cfg *config.RemembrancesConfig, startupMode string, skipStartupIndexing bool) {
	if svc == nil || svc.Code == nil || cfg == nil || !cfg.Enabled {
		return
	}
	if startupMode == "cronjob" {
		logging.Info("remembrances code: startup watcher skipped", "startup_mode", startupMode)
		return
	}

	workingDir := strings.TrimSpace(config.WorkingDirectory())
	if workingDir == "" {
		return
	}

	rootPath, err := filepath.Abs(workingDir)
	if err != nil {
		logging.Warn("remembrances code: resolve working directory failed", "path", workingDir, "error", err)
		return
	}

	if fi, err := os.Stat(rootPath); err != nil || !fi.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		logging.Warn("remembrances code: invalid working directory for startup indexing", "path", rootPath, "error", err)
		return
	}

	// Skip code indexing when running from the user's home directory.
	// Indexing the entire home tree is expensive and rarely intentional.
	// KB sync (handled separately) will still run if configured.
	if config.IsHomeDirectory(rootPath) {
		logging.Info("remembrances code: startup indexing skipped (running from home directory)", "path", rootPath)
		return
	}

	configuredID := strings.TrimSpace(cfg.ContextEnrichmentCodeProject)
	projectID := configuredID
	if projectID == "" {
		projectID = sanitizeRemembrancesProjectID(rootPath)
	}

	if projectID == "" {
		logging.Warn("remembrances code: unable to determine startup project id", "path", rootPath)
		return
	}

	// Reuse the id this directory is already indexed under (the code tools
	// derive ids differently), and never move a configured id that belongs to
	// another directory onto this one: either would re-index a whole tree.
	resolvedID, err := svc.Code.ResolveProjectID(ctx, projectID, rootPath)
	if err != nil {
		logging.Warn("remembrances code: resolve startup project id failed", "project_id", projectID, "path", rootPath, "error", err)
		return
	}
	if resolvedID != projectID {
		logging.Info("remembrances code: using the project id already indexed for this directory",
			"requested", projectID,
			"project_id", resolvedID,
			"path", rootPath,
		)
		projectID = resolvedID
	}

	if configuredID == "" || configuredID != projectID {
		cfg.ContextEnrichmentCodeProject = projectID
	}

	if skipStartupIndexing {
		logging.Info("remembrances code: startup indexing skipped (one-shot run)",
			"project_id", projectID,
			"path", rootPath,
			"startup_mode", startupMode,
		)
		return
	}

	start := func() {
		app.scheduleStartupProjectIndex(ctx, svc, projectID, rootPath, startupMode)
	}

	// Only the IPC primary indexes and watches the tree. Every secondary
	// would otherwise run its own recursive watcher over the same directory
	// (tens of thousands of file descriptors each with kqueue on macOS). The
	// start closure is parked so a secondary promoted to primary can run it.
	if app.isSecondaryAtStartup() {
		logging.Info("remembrances code: startup indexing and file watching skipped (another instance is the IPC primary)",
			"project_id", projectID,
			"path", rootPath,
			"startup_mode", startupMode,
		)
		app.deferredCodeIndexMu.Lock()
		app.deferredCodeIndex = start
		app.deferredCodeIndexMu.Unlock()
		return
	}

	start()
}

// isSecondaryAtStartup reports whether this instance is an IPC secondary while
// the app is still being constructed. SetIPCSecondaryContext runs after
// app.New, so the role is derived from the querier: secondaries get a
// *dbproxy.DBProxy that forwards writes to the primary.
func (app *App) isSecondaryAtStartup() bool {
	if app.IPCIsPrimary {
		return false
	}
	_, ok := app.DBQuerier.(*dbproxy.DBProxy)
	return ok
}

// startDeferredCodeIndex runs the startup code indexing/watching that was
// skipped while this instance was an IPC secondary. It is called when the
// instance is promoted to primary and runs the parked closure at most once.
func (app *App) startDeferredCodeIndex() {
	app.deferredCodeIndexMu.Lock()
	start := app.deferredCodeIndex
	app.deferredCodeIndex = nil
	app.deferredCodeIndexMu.Unlock()
	if start == nil {
		return
	}
	logging.Info("remembrances code: promoted to IPC primary, starting deferred startup indexing and file watching")
	start()
}

func (app *App) scheduleStartupProjectIndex(ctx context.Context, svc *rag.RemembrancesService, projectID, rootPath, startupMode string) {
	if ctx.Err() != nil {
		return
	}
	logging.Info("remembrances code: startup indexing scheduled",
		"project_id", projectID,
		"path", rootPath,
		"startup_mode", startupMode,
	)

	indexCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		runWithCodeWatchLock(indexCtx, rootPath, codeWatchLockRetryInterval, func() {
			if err := app.runStartupProjectIndex(indexCtx, svc, projectID, rootPath, startupMode); err != nil && !errors.Is(err, context.Canceled) {
				logging.Error("remembrances code: startup indexing failed",
					"project_id", projectID,
					"path", rootPath,
					"startup_mode", startupMode,
					"error", err,
				)
			}
		})
	}()
}

// codeWatchLockRetryInterval is how often a process that lost the code-watch
// lock retries, so a survivor takes over when the holder exits.
const codeWatchLockRetryInterval = 30 * time.Second

// runWithCodeWatchLock runs fn only while holding the per-project cross-process
// code-watch lock, so a single Pando process per project runs the startup
// index and the recursive filesystem watcher regardless of IPC role. When the
// lock is held elsewhere it retries every interval until ctx is cancelled.
// The lock is released when fn returns.
func runWithCodeWatchLock(ctx context.Context, rootPath string, interval time.Duration, fn func()) {
	logged := false
	for {
		release, ok, err := fswatch.TryLockDir(rootPath)
		if err != nil {
			// Do not block indexing on an unusable lock file; fall back to
			// the previous behaviour.
			logging.Warn("remembrances code: code-watch lock unavailable, continuing without it", "path", rootPath, "error", err)
			fn()
			return
		}
		if ok {
			defer release()
			fn()
			return
		}
		if !logged {
			logged = true
			logging.Info("remembrances code: another Pando process is already indexing/watching this project; will retry",
				"path", rootPath, "retry_interval", interval.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (app *App) runStartupProjectIndex(ctx context.Context, svc *rag.RemembrancesService, projectID, rootPath, startupMode string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	alreadyIndexed, err := svc.Code.HasProject(ctx, projectID, rootPath)
	if err != nil {
		return err
	}

	if alreadyIndexed {
		logging.Info("remembrances code: startup incremental indexing ready",
			"project_id", projectID,
			"path", rootPath,
			"startup_mode", startupMode,
		)
	} else {
		jobID, err := svc.Code.IndexProject(ctx, projectID, rootPath, nil)
		if err != nil {
			return err
		}

		logging.Info("remembrances code: startup indexing started",
			"project_id", projectID,
			"path", rootPath,
			"startup_mode", startupMode,
			"job_id", jobID,
		)
	}

	return app.watchIndexedProject(ctx, svc, projectID, rootPath, startupMode)
}
