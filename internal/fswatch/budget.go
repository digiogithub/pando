package fswatch

import (
	"runtime"
	"sync"

	"github.com/digiogithub/pando/internal/logging"
)

// WarnThreshold is the estimated file-descriptor cost above which a watcher
// logs a one-time warning.
const WarnThreshold = 10000

// EstimateCost returns the estimated number of file descriptors (or kernel
// watches) needed to watch dirs directories holding files regular files.
// kqueue platforms (macOS, BSD) spend one descriptor per directory AND per
// file; inotify (Linux) and others only count directories.
func EstimateCost(goos string, dirs, files int) int {
	switch goos {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly", "ios":
		return dirs + files
	}
	return dirs
}

// WatchBudget tracks the watch cost of one watcher instance and warns once
// when it exceeds the threshold. It is safe for concurrent use.
type WatchBudget struct {
	name string
	root string

	// Overridable for tests.
	goos      string
	threshold int
	warn      func(msg string, args ...any)

	mu     sync.Mutex
	dirs   int
	files  int
	warned bool
}

// NewWatchBudget creates a budget for the named watcher ("lsp-workspace",
// "code-index", ...) rooted at root.
func NewWatchBudget(name, root string) *WatchBudget {
	return &WatchBudget{
		name:      name,
		root:      root,
		goos:      runtime.GOOS,
		threshold: WarnThreshold,
		warn:      logging.Warn,
	}
}

// AddDir records one watched directory. Nil-safe.
func (b *WatchBudget) AddDir() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.dirs++
	b.mu.Unlock()
}

// AddFile records one file living directly inside a watched directory.
// Nil-safe.
func (b *WatchBudget) AddFile() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.files++
	b.mu.Unlock()
}

// Cost returns the current estimated FD cost.
func (b *WatchBudget) Cost() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return EstimateCost(b.goos, b.dirs, b.files)
}

// Check logs a single warning (per budget) when the estimate exceeds the
// threshold. It returns true when the warning was emitted by this call.
func (b *WatchBudget) Check() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	cost := EstimateCost(b.goos, b.dirs, b.files)
	if b.warned || cost <= b.threshold {
		b.mu.Unlock()
		return false
	}
	b.warned = true
	dirs, files := b.dirs, b.files
	b.mu.Unlock()

	b.warn("filesystem watcher registers many paths; this may exhaust file descriptors. "+
		"Add heavy generated folders to WatchExclude in .pando.toml or to .gitignore/.pandoignore",
		"watcher", b.name,
		"root", b.root,
		"dirs", dirs,
		"files", files,
		"estimated_fds", cost,
		"threshold", b.threshold,
	)
	return true
}
