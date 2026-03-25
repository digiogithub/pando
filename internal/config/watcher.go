package config

import (
	"context"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/fsnotify/fsnotify"
)

// WatchConfigFile monitors path for changes and triggers a config reload when
// the file is written. A 200 ms debounce prevents double-events caused by
// editors that write files in two steps (truncate + write).
//
// The goroutine shuts down cleanly when ctx is cancelled.
func WatchConfigFile(ctx context.Context, path string) {
	if path == "" {
		return
	}

	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			logging.Error("config watcher: failed to create fsnotify watcher", "error", err)
			return
		}
		defer watcher.Close()

		if err := watcher.Add(path); err != nil {
			logging.Error("config watcher: failed to watch config file", "path", path, "error", err)
			return
		}

		logging.Debug("config watcher: watching config file", "path", path)

		var debounce *time.Timer

		for {
			select {
			case <-ctx.Done():
				if debounce != nil {
					debounce.Stop()
				}
				return

			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// Only react to write / create events.
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}

				// Debounce: reset the timer on every event within the window.
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(200*time.Millisecond, func() {
					logging.Debug("config watcher: change detected, reloading", "path", path)
					if err := Reload(); err != nil {
						logging.Error("config watcher: reload failed", "error", err)
						return
					}
					Bus.Publish(ConfigChangeEvent{
						Section:   "",
						Timestamp: time.Now(),
						Source:    "file",
					})
					logging.Debug("config watcher: reload complete")
				})

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logging.Error("config watcher: fsnotify error", "error", err)
			}
		}
	}()
}
