package app

import (
	"context"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/config"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// memoryInjectorAdapter satisfies agent.MemoryInjector without creating an import
// cycle between the agent and rag packages.
type memoryInjectorAdapter struct {
	store *kb.KBStore
	cfg   config.RemembrancesConfig
	// relevance is the optional decision-model filter (nil = off), swapped on hot reload.
	relevance atomic.Pointer[memoryRelevanceHolder]
}

type memoryRelevanceHolder struct{ f rag.RelevanceFilter }

// SetRelevanceFilter installs (or, with nil, removes) the relevance filter
// applied to the memories before they are injected.
func (m *memoryInjectorAdapter) SetRelevanceFilter(f rag.RelevanceFilter) {
	if f == nil {
		m.relevance.Store(nil)
		return
	}
	m.relevance.Store(&memoryRelevanceHolder{f: f})
}

func (m *memoryInjectorAdapter) filter() rag.RelevanceFilter {
	if h := m.relevance.Load(); h != nil {
		return h.f
	}
	return nil
}

// BuildMemoryBlock delegates to rag.BuildMemoryBlock using the stored KB store and config.
func (m *memoryInjectorAdapter) BuildMemoryBlock(ctx context.Context, query string) string {
	out, _ := m.BuildMemoryBlockWithResult(ctx, query)
	return out
}

// BuildMemoryBlockWithResult is BuildMemoryBlock that also reports what the
// relevance filter did.
func (m *memoryInjectorAdapter) BuildMemoryBlockWithResult(ctx context.Context, query string) (string, rag.FilterResult) {
	return rag.BuildMemoryBlockWithResult(ctx, m.store, query, m.cfg, m.filter())
}
