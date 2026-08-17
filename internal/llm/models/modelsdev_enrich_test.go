package models

import (
	"reflect"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models/modelsdev"
)

func catalogEntry() modelsdev.Model {
	return modelsdev.Model{
		ID:          "claude-sonnet-4-5",
		Description: "catalog description",
		Attachment:  true,
		Reasoning:   true,
		ReasoningOptions: []modelsdev.ReasoningOption{
			{Type: "effort", Values: []string{"low", "high"}},
		},
		Knowledge:   "2025-01",
		ReleaseDate: "2025-09-29",
		Cost:        modelsdev.Cost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
		Limit:       modelsdev.Limit{Context: 200000, Output: 64000},
	}
}

func TestApplyModelsDevMetadataFillsMissingFields(t *testing.T) {
	model := Model{ID: "copilot.claude-sonnet-4-5", Provider: ProviderCopilot, APIModel: "claude-sonnet-4-5"}
	applyModelsDevMetadata(&model, catalogEntry())

	if model.CostPer1MIn != 3 || model.CostPer1MOut != 15 {
		t.Errorf("costs = %v/%v, want 3/15", model.CostPer1MIn, model.CostPer1MOut)
	}
	// Pando's cached-cost naming is inverted relative to models.dev: "in cached"
	// is charged on cache creation (write), "out cached" on cache reads.
	if model.CostPer1MInCached != 3.75 {
		t.Errorf("CostPer1MInCached = %v, want the cache_write price", model.CostPer1MInCached)
	}
	if model.CostPer1MOutCached != 0.3 {
		t.Errorf("CostPer1MOutCached = %v, want the cache_read price", model.CostPer1MOutCached)
	}
	if model.ContextWindow != 200000 || model.DefaultMaxTokens != 64000 {
		t.Errorf("limits = %d/%d, want 200000/64000", model.ContextWindow, model.DefaultMaxTokens)
	}
	if !model.CanReason || !model.SupportsReasoningEffort || !model.SupportsAttachments {
		t.Error("capability flags should be raised from the catalog")
	}
	if model.Description != "catalog description" || model.Knowledge != "2025-01" || model.ReleaseDate != "2025-09-29" {
		t.Errorf("descriptive fields not filled: %+v", model)
	}
}

// Provider-reported metadata is authoritative: a stale catalog entry must never
// overwrite what Pando already knows.
func TestApplyModelsDevMetadataNeverOverwritesKnownValues(t *testing.T) {
	model := Model{
		Provider:           ProviderAnthropic,
		APIModel:           "claude-sonnet-4-5",
		CostPer1MIn:        1,
		CostPer1MOut:       2,
		CostPer1MInCached:  3,
		CostPer1MOutCached: 4,
		ContextWindow:      111,
		DefaultMaxTokens:   222,
		Description:        "kept",
	}
	applyModelsDevMetadata(&model, catalogEntry())

	want := Model{
		Provider: ProviderAnthropic, APIModel: "claude-sonnet-4-5",
		CostPer1MIn: 1, CostPer1MOut: 2, CostPer1MInCached: 3, CostPer1MOutCached: 4,
		ContextWindow: 111, DefaultMaxTokens: 222, Description: "kept",
		CanReason: true, SupportsReasoningEffort: true, SupportsAttachments: true,
		ReasoningEfforts: []string{"low", "high"},
		Knowledge:        "2025-01", ReleaseDate: "2025-09-29",
	}
	if !reflect.DeepEqual(model, want) {
		t.Errorf("got %+v, want %+v", model, want)
	}
}

// Capability flags are only ever raised: an incomplete catalog entry must not
// strip reasoning support the provider itself reported.
func TestApplyModelsDevMetadataOnlyRaisesCapabilities(t *testing.T) {
	model := Model{CanReason: true, SupportsAttachments: true, SupportsReasoningEffort: true}
	applyModelsDevMetadata(&model, modelsdev.Model{})
	if !model.CanReason || !model.SupportsAttachments || !model.SupportsReasoningEffort {
		t.Error("capabilities must not be cleared by an empty catalog entry")
	}
}

// With the catalog disabled (TestMain) enrichment is a no-op, which is exactly
// the offline/fallback behaviour.
func TestEnrichModelFromModelsDevIsNoOpWhenUnavailable(t *testing.T) {
	model := Model{Provider: ProviderAnthropic, APIModel: "claude-sonnet-4-5"}
	EnrichModelFromModelsDev(t.Context(), &model)
	if model.CostPer1MIn != 0 || model.ContextWindow != 0 {
		t.Errorf("no enrichment expected without a catalog: %+v", model)
	}
	// Must not panic on a nil model either.
	EnrichModelFromModelsDev(t.Context(), nil)
}

// Local runtimes are intentionally unmapped: importing hosted pricing for a
// same-named open-weights model would invent a cost that does not exist.
func TestLocalProvidersHaveNoCatalogMapping(t *testing.T) {
	for _, p := range []ModelProvider{ProviderOllama, ProviderLlamaCpp, ProviderLocal, ProviderOpenAICompatible} {
		if _, ok := modelsDevProviders[p]; ok {
			t.Errorf("provider %s must not be mapped to a models.dev catalog", p)
		}
	}
}
