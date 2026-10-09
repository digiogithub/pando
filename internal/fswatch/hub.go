package fswatch

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/fileutil"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/fsnotify/fsnotify"
)

// subscriberBuffer is the per-subscriber event queue size. A subscriber that
// falls this far behind starts losing events instead of stalling the hub.
const subscriberBuffer = 1024

// Hub owns ONE recursive fsnotify watcher for a workspace and fans every event
// out to any number of subscribers.
//
// On macOS (kqueue) fsnotify spends one file descriptor per watched directory
// and per file inside it, so each extra full-tree watcher multiplies the FD
// cost. Sharing one watcher keeps that cost constant regardless of how many
// consumers (LSP clients, the bootstrap watcher, ...) exist.
//
// Lifecycle: the underlying watcher is started lazily by the first Subscribe
// and keeps running until Close (app shutdown), even if every subscriber
// leaves. LSP restarts unsubscribe and resubscribe in quick succession, and
// tearing the tree down and re-walking it each time would be wasteful.
type Hub struct {
	root     string
	excluder *Excluder

	mu      sync.Mutex
	started bool
	closed  bool
	cancel  context.CancelFunc
	done    chan struct{}
	subs    map[uint64]*Subscription
	nextID  uint64

	watched atomic.Int64
	budget  *WatchBudget
}

// Subscription receives the events of a Hub until Close is called.
type Subscription struct {
	// Events delivers every filesystem event the hub observes. It is closed
	// when the subscription is closed or the hub is shut down.
	Events <-chan fsnotify.Event

	ch      chan fsnotify.Event
	hub     *Hub
	id      uint64
	once    sync.Once
	dropped atomic.Int64
}

// NewHub creates a hub for root. excluder decides which directories are never
// watched; when nil one is built from the defaults.
func NewHub(root string, excluder *Excluder) *Hub {
	if excluder == nil {
		excluder = NewExcluder(root, nil)
	}
	return &Hub{root: root, excluder: excluder, subs: make(map[uint64]*Subscription), budget: NewWatchBudget("lsp-workspace", root)}
}

// Excluder returns the directory excluder used by the hub.
func (h *Hub) Excluder() *Excluder { return h.excluder }

// Root returns the watched workspace root.
func (h *Hub) Root() string { return h.root }

// PathCount returns how many directories are currently registered with the
// underlying fsnotify watcher (kept in sync on removals).
func (h *Hub) PathCount() int { return int(h.watched.Load()) }

// SubscriberCount returns the number of active subscriptions.
func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Subscribe registers a new subscriber, starting the shared watcher on first
// use. It returns nil when the hub is closed.
func (h *Hub) Subscribe() *Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.nextID++
	ch := make(chan fsnotify.Event, subscriberBuffer)
	s := &Subscription{Events: ch, ch: ch, hub: h, id: h.nextID}
	h.subs[s.id] = s
	if !h.started {
		h.started = true
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		h.done = make(chan struct{})
		go h.run(ctx)
	}
	return s
}

// Dropped returns how many events were discarded because the subscriber was
// too slow.
func (s *Subscription) Dropped() int64 { return s.dropped.Load() }

// Close unsubscribes; no further events are delivered and Events is closed.
func (s *Subscription) Close() {
	s.once.Do(func() {
		s.hub.mu.Lock()
		delete(s.hub.subs, s.id)
		// Closing under the hub lock guarantees broadcast (which also holds
		// it) never sends on a closed channel.
		close(s.ch)
		s.hub.mu.Unlock()
	})
}

// Close stops the shared watcher and closes every subscription. It blocks
// until the watcher goroutine has released its file descriptors.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	cancel, done := h.cancel, h.done
	subs := make([]*Subscription, 0, len(h.subs))
	for _, s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()

	if cancel != nil {
		cancel()
		<-done
	}
	for _, s := range subs {
		s.Close()
	}
}

func (h *Hub) broadcast(ev fsnotify.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.subs {
		select {
		case s.ch <- ev:
		default:
			if s.dropped.Add(1) == 1 {
				logging.Warn("fswatch hub: subscriber too slow, dropping events", "root", h.root)
			}
		}
	}
}

func (h *Hub) add(w *fsnotify.Watcher, dir string) {
	if err := w.Add(dir); err != nil {
		logging.Error("Error watching path", "path", dir, "error", err)
		return
	}
	h.watched.Add(1)
	h.budget.AddDir()
}

// addTree watches dir and every non-excluded directory beneath it.
func (h *Hub) addTree(w *fsnotify.Watcher, dir string) {
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Unreadable or vanished entries must not abort the walk.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			// Files directly inside a watched directory cost one FD each on kqueue.
			h.budget.AddFile()
			return nil
		}
		if path != h.root && h.excluder.ShouldSkipDir(path) {
			return filepath.SkipDir
		}
		h.add(w, path)
		return nil
	})
	if err != nil {
		logging.Error("Error walking workspace", "error", err)
	}
	h.budget.Check()
}

func (h *Hub) run(ctx context.Context) {
	defer close(h.done)
	defer logging.RecoverPanic("fswatch-hub", nil)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		// Do not panic when the OS refuses a new watcher (e.g. FD exhaustion).
		logging.Error("Error creating workspace watcher", "error", err)
		return
	}
	defer w.Close()

	// Only watch recursively when the workspace is a recognised project
	// directory. Skipping the walk avoids registering thousands of watches
	// when pando is started from the home directory or the filesystem root.
	if !fileutil.IsSafeWorkingDirectory(h.root) {
		logging.Debug("workspace watcher: skipping recursive walk - not a project directory", "path", h.root)
		// Still watch the top-level directory so events for files created
		// directly in the workspace root are delivered.
		h.add(w, h.root)
	} else {
		h.addTree(w, h.root)
	}
	logging.Debug("Shared workspace watcher started", "root", h.root, "paths", h.PathCount())

	recursive := fileutil.IsSafeWorkingDirectory(h.root)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			// Keep the watch set in sync with new directories.
			if recursive && ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() && !h.excluder.ShouldSkipDir(ev.Name) {
					h.addTree(w, ev.Name)
				}
			}
			// fsnotify drops watches of removed directories itself; resync the
			// counter so PathCount stays accurate.
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				h.watched.Store(int64(len(w.WatchList())))
			}
			h.broadcast(ev)
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			logging.Error("Error watching file", "error", err)
		}
	}
}
