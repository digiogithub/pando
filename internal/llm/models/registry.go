package models

import (
	"context"
	"fmt"
	"sync"
	"unicode"
)

var (
	dynamicModels sync.Map // map[ModelID]Model
)

// RegisterDynamicModel adds a dynamically discovered model
func RegisterDynamicModel(model Model) {
	dynamicModels.Store(model.ID, model)
	SupportedModels[model.ID] = model
}

// RefreshProviderModels fetches and registers models from a provider
func RefreshProviderModels(ctx context.Context, provider ModelProvider, apiKey string, bearerToken string, baseURL string) error {
	fetched, err := FetchModelsFromProvider(ctx, provider, apiKey, bearerToken, baseURL)
	if err != nil {
		return fmt.Errorf("fetch models from %s: %w", provider, err)
	}

	for _, fm := range fetched {
		modelID := ModelID(fmt.Sprintf("%s.%s", provider, fm.ID))

		// Don't overwrite statically defined models
		if _, exists := SupportedModels[modelID]; exists {
			continue
		}

		name := fm.Name
		if name == "" {
			name = fm.ID
		}

		model := Model{
			ID:               modelID,
			Name:             fmt.Sprintf("%s: %s", capitalizeProvider(string(provider)), name),
			Provider:         provider,
			APIModel:         fm.ID,
			ContextWindow:    128_000, // reasonable default
			DefaultMaxTokens: 4096,    // reasonable default
		}

		RegisterDynamicModel(model)
	}

	return nil
}

func capitalizeProvider(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// GetAllModels returns both static and dynamic models
func GetAllModels() map[ModelID]Model {
	result := make(map[ModelID]Model, len(SupportedModels))
	for k, v := range SupportedModels {
		result[k] = v
	}
	return result
}
