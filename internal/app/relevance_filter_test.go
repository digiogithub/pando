package app

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/rag"
)

type nopFilter struct{}

func (nopFilter) Filter(_ context.Context, _ string, c []rag.RelevanceCandidate) ([]bool, rag.FilterResult) {
	k := make([]bool, len(c))
	for i := range k {
		k[i] = true
	}
	return k, rag.FilterResult{Applied: true}
}

func TestRelevanceTargetsApplySetsAndUnsetsPerFlag(t *testing.T) {
	enricher := rag.NewContextEnricher(&rag.RemembrancesService{}, rag.EnricherConfig{})
	injector := &memoryInjectorAdapter{}
	targets := relevanceTargets{enricher: enricher, injector: injector}
	f := nopFilter{}

	targets.apply(config.RemembrancesConfig{MemoryContextDecisionFilterEnabled: true}, f)
	if injector.filter() == nil {
		t.Fatal("memory flag on must install the filter")
	}
	targets.apply(config.RemembrancesConfig{}, f)
	if injector.filter() != nil {
		t.Fatal("flag off must remove the filter (hot reload)")
	}
	// Nil targets are tolerated.
	relevanceTargets{}.apply(config.RemembrancesConfig{ContextEnrichmentDecisionFilterEnabled: true}, f)
}
