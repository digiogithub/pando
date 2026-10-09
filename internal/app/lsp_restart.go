package app

import (
	"context"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp"
)

const (
	// lspMaxRestarts is how many times one LSP client may be restarted within
	// lspRestartWindow before it is marked broken and no longer restarted.
	lspMaxRestarts   = 3
	lspRestartWindow = 5 * time.Minute
)

// lspWatcherEntry tracks the watcher goroutine of one LSP client so it can be
// stopped when the client is restarted.
type lspWatcherEntry struct {
	cancel context.CancelFunc
	// parent is the context the watcher context was derived from; a restart
	// must use it because cancelling the watcher also cancels its own context.
	parent context.Context
}

// restartLimiter bounds restarts per client within a sliding time window.
type restartLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string][]time.Time
}

func newRestartLimiter(max int, window time.Duration) *restartLimiter {
	return &restartLimiter{max: max, window: window, attempts: make(map[string][]time.Time)}
}

// allow records a restart attempt for name and reports whether it is permitted.
func (l *restartLimiter) allow(name string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.attempts[name][:0]
	for _, t := range l.attempts[name] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.attempts[name] = kept
		return false
	}
	l.attempts[name] = append(kept, now)
	return true
}

// disposeLSPClient shuts an LSP client down completely: shutdown request,
// exit notification, then Close, which releases the pipes and reaps (or kills)
// the server process. Without it every restart leaks a process and its FDs.
func disposeLSPClient(c *lsp.Client) {
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		logging.Debug("LSP shutdown request failed during dispose", "error", err)
	}
	if err := c.Exit(ctx); err != nil {
		logging.Debug("LSP exit notification failed during dispose", "error", err)
	}
	if err := c.Close(); err != nil {
		logging.Debug("LSP close reported an error during dispose", "error", err)
	}
}
