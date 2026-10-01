package app

import (
	"context"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag"
)

// relevanceTargets are the injection points that accept a relevance filter.
type relevanceTargets struct {
	enricher *rag.ContextEnricher
	injector *memoryInjectorAdapter
}

// apply installs the decision-model relevance filter on every target whose flag
// is on and removes it from the others. The filter reads the decision model and
// thresholds from the live config on each call, so one instance serves every
// reload.
func (t relevanceTargets) apply(rem config.RemembrancesConfig, f rag.RelevanceFilter) {
	if t.enricher != nil {
		if rem.ContextEnrichmentDecisionFilterEnabled {
			t.enricher.SetRelevanceFilter(f)
		} else {
			t.enricher.SetRelevanceFilter(nil)
		}
	}
	if t.injector != nil {
		if rem.MemoryContextDecisionFilterEnabled {
			t.injector.SetRelevanceFilter(f)
		} else {
			t.injector.SetRelevanceFilter(nil)
		}
	}
}

// startRelevanceFilterWiring applies the filter now and keeps it in sync with
// config changes (remembrances and decision model sections) until ctx ends.
func startRelevanceFilterWiring(ctx context.Context, t relevanceTargets) {
	f := modelrouter.NewRelevanceFilter(modelrouter.RelevanceOptions{})
	reload := func() {
		if c := config.Get(); c != nil {
			t.apply(c.Remembrances, f)
		}
	}
	reload()
	ch := make(chan config.ConfigChangeEvent, 16)
	config.Bus.Subscribe(ch)
	go func() {
		defer config.Bus.Unsubscribe(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				switch ev.Section {
				case "", "remembrances", "decisionModel":
					reload()
					logging.Debug("remembrances: relevance filter reloaded", "section", ev.Section)
				}
			}
		}
	}()
}

// Compile-time checks: the adapters report the filter result to the agent.
var (
	_ agent.FilteredContextEnricher        = (*rag.ContextEnricher)(nil)
	_ agent.FilteredSessionContextEnricher = (*agentLoopEnricher)(nil)
	_ agent.FilteredMemoryInjector         = (*memoryInjectorAdapter)(nil)
)
