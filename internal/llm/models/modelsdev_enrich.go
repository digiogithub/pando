package models

import (
	"context"

	"github.com/digiogithub/pando/internal/llm/models/modelsdev"
)

// modelsDevProviders maps a Pando provider type to the models.dev provider ids
// that can describe its models, in preference order. Providers that front more
// than one upstream catalog (Antigravity, Bedrock, Vertex AI) list several.
//
// Local runtimes (Ollama, llama.cpp, …) are deliberately absent: their models
// are free to run, so importing a hosted provider's pricing for a same-named
// open-weights model would show an invented cost. They still gain nothing from
// the catalog and keep their current behaviour.
var modelsDevProviders = map[ModelProvider][]string{
	ProviderAnthropic:   {"anthropic"},
	ProviderOpenAI:      {"openai"},
	ProviderGemini:      {"google"},
	ProviderVertexAI:    {"google-vertex", "google-vertex-anthropic", "google"},
	ProviderAzure:       {"azure", "openai"},
	ProviderCopilot:     {"github-copilot"},
	ProviderGROQ:        {"groq"},
	ProviderOpenRouter:  {"openrouter"},
	ProviderXAI:         {"xai"},
	ProviderBedrock:     {"amazon-bedrock", "anthropic"},
	ProviderAntigravity: {"google", "anthropic", "openai"},
}

// ModelsDevMetadata returns the models.dev entry describing a provider/model
// pair. ok is false whenever the catalog is unavailable or the model is
// unknown, which every caller must treat as "keep what you already have".
func ModelsDevMetadata(ctx context.Context, provider ModelProvider, apiModel string) (modelsdev.Model, bool) {
	providerIDs, ok := modelsDevProviders[provider]
	if !ok || apiModel == "" {
		return modelsdev.Model{}, false
	}
	catalog, err := modelsdev.Get(ctx)
	if err != nil {
		return modelsdev.Model{}, false
	}
	return catalog.LookupAny(providerIDs, apiModel)
}

// EnrichModelFromModelsDev fills in metadata the provider's own listing API did
// not report — most importantly per-token costs, which almost no provider
// returns and without which the session cost panel stays empty.
//
// It only ever writes fields that are currently zero/empty: whatever the
// provider (or Pando's curated catalog) already stated stays authoritative, so
// a stale or wrong catalog entry can never downgrade known-good metadata.
// Boolean capability flags are only raised, never cleared, for the same reason.
func EnrichModelFromModelsDev(ctx context.Context, model *Model) {
	if model == nil {
		return
	}
	entry, ok := ModelsDevMetadata(ctx, model.Provider, model.APIModel)
	if !ok {
		return
	}
	applyModelsDevMetadata(model, entry)
}

// EnrichRegisteredModels tops up every model already in the registry with the
// catalog data it is missing. Curated static entries (Antigravity, Bedrock,
// Vertex AI, …) are hand-written without prices, so without this pass they would
// keep reporting a zero session cost even though the catalog knows their rates.
//
// Safe to call repeatedly; it is a no-op when the catalog is unavailable.
func EnrichRegisteredModels(ctx context.Context) {
	if _, err := modelsdev.Get(ctx); err != nil {
		return
	}
	for id, model := range SupportedModels {
		entry, ok := ModelsDevMetadata(ctx, model.Provider, model.APIModel)
		if !ok || !applyModelsDevMetadata(&model, entry) {
			continue
		}
		SupportedModels[id] = model
		if _, dynamic := dynamicModels.Load(id); dynamic {
			dynamicModels.Store(id, model)
		}
	}
}

// applyModelsDevMetadata is the pure part of the enrichment, split out so it can
// be tested without touching the network. It reports whether anything changed.
func applyModelsDevMetadata(model *Model, entry modelsdev.Model) bool {
	before := enrichableFields(*model)

	if model.CostPer1MIn == 0 {
		model.CostPer1MIn = entry.Cost.Input
	}
	if model.CostPer1MOut == 0 {
		model.CostPer1MOut = entry.Cost.Output
	}
	// Pando's naming is inverted relative to models.dev: CostPer1MInCached is
	// the price of *writing* the cache (a prompt-side cost) and
	// CostPer1MOutCached the price of *reading* it back. See TrackUsage in
	// internal/llm/agent/agent.go, which multiplies them by CacheCreationTokens
	// and CacheReadTokens respectively.
	if model.CostPer1MInCached == 0 {
		model.CostPer1MInCached = entry.Cost.CacheWrite
	}
	if model.CostPer1MOutCached == 0 {
		model.CostPer1MOutCached = entry.Cost.CacheRead
	}
	if model.ContextWindow == 0 && entry.Limit.Context > 0 {
		model.ContextWindow = entry.Limit.Context
	}
	if model.DefaultMaxTokens == 0 && entry.Limit.Output > 0 {
		model.DefaultMaxTokens = entry.Limit.Output
	}
	if entry.Reasoning {
		model.CanReason = true
	}
	if entry.SupportsReasoningEffort() {
		model.SupportsReasoningEffort = true
	}
	if entry.Attachment {
		model.SupportsAttachments = true
	}
	if model.Description == "" {
		model.Description = entry.Description
	}
	if model.Knowledge == "" {
		model.Knowledge = entry.Knowledge
	}
	if model.ReleaseDate == "" {
		model.ReleaseDate = entry.ReleaseDate
	}

	return before != enrichableFields(*model)
}

// enrichable is the comparable projection of the fields enrichment can write.
// Model itself holds a slice and is therefore not comparable.
type enrichable struct {
	costIn, costOut, costInCached, costOutCached float64
	contextWindow, maxTokens                     int64
	canReason, effort, attachments               bool
	description, knowledge, releaseDate          string
}

func enrichableFields(m Model) enrichable {
	return enrichable{
		costIn: m.CostPer1MIn, costOut: m.CostPer1MOut,
		costInCached: m.CostPer1MInCached, costOutCached: m.CostPer1MOutCached,
		contextWindow: m.ContextWindow, maxTokens: m.DefaultMaxTokens,
		canReason: m.CanReason, effort: m.SupportsReasoningEffort, attachments: m.SupportsAttachments,
		description: m.Description, knowledge: m.Knowledge, releaseDate: m.ReleaseDate,
	}
}
