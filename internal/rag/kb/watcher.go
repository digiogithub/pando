package kb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/fsnotify/fsnotify"
)

// WatchDirectory monitors a KB source directory recursively and keeps KB
// documents in sync with .md file changes in near real-time.
func (s *KBStore) WatchDirectory(ctx context.Context, dirPath string) error {
	baseDir, err := filepath.Abs(strings.TrimSpace(dirPath))
	if err != nil {
		return fmt.Errorf("kb: resolve watch directory %q: %w", dirPath, err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("kb: create watcher: %w", err)
	}
	defer watcher.Close()

	if err := addWatchRecursively(watcher, baseDir); err != nil {
		return err
	}

	logging.Debug("kb watcher: started", "path", baseDir)

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
			logging.Debug("kb watcher: stopped", "path", baseDir)
			return nil
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			logging.Warn("kb watcher: fsnotify error", "path", baseDir, "error", err)
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Newly created directories must be watched too.
			if event.Op&fsnotify.Create != 0 {
				if fi, statErr := os.Stat(event.Name); statErr == nil && fi.IsDir() {
					if err := addWatchRecursively(watcher, event.Name); err != nil {
						logging.Warn("kb watcher: add recursive watch failed", "path", event.Name, "error", err)
					}
					continue
				}
			}

			if !isMarkdownFile(event.Name) {
				continue
			}

			evt := event
			handleWithDebounce(event.Name, func() {
				s.handleWatchEvent(ctx, baseDir, evt)
			})
		}
	}
}

func (s *KBStore) handleWatchEvent(ctx context.Context, baseDir string, event fsnotify.Event) {
	absPath, err := filepath.Abs(event.Name)
	if err != nil {
		logging.Warn("kb watcher: abs path failed", "path", event.Name, "error", err)
		return
	}
	if !isPathWithinBase(absPath, baseDir) {
		return
	}

	rel, err := filepath.Rel(baseDir, absPath)
	if err != nil {
		logging.Warn("kb watcher: relative path failed", "path", absPath, "error", err)
		return
	}
	docPath := normalizeDocPath(rel)

	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		if err := s.DeleteDocument(ctx, docPath); err != nil {
			logging.Warn("kb watcher: delete from kb failed", "doc_path", docPath, "error", err)
		}
		return
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			if delErr := s.DeleteDocument(ctx, docPath); delErr != nil {
				logging.Warn("kb watcher: delete after missing file failed", "doc_path", docPath, "error", delErr)
			}
			return
		}
		logging.Warn("kb watcher: stat failed", "path", absPath, "error", err)
		return
	}
	if fi.IsDir() {
		return
	}

	contentBytes, err := os.ReadFile(absPath)
	if err != nil {
		logging.Warn("kb watcher: read failed", "path", absPath, "error", err)
		return
	}

	metadata := map[string]interface{}{
		"source_path":       absPath,
		"source_mtime_unix": fi.ModTime().Unix(),
	}

	existing, err := s.GetDocument(ctx, docPath)
	if err != nil {
		logging.Warn("kb watcher: get document failed", "doc_path", docPath, "error", err)
		return
	}

	content := string(contentBytes)
	if existing == nil {
		if err := s.AddDocument(ctx, docPath, content, metadata); err != nil {
			logging.Warn("kb watcher: add document failed", "doc_path", docPath, "error", err)
		}
		return
	}

	prevMTime := metadataInt64(existing.Metadata, "source_mtime_unix")
	if existing.Content == content && prevMTime == fi.ModTime().Unix() {
		return
	}

	if err := s.UpdateDocument(ctx, docPath, content, metadata); err != nil {
		logging.Warn("kb watcher: update document failed", "doc_path", docPath, "error", err)
	}
}

func addWatchRecursively(w *fsnotify.Watcher, root string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("kb: abs watch root %q: %w", root, err)
	}
	return filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if err := w.Add(path); err != nil {
			return fmt.Errorf("kb: watch directory %q: %w", path, err)
		}
		return nil
	})
}
