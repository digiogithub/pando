package kb

import (
	"context"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// MemoryGCService periodically marks expired memory documents as outdated.
type MemoryGCService struct {
	store    *KBStore
	interval time.Duration
	done     chan struct{}
}

// NewMemoryGCService creates a new MemoryGCService.
func NewMemoryGCService(store *KBStore, interval time.Duration) *MemoryGCService {
	return &MemoryGCService{
		store:    store,
		interval: interval,
		done:     make(chan struct{}),
	}
}

// Start launches the GC loop in a background goroutine.
func (g *MemoryGCService) Start(ctx context.Context) {
	go g.run(ctx)
}

// Stop signals the background goroutine to exit.
func (g *MemoryGCService) Stop() {
	close(g.done)
}

func (g *MemoryGCService) run(ctx context.Context) {
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()

	// Run once immediately so the first GC cycle doesn't wait a full interval.
	if err := g.runOnce(ctx); err != nil {
		logging.Debug("memory gc: initial run error", "error", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := g.runOnce(ctx); err != nil {
				logging.Debug("memory gc: run error", "error", err)
			}
		case <-g.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (g *MemoryGCService) runOnce(ctx context.Context) error {
	expired, err := g.store.GetExpiredDocuments(ctx)
	if err != nil {
		return err
	}
	if len(expired) == 0 {
		return nil
	}

	marked := 0
	for _, ref := range expired {
		if err := g.store.MarkDocumentOutdated(ctx, ref.FilePath); err != nil {
			logging.Debug("memory gc: mark outdated error", "file_path", ref.FilePath, "error", err)
			continue
		}
		marked++
	}

	if marked > 0 {
		logging.Debug("memory gc: marked documents as outdated", "count", marked)
	}
	return nil
}
