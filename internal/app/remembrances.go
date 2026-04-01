package app

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	rag "github.com/digiogithub/pando/internal/rag"
)

func (app *App) initRemembrancesKBSync(ctx context.Context, svc *rag.RemembrancesService, cfg *config.RemembrancesConfig) {
	if svc == nil || svc.KB == nil || cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.KBPath) == "" {
		return
	}

	kbPath := strings.TrimSpace(cfg.KBPath)
	if !filepath.IsAbs(kbPath) {
		kbPath = filepath.Join(config.WorkingDirectory(), kbPath)
	}
	kbPath = filepath.Clean(kbPath)

	if err := svc.KB.ConfigureFilesystemMirror(kbPath); err != nil {
		logging.Error("remembrances kb: configure filesystem mirror failed", "path", kbPath, "error", err)
		return
	}

	if cfg.KBAutoImport {
		stats, err := svc.KB.SyncDirectoryWithStats(ctx, kbPath, true)
		if err != nil {
			logging.Error("remembrances kb: initial import failed", "path", kbPath, "error", err)
		} else {
			logging.Info("remembrances kb: initial import completed",
				"path", kbPath,
				"scanned", stats.Scanned,
				"added", stats.Added,
				"updated", stats.Updated,
				"unchanged", stats.Unchanged,
				"deleted", stats.Deleted,
			)
		}
	}

	if !cfg.KBWatch {
		return
	}

	watchCtx, cancel := context.WithCancel(ctx)
	app.cancelFuncsMutex.Lock()
	app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
	app.cancelFuncsMutex.Unlock()

	app.watcherWG.Add(1)
	go func() {
		defer app.watcherWG.Done()
		if err := svc.KB.WatchDirectory(watchCtx, kbPath); err != nil {
			logging.Error("remembrances kb: watcher exited with error", "path", kbPath, "error", err)
		}
	}()

	logging.Info("remembrances kb: filesystem sync enabled", "path", kbPath, "watch", cfg.KBWatch)
}
