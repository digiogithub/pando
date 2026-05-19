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

		// Don't add duplicates by APIModel (handles cases where static model ID differs from dynamic)
		if modelExistsByAPIModel(provider, fm.ID) {
			continue
		}

		name := fm.Name
		if name == "" {
			name = fm.ID
		}

		contextWindow := fm.ContextWindow
		if contextWindow <= 0 {
			contextWindow = 128_000 // reasonable default
		}
		maxTokens := int64(4096) // reasonable default
		if contextWindow < maxTokens {
			maxTokens = contextWindow / 2
		}

		model := Model{
			ID:               modelID,
			Name:             fmt.Sprintf("%s: %s", capitalizeProvider(string(provider)), name),
			Provider:         provider,
			APIModel:         fm.ID,
			ContextWindow:    contextWindow,
			DefaultMaxTokens: maxTokens,
		}

		RegisterDynamicModel(model)
	}

	return nil
}

// modelExistsByAPIModel checks if a static model already exists for a given provider+apiModel combination
func modelExistsByAPIModel(provider ModelProvider, apiModel string) bool {
	for _, m := range SupportedModels {
		if m.Provider == provider && m.APIModel == apiModel {
			return true
		}
	}
	return false
}

func capitalizeProvider(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// AccountModelRefreshParams holds the parameters needed to refresh models for a named account.
type AccountModelRefreshParams struct {
	AccountID    string
	ProviderType ModelProvider
	APIKey       string
	BearerToken  string
	BaseURL      string
	ExtraHeaders map[string]string
	// AllAccountsOfType is the count of non-disabled accounts sharing ProviderType,
	// used to decide whether to prefix the model ID with AccountID.
	AllAccountsOfType int
}

// RefreshProviderModelsForAccount fetches and registers models for a named provider account.
// Model IDs are prefixed with accountID when AllAccountsOfType > 1 (disambiguates multiple accounts of same type).
func RefreshProviderModelsForAccount(ctx context.Context, params AccountModelRefreshParams) error {
	fetched, err := FetchModelsFromProvider(ctx, params.ProviderType, params.APIKey, params.BearerToken, params.BaseURL)
	if err != nil {
		return fmt.Errorf("fetch models from account %s (%s): %w", params.AccountID, params.ProviderType, err)
	}

	for _, fm := range fetched {
		model := modelFromFetchedAccountModel(params, fm)
		if shouldSkipAccountScopedModel(params.ProviderType, model.ID, model.APIModel) {
			continue
		}
		RegisterDynamicModel(model)
	}

	return nil
}

func modelFromFetchedAccountModel(params AccountModelRefreshParams, fetched FetchedModel) Model {
	prefix := dynamicModelPrefix(params.ProviderType, params.AccountID, params.AllAccountsOfType)
	modelID := ModelID(fmt.Sprintf("%s.%s", prefix, fetched.ID))
	name := fetchedModelName(fetched)
	contextWindow := fetchedModelContextWindow(fetched.ContextWindow)

	maxTokens := int64(4096)
	if contextWindow < maxTokens {
		maxTokens = contextWindow / 2
	}

	return Model{
		ID:               modelID,
		Name:             fmt.Sprintf("%s: %s", capitalizeProvider(string(params.ProviderType)), name),
		Provider:         params.ProviderType,
		APIModel:         fetched.ID,
		ContextWindow:    contextWindow,
		DefaultMaxTokens: maxTokens,
		AccountID:        params.AccountID,
	}
}

func dynamicModelPrefix(providerType ModelProvider, accountID string, allAccountsOfType int) string {
	if providerType == ProviderAntigravity {
		return string(providerType)
	}
	if allAccountsOfType > 1 {
		return accountID
	}
	return string(providerType)
}

func shouldSkipAccountScopedModel(providerType ModelProvider, modelID ModelID, apiModel string) bool {
	if _, exists := SupportedModels[modelID]; exists {
		return true
	}
	if providerType == ProviderAntigravity {
		return modelExistsByAPIModel(providerType, apiModel)
	}
	return false
}

func fetchedModelName(fetched FetchedModel) string {
	if fetched.Name != "" {
		return fetched.Name
	}
	return fetched.ID
}

func fetchedModelContextWindow(contextWindow int64) int64 {
	if contextWindow > 0 {
		return contextWindow
	}
	return 128_000
}

// GetAllModels returns both static and dynamic models
func GetAllModels() map[ModelID]Model {
	result := make(map[ModelID]Model, len(SupportedModels))
	for k, v := range SupportedModels {
		result[k] = v
	}
	return result
}
