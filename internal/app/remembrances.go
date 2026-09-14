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
func (app *App) initKBLinkBackfill(ctx context.Context, svc *rag.RemembrancesService) {
	if svc == nil || svc.KB == nil || !svc.KB.WikiLinksEnabled() {
		return
	}

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
}

// initKBEmbeddingStalenessCheck compares the recorded embedding dimension of
// every chunk against the currently configured document embedder and logs a
// single loud warning naming both the configured model and the model(s)
// recorded on the mismatched chunks, plus the count — evidence the document
// embedding model changed since those chunks were written, which otherwise
// blinds every consumer at once with no error, no log line, and no way to
// notice (PANDO-US-0029). It logs nothing on a consistent corpus.
//
// Unlike the link backfill above, this is one indexed COUNT query (and, only
// when it finds a mismatch, one more DISTINCT query for the offending model
// names) — fast enough to run inline at startup rather than in a background
// goroutine.
func (app *App) initKBEmbeddingStalenessCheck(ctx context.Context, svc *rag.RemembrancesService, cfg *config.RemembrancesConfig) {
	if svc == nil || svc.KB == nil || cfg == nil {
		return
	}
	embedder := svc.DocumentEmbedder()
	if embedder == nil {
		return
	}
	configuredDims := embedder.Dimension()
	if configuredDims <= 0 {
		return
	}

	stats, err := svc.KB.CountStaleEmbeddings(ctx, configuredDims)
	if err != nil {
		logging.Error("remembrances kb: embedding staleness check failed", "error", err)
		return
	}
	if stats.Count == 0 {
		return
	}

	recordedModels := "unknown"
	if len(stats.RecordedModels) > 0 {
		recordedModels = strings.Join(stats.RecordedModels, ", ")
	}
	logging.WarnPersist(fmt.Sprintf(
		"KB embedding staleness: %d chunk(s) recorded under model(s) %q no longer match the configured document embedding model %q (%d dims) — recall from those chunks is degraded until a reindex (POST /api/v1/remembrances/kb/reindex)",
		stats.Count, recordedModels, cfg.DocumentEmbeddingModel, configuredDims,
	))
}

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

	// Install the document converter so docx/pdf/xlsx/… dropped in the KB
	// directory are converted to Markdown on the fly and indexed, referencing
	// the original file.
	if cfg.KBConvertDocuments {
		svc.KB.SetDocumentConverter(convert.NewWithConvertibleExtensions(cfg.KBConvertExtensions))
		logging.Info("remembrances kb: document conversion enabled", "path", kbPath)
	}

	if cfg.KBAutoImport {
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

			// One-shot repair for documents a pre-fix watcher damaged: it replaced
			// a document's metadata with bare source_* fields on every edit,
			// discarding tags and other front-matter keys, and recorded a fresh
			// source_mtime_unix so the sync above never noticed and never fixed
			// it. Bounded: it records a marker on success and is a no-op on any
			// later start, including one against a corpus this fixed code indexed
			// from scratch (PANDO-US-0003).
			repairStats, repairErr := svc.KB.RepairFrontMatterMetadata(importCtx, kbPath)
			if repairErr != nil {
				if errors.Is(repairErr, context.Canceled) {
					return
				}
				logging.Error("remembrances kb: front matter repair failed", "path", kbPath, "error", repairErr)
				return
			}
			if repairStats.Repaired > 0 {
				logging.WarnPersist(fmt.Sprintf("KB front-matter metadata repair completed (%d scanned, %d repaired)",
					repairStats.Scanned, repairStats.Repaired), "path", kbPath)
			}
			logging.Info("remembrances kb: front matter repair completed",
				"path", kbPath,
				"scanned", repairStats.Scanned,
				"repaired", repairStats.Repaired,
			)
		}()
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
