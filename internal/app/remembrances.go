package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/convert"
	"github.com/digiogithub/pando/internal/logging"
	rag "github.com/digiogithub/pando/internal/rag"
)

func sanitizeRemembrancesProjectID(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}

	result := make([]byte, 0, len(trimmed))
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		switch {
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9'):
			result = append(result, ch)
		case ch == '-' || ch == '_':
			result = append(result, ch)
		default:
			result = append(result, '_')
		}
	}

	return strings.Trim(strings.TrimSpace(string(result)), "_")
}

// initKBLinkBackfill indexes the [[wiki links]] of documents stored before the
// link graph existed, so a database upgraded from an older Pando gets a complete
// graph instead of one that only covers documents written from now on. It runs
// in the background because it is pure local work (no embeddings) and must not
// delay startup. Databases with nothing to backfill exit on the first query.
// Primary-only (see primary_services.go): it is registered here and started by
// startPrimaryServices.
func (app *App) initKBLinkBackfill(svc *rag.RemembrancesService) {
	if svc == nil || svc.KB == nil || !svc.KB.WikiLinksEnabled() {
		return
	}

	app.registerPrimaryService("kb-link-backfill", func(ctx context.Context) {
		backfillCtx, cancel := context.WithCancel(ctx)
		app.cancelFuncsMutex.Lock()
		app.watcherCancelFuncs = append(app.watcherCancelFuncs, cancel)
		app.cancelFuncsMutex.Unlock()

		app.watcherWG.Add(1)
		go func() {
			defer app.watcherWG.Done()
			stats, err := svc.KB.BackfillLinks(backfillCtx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				logging.Error("remembrances kb: wiki link backfill failed", "error", err)
				return
			}
			if stats.Links == 0 {
				return
			}
			logging.Info("remembrances kb: wiki link backfill completed",
				"documents", stats.Documents,
				"links", stats.Links,
				"scanned", stats.Scanned,
			)
		}()
	})
}

// initRemembrancesKBSync configures the KB filesystem mirror and document
// converter for this process (every role: they shape this process's own KB
// writes) and registers the primary-only KB auto-import and directory watcher
// (see primary_services.go), so the KB directory is imported and watched once
// per project instead of once per process.
func (app *App) initRemembrancesKBSync(svc *rag.RemembrancesService, cfg *config.RemembrancesConfig) {
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

	// Install the document converter so docx/pdf/xlsx/… dropped in the KB
	// directory are converted to Markdown on the fly and indexed, referencing
	// the original file.
	if cfg.KBConvertDocuments {
		svc.KB.SetDocumentConverter(convert.NewWithConvertibleExtensions(cfg.KBConvertExtensions))
		logging.Info("remembrances kb: document conversion enabled", "path", kbPath)
	}

	if cfg.KBAutoImport {
		app.registerPrimaryService("kb-auto-import", func(ctx context.Context) {
			logging.WarnPersist("KB import/index started in background", "path", kbPath)
			importCtx, importCancel := context.WithCancel(ctx)
			app.cancelFuncsMutex.Lock()
			app.watcherCancelFuncs = append(app.watcherCancelFuncs, importCancel)
			app.cancelFuncsMutex.Unlock()

			app.watcherWG.Add(1)
			go func() {
				defer app.watcherWG.Done()
				stats, err := svc.KB.SyncDirectoryWithStats(importCtx, kbPath, true)
				if err != nil {
					logging.ErrorPersist("KB import/index failed", "path", kbPath, "error", err)
					logging.Error("remembrances kb: initial import failed", "path", kbPath, "error", err)
					return
				}
				summary := fmt.Sprintf("KB import/index completed (%d scanned, %d added, %d updated, %d unchanged, %d deleted)",
					stats.Scanned,
					stats.Added,
					stats.Updated,
					stats.Unchanged,
					stats.Deleted,
				)
				// Mentioned only when the imported documents use the syntax, so a KB
				// without wiki links logs exactly the line it logged before.
				if stats.LinksIndexed > 0 {
					summary = fmt.Sprintf("%s — %d wiki links indexed", summary, stats.LinksIndexed)
				}
				logging.WarnPersist(summary, "path", kbPath)
				logging.Info("remembrances kb: initial import completed",
					"path", kbPath,
					"scanned", stats.Scanned,
					"added", stats.Added,
					"updated", stats.Updated,
					"unchanged", stats.Unchanged,
					"deleted", stats.Deleted,
					"links_indexed", stats.LinksIndexed,
				)
			}()
		})
	}

	if !cfg.KBWatch {
		return
	}

	app.registerPrimaryService("kb-watch", func(ctx context.Context) {
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

		logging.Info("remembrances kb: filesystem sync enabled", "path", kbPath, "watch", true)
	})
}
