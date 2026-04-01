package kb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

type syncJob struct {
	absPath   string
	docPath   string
	mtimeUnix int64
}

type syncResult struct {
	job     syncJob
	content string
	err     error
}

type walkSummary struct {
	queued    int
	scanned   int
	unchanged int
}

const kbSyncPerFileTimeout = 5 * time.Minute

// SyncDirectoryWithStats imports or syncs all markdown files from a directory.
// It recursively scans for .md files, upserts modified documents, and optionally
// deletes KB documents that no longer exist on disk for that directory source.
func (s *KBStore) SyncDirectoryWithStats(ctx context.Context, dirPath string, deleteMissing bool) (SyncStats, error) {
	var stats SyncStats
	syncStartedAt := time.Now()
	logging.Debug("kb sync: start",
		"dir", dirPath,
		"workers", s.getSyncWorkers(),
		"deleteMissing", deleteMissing,
	)
	logSyncMemStats("kb sync: start mem", 0, 0)

	baseDir, err := filepath.Abs(strings.TrimSpace(dirPath))
	if err != nil {
		return stats, fmt.Errorf("kb: resolve sync directory %q: %w", dirPath, err)
	}

	baseInfo, err := os.Stat(baseDir)
	if err != nil {
		return stats, fmt.Errorf("kb: stat sync directory %q: %w", baseDir, err)
	}
	if !baseInfo.IsDir() {
		return stats, fmt.Errorf("kb: sync path is not a directory: %s", baseDir)
	}

	seen := make(map[string]struct{})
	offset := 0
	existingByPath := make(map[string]documentMetadata)
	for {
		items, listErr := s.listDocumentMetadata(ctx, 500, offset)
		if listErr != nil {
			return stats, fmt.Errorf("kb: preload document metadata: %w", listErr)
		}
		if len(items) == 0 {
			break
		}
		for i := range items {
			item := items[i]
			existingByPath[item.FilePath] = item
		}
		offset += len(items)
	}
	logging.Debug("kb sync: metadata preloaded", "documents", len(existingByPath))

	workerCount := s.getSyncWorkers()
	jobs := make(chan syncJob, workerCount*2)
	results := make(chan syncResult, workerCount*2)

	ctxSync, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctxSync.Err() != nil {
					return
				}
				contentBytes, readErr := os.ReadFile(job.absPath)
				res := syncResult{job: job}
				if readErr != nil {
					res.err = fmt.Errorf("kb: read file %s: %w", job.absPath, readErr)
				} else {
					res.content = string(contentBytes)
				}
				select {
				case results <- res:
				case <-ctxSync.Done():
					return
				}
			}
		}()
	}

	walkErrCh := make(chan error, 1)
	summaryCh := make(chan walkSummary, 1)
	go func() {
		summary := walkSummary{}
		walkErr := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !isMarkdownFile(path) {
				return nil
			}

			summary.scanned++
			relPath, relErr := filepath.Rel(baseDir, path)
			if relErr != nil {
				return fmt.Errorf("kb: relative path for %q: %w", path, relErr)
			}
			docPath := normalizeDocPath(relPath)
			seen[docPath] = struct{}{}

			fi, statErr := d.Info()
			if statErr != nil {
				return fmt.Errorf("kb: stat file %s: %w", path, statErr)
			}
			mtimeUnix := fi.ModTime().Unix()

			existingMeta, exists := existingByPath[docPath]
			if exists {
				prevMTime := metadataInt64(existingMeta.Metadata, "source_mtime_unix")
				if prevMTime == mtimeUnix {
					summary.unchanged++
					return nil
				}
			}

			job := syncJob{absPath: path, docPath: docPath, mtimeUnix: mtimeUnix}
			select {
			case jobs <- job:
				summary.queued++
			case <-ctxSync.Done():
				return ctxSync.Err()
			}
			return nil
		})
		close(jobs)
		walkErrCh <- walkErr
		summaryCh <- summary
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	processed := 0
	var firstErr error
	errorCount := 0

	for res := range results {
		processed++
		if res.err != nil {
			errorCount++
			firstErr = res.err
			if processed%10 == 0 {
				logSyncProgress(processed, -1, stats, errorCount)
			}
			continue
		}

		if strings.TrimSpace(res.content) == "" {
			stats.Unchanged++
			if processed%10 == 0 {
				logSyncProgress(processed, -1, stats, errorCount)
			}
			continue
		}

		meta := map[string]interface{}{
			"source_path":       res.job.absPath,
			"source_mtime_unix": res.job.mtimeUnix,
		}

		processingCtx, cancel := context.WithTimeout(ctxSync, kbSyncPerFileTimeout)

		if _, exists := existingByPath[res.job.docPath]; !exists {
			if addErr := s.AddDocument(processingCtx, res.job.docPath, res.content, meta); addErr != nil {
				cancel()
				errorCount++
				if firstErr == nil {
					firstErr = fmt.Errorf("kb: add %s: %w", res.job.docPath, addErr)
				}
				if processed%10 == 0 {
					logSyncProgress(processed, -1, stats, errorCount)
				}
				continue
			}
			cancel()
			stats.Added++
			existingByPath[res.job.docPath] = documentMetadata{FilePath: res.job.docPath, Metadata: meta}
			if processed%10 == 0 {
				logSyncProgress(processed, -1, stats, errorCount)
			}
			continue
		}

		if updateErr := s.UpdateDocument(processingCtx, res.job.docPath, res.content, meta); updateErr != nil {
			cancel()
			errorCount++
			if firstErr == nil {
				firstErr = fmt.Errorf("kb: update %s: %w", res.job.docPath, updateErr)
			}
			if processed%10 == 0 {
				logSyncProgress(processed, -1, stats, errorCount)
			}
			continue
		}
		cancel()
		stats.Updated++
		existingByPath[res.job.docPath] = documentMetadata{FilePath: res.job.docPath, Metadata: meta}

		if processed%10 == 0 {
			logSyncProgress(processed, -1, stats, errorCount)
		}
	}

	summary := <-summaryCh
	jobCount := summary.queued
	stats.Scanned = summary.scanned
	stats.Unchanged = summary.unchanged

	if walkErr := <-walkErrCh; firstErr == nil && walkErr != nil && walkErr != context.Canceled {
		firstErr = fmt.Errorf("kb: walk directory: %w", walkErr)
	}
	if firstErr != nil {
		logging.Debug("kb sync: failed",
			"elapsed", time.Since(syncStartedAt).String(),
			"processed", processed,
			"queued", jobCount,
			"scanned", stats.Scanned,
			"added", stats.Added,
			"updated", stats.Updated,
			"unchanged", stats.Unchanged,
			"errors", errorCount,
		)
		logSyncMemStats("kb sync: failed mem", processed, jobCount)
		return stats, firstErr
	}
	if processed != jobCount {
		return stats, fmt.Errorf("kb: sync pipeline mismatch: processed=%d queued=%d", processed, jobCount)
	}

	if deleteMissing {
		toDelete := make([]string, 0)
		for _, doc := range existingByPath {
			sourcePath := strings.TrimSpace(metadataString(doc.Metadata, "source_path"))
			if sourcePath == "" {
				continue
			}
			absSource, absErr := filepath.Abs(sourcePath)
			if absErr != nil {
				continue
			}
			if !isPathWithinBase(absSource, baseDir) {
				continue
			}
			if _, ok := seen[doc.FilePath]; ok {
				continue
			}
			toDelete = append(toDelete, doc.FilePath)
		}

		for _, docPath := range toDelete {
			if delErr := s.DeleteDocument(ctxSync, docPath); delErr != nil {
				errorCount++
				return stats, fmt.Errorf("kb: delete missing doc %s: %w", docPath, delErr)
			}
			stats.Deleted++
		}
	}

	logging.Debug("kb sync: completed",
		"elapsed", time.Since(syncStartedAt).String(),
		"scanned", stats.Scanned,
		"added", stats.Added,
		"updated", stats.Updated,
		"unchanged", stats.Unchanged,
		"deleted", stats.Deleted,
		"errors", errorCount,
	)
	logSyncMemStats("kb sync: completed mem", processed, jobCount)

	return stats, nil
}

func logSyncMemStats(prefix string, processed, queued int) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	logging.Debug(prefix,
		"processed", processed,
		"queued", queued,
		"alloc_mb", ms.Alloc/1024/1024,
		"heap_inuse_mb", ms.HeapInuse/1024/1024,
		"heap_sys_mb", ms.HeapSys/1024/1024,
		"num_gc", ms.NumGC,
		"goroutines", runtime.NumGoroutine(),
	)
}

func logSyncProgress(processed, queued int, stats SyncStats, errorCount int) {
	logging.Debug("kb sync: progress",
		"processed", processed,
		"queued", queued,
		"added", stats.Added,
		"updated", stats.Updated,
		"unchanged", stats.Unchanged,
		"deleted", stats.Deleted,
		"errors", errorCount,
	)
	logSyncMemStats("kb sync: progress mem", processed, queued)
}

func isMarkdownFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".md")
}

func normalizeDocPath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func metadataString(meta map[string]interface{}, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func metadataInt64(meta map[string]interface{}, key string) int64 {
	if meta == nil {
		return 0
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return 0
	}
	switch typed := v.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func isPathWithinBase(path string, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
