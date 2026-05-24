package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	rag "github.com/digiogithub/pando/internal/rag"
)

func (app *App) initRemembrancesProjectIndexing(ctx context.Context, svc *rag.RemembrancesService, cfg *config.RemembrancesConfig, startupMode string) {
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

	projectID := strings.TrimSpace(cfg.ContextEnrichmentCodeProject)
	if projectID == "" {
		projectID = sanitizeRemembrancesProjectID(rootPath)
	}

	if projectID == "" {
		logging.Warn("remembrances code: unable to determine startup project id", "path", rootPath)
		return
	}

	if cfg.ContextEnrichmentCodeProject == "" {
		cfg.ContextEnrichmentCodeProject = projectID
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
		if err := app.runStartupProjectIndex(indexCtx, svc, projectID, rootPath, startupMode); err != nil && !errors.Is(err, context.Canceled) {
			logging.Error("remembrances code: startup indexing failed",
				"project_id", projectID,
				"path", rootPath,
				"startup_mode", startupMode,
				"error", err,
			)
		}
	}()
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
