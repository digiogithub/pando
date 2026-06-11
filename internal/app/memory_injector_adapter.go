package app

import (
	"context"

	"github.com/digiogithub/pando/internal/config"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// memoryInjectorAdapter satisfies agent.MemoryInjector without creating an import
// cycle between the agent and rag packages.
type memoryInjectorAdapter struct {
	store *kb.KBStore
	cfg   config.RemembrancesConfig
}

// BuildMemoryBlock delegates to rag.BuildMemoryBlock using the stored KB store and config.
func (m *memoryInjectorAdapter) BuildMemoryBlock(ctx context.Context, query string) string {
	return rag.BuildMemoryBlock(ctx, m.store, query, m.cfg)
}
