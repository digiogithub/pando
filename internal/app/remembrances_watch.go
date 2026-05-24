package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/treesitter"
	"github.com/fsnotify/fsnotify"
)

func (app *App) watchIndexedProject(ctx context.Context, svc *rag.RemembrancesService, projectID, rootPath, startupMode string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("remembrances code: create watcher: %w", err)
	}
	defer watcher.Close()

	if err := addProjectWatchRecursively(watcher, rootPath); err != nil {
		return err
	}

	logging.Info("remembrances code: filesystem watching enabled",
		"project_id", projectID,
		"path", rootPath,
		"startup_mode", startupMode,
	)

	var (
		timersMu sync.Mutex
		timers   = map[string]*time.Timer{}
	)

	handleWithDebounce := func(path string, fn func()) {
		timersMu.Lock()
		defer timersMu.Unlock()

		if t, ok := timers[path]; ok {
			t.Stop()
		}
		timers[path] = time.AfterFunc(250*time.Millisecond, func() {
			fn()
			timersMu.Lock()
			delete(timers, path)
			timersMu.Unlock()
		})
	}

	for {
		select {
		case <-ctx.Done():
			timersMu.Lock()
			for _, t := range timers {
				t.Stop()
			}
			timersMu.Unlock()
			return nil
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			logging.Warn("remembrances code: fsnotify error",
				"project_id", projectID,
				"path", rootPath,
				"error", err,
			)
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			if event.Op&fsnotify.Create != 0 {
				if fi, statErr := os.Stat(event.Name); statErr == nil && fi.IsDir() {
					if err := addProjectWatchRecursively(watcher, event.Name); err != nil {
						logging.Warn("remembrances code: add recursive watch failed",
							"project_id", projectID,
							"path", event.Name,
							"error", err,
						)
					}
					continue
				}
			}

			absPath, err := filepath.Abs(event.Name)
			if err != nil || !isProjectPathWithinBase(absPath, rootPath) {
				continue
			}
			if !treesitter.IsSupportedFile(absPath) {
				continue
			}

			evt := event
			handleWithDebounce(absPath, func() {
				app.handleIndexedProjectEvent(ctx, svc, projectID, rootPath, evt, startupMode)
			})
		}
	}
}

func (app *App) handleIndexedProjectEvent(ctx context.Context, svc *rag.RemembrancesService, projectID, rootPath string, event fsnotify.Event, startupMode string) {
	absPath, err := filepath.Abs(event.Name)
	if err != nil {
		return
	}

	relPath, err := filepath.Rel(rootPath, absPath)
	if err != nil {
		relPath = absPath
	}
	relPath = filepath.Clean(relPath)
	if relPath == "." || strings.HasPrefix(relPath, "..") {
		return
	}

	deleteCtx := context.Background()
	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		if err := svc.Code.DeleteFile(deleteCtx, projectID, relPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			logging.Warn("remembrances code: delete indexed file failed",
				"project_id", projectID,
				"file", relPath,
				"startup_mode", startupMode,
				"error", err,
			)
		}
		return
	}

	if _, statErr := os.Stat(absPath); statErr != nil {
		if os.IsNotExist(statErr) {
			if err := svc.Code.DeleteFile(deleteCtx, projectID, relPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				logging.Warn("remembrances code: delete missing indexed file failed",
					"project_id", projectID,
					"file", relPath,
					"startup_mode", startupMode,
					"error", err,
				)
			}
		}
		return
	}

	if err := svc.Code.ReindexFile(ctx, projectID, relPath); err != nil {
		logging.Warn("remembrances code: reindex file failed",
			"project_id", projectID,
			"file", relPath,
			"startup_mode", startupMode,
			"error", err,
		)
	}
}

func addProjectWatchRecursively(w *fsnotify.Watcher, root string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("remembrances code: abs watch root %q: %w", root, err)
	}

	return filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}

		name := d.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" || name == "__pycache__" {
			if path != rootAbs {
				return filepath.SkipDir
			}
		}

		if err := w.Add(path); err != nil {
			return fmt.Errorf("remembrances code: watch directory %q: %w", path, err)
		}
		return nil
	})
}

func isProjectPathWithinBase(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != "")
}
