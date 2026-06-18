package models

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"
)

var (
	dynamicModels sync.Map // map[ModelID]Model
)

// RegisterDynamicModel adds a dynamically discovered model.
func RegisterDynamicModel(model Model) {
	dynamicModels.Store(model.ID, model)
	SupportedModels[model.ID] = model
}

// pruneDynamicModelsForProvider removes dynamic models whose provider matches
// but whose ID is not in keepIDs. Call this after a successful provider fetch
// so that deleted models (e.g. removed from Ollama) disappear from the registry.
func pruneDynamicModelsForProvider(provider ModelProvider, keepIDs map[ModelID]struct{}) {
	dynamicModels.Range(func(key, _ any) bool {
		id := key.(ModelID)
		m, ok := dynamicModels.Load(id)
		if !ok {
			return true
		}
		if m.(Model).Provider != provider {
			return true
		}
		if _, keep := keepIDs[id]; !keep {
			dynamicModels.Delete(id)
			delete(SupportedModels, id)
		}
		return true
	})
}

// pruneDynamicModelsForAccount removes dynamic models belonging to a specific
// provider account whose ID is no longer returned by the latest fetch. Unlike
// pruneDynamicModelsForProvider it is scoped by AccountID, so refreshing one
// account does not delete the models of another account of the same provider
// type (e.g. a global and a project-level Anthropic account). It also clears
// stale entries left over when an account transitions between prefixed and
// unprefixed IDs (the AccountID still matches even though the ID changed).
func pruneDynamicModelsForAccount(provider ModelProvider, accountID string, keepIDs map[ModelID]struct{}) {
	dynamicModels.Range(func(key, _ any) bool {
		id := key.(ModelID)
		mv, ok := dynamicModels.Load(id)
		if !ok {
			return true
		}
		m := mv.(Model)
		if m.Provider != provider || m.AccountID != accountID {
			return true
		}
		if _, keep := keepIDs[id]; !keep {
			dynamicModels.Delete(id)
			delete(SupportedModels, id)
		}
		return true
	})
}

// RefreshProviderModels fetches and registers models from a provider.
// Models previously registered for this provider that are no longer returned
// by the API are removed from the registry and cache.
func RefreshProviderModels(ctx context.Context, provider ModelProvider, apiKey string, bearerToken string, baseURL string) error {
	fetched, err := FetchModelsFromProvider(ctx, provider, apiKey, bearerToken, baseURL)
	if err != nil {
		return fmt.Errorf("fetch models from %s: %w", provider, err)
	}

	keepIDs := make(map[ModelID]struct{}, len(fetched))
	for _, fm := range fetched {
		modelID := ModelID(fmt.Sprintf("%s.%s", provider, fm.ID))

		// Don't overwrite statically defined models
		if _, exists := SupportedModels[modelID]; exists {
			keepIDs[modelID] = struct{}{}
			continue
		}

		// Don't add duplicates by APIModel (handles cases where static model ID differs from dynamic)
		if modelExistsByAPIModel(provider, fm.ID) {
			keepIDs[modelID] = struct{}{}
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
		keepIDs[modelID] = struct{}{}
	}

	pruneDynamicModelsForProvider(provider, keepIDs)
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
// Models previously registered for this account that are no longer returned by the API are removed.
func RefreshProviderModelsForAccount(ctx context.Context, params AccountModelRefreshParams) error {
	// Providers without a model-listing API (Azure, Vertex AI, …) expose a static
	// catalog only. With multiple accounts of such a type, register per-account
	// copies of those static models so each account is independently selectable
	// and resolves to its own credentials — otherwise selection silently falls
	// back to the first account, exactly like the account-less static models did
	// for the listing-capable providers.
	if !ProviderSupportsModelListing(params.ProviderType) {
		keepIDs := make(map[ModelID]struct{})
		for _, m := range AccountScopedStaticModels(params.ProviderType, params.AccountID, params.AllAccountsOfType) {
			RegisterDynamicModel(m)
			keepIDs[m.ID] = struct{}{}
		}
		pruneDynamicModelsForAccount(params.ProviderType, params.AccountID, keepIDs)
		return nil
	}

	fetched, err := FetchModelsFromProvider(ctx, params.ProviderType, params.APIKey, params.BearerToken, params.BaseURL)
	if err != nil {
		return fmt.Errorf("fetch models from account %s (%s): %w", params.AccountID, params.ProviderType, err)
	}

	keepIDs := make(map[ModelID]struct{}, len(fetched))
	for _, fm := range fetched {
		model := modelFromFetchedAccountModel(params, fm)
		keepIDs[model.ID] = struct{}{}
		if shouldSkipAccountScopedModel(params.ProviderType, model.ID, model.APIModel) {
			continue
		}
		RegisterDynamicModel(model)
	}

	// Scope pruning to this account so refreshing it does not wipe out the
	// models of another account that shares the same provider type.
	pruneDynamicModelsForAccount(params.ProviderType, params.AccountID, keepIDs)
	return nil
}

func modelFromFetchedAccountModel(params AccountModelRefreshParams, fetched FetchedModel) Model {
	prefix := dynamicModelPrefix(params.ProviderType, params.AccountID, params.AllAccountsOfType)
	modelID := ModelID(fmt.Sprintf("%s.%s", prefix, fetched.ID))

	// When a statically-defined model exists for this provider+APIModel (e.g. the
	// curated Anthropic catalogue), inherit its rich metadata — reasoning support,
	// costs, context window, attachments — and override only the identity fields.
	// Otherwise per-account copies would lose "thinking" mode and cost data, which
	// matters once the ambiguous account-less static entries are hidden from the
	// selectors for multi-account provider types.
	if static, ok := staticModelByAPIModel(params.ProviderType, fetched.ID); ok {
		static.ID = modelID
		static.Provider = params.ProviderType
		static.APIModel = fetched.ID
		static.AccountID = params.AccountID
		return static
	}

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

// staticModelByAPIModel returns a copy of the statically-defined model for the
// given provider and API model identifier, if one exists. Only static models are
// considered (dynamic ones carry an AccountID), so this is a stable source of
// curated metadata regardless of refresh ordering.
func staticModelByAPIModel(provider ModelProvider, apiModel string) (Model, bool) {
	for _, m := range SupportedModels {
		if m.AccountID == "" && m.Provider == provider && m.APIModel == apiModel {
			return m, true
		}
	}
	return Model{}, false
}

// StaticModelsForProvider returns copies of every statically-defined (account-less)
// model for a provider type (exported for listing handlers).
func StaticModelsForProvider(provider ModelProvider) []Model {
	var out []Model
	for _, m := range SupportedModels {
		if m.AccountID == "" && m.Provider == provider {
			out = append(out, m)
		}
	}
	return out
}

// AccountScopedStaticModels returns per-account copies of a provider's static
// models, with account-prefixed IDs and AccountID set. It returns nothing when
// fewer than two accounts of the type exist: with a single account the prefix
// equals the provider type, so the prefixed ID would collide with the static one
// and the account-less static models (which already resolve to that single
// account) are used as-is. Used both to register the models (RefreshProviderModels-
// ForAccount) and to list them (the /api/v1/models handler) for providers that
// have no model-listing API.
func AccountScopedStaticModels(provider ModelProvider, accountID string, allAccountsOfType int) []Model {
	if allAccountsOfType <= 1 {
		return nil
	}
	prefix := dynamicModelPrefix(provider, accountID, allAccountsOfType)
	statics := StaticModelsForProvider(provider)
	out := make([]Model, 0, len(statics))
	for _, sm := range statics {
		if sm.APIModel == "" {
			continue
		}
		m := sm
		m.ID = ModelID(fmt.Sprintf("%s.%s", prefix, sm.APIModel))
		m.AccountID = accountID
		out = append(out, m)
	}
	return out
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

// CanonicalAccountModelID returns the registry model ID for a model fetched from a
// provider account, matching exactly the IDs produced by
// RefreshProviderModelsForAccount (i.e. "<prefix>.<apiModelID>"). Callers that list
// models for selection must use this so the IDs they expose are the same ones the
// rest of the system (model cache, agent validation, TUI) recognises. Using a bare,
// non-prefixed ID causes validateAgent to reject the saved agent model on the next
// config reload and silently revert it to a provider default.
func CanonicalAccountModelID(providerType ModelProvider, accountID string, allAccountsOfType int, apiModelID string) ModelID {
	prefix := dynamicModelPrefix(providerType, accountID, allAccountsOfType)
	return ModelID(fmt.Sprintf("%s.%s", prefix, apiModelID))
}

// ResolveModelID maps a possibly non-canonical model ID to a registered model ID.
// It returns (id, true) when the input is already registered, or when exactly one
// registered model matches it by APIModel or by a "<provider>.<input>" suffix.
// Otherwise it returns (input, false). This lets callers repair legacy/bare model
// IDs (e.g. "gpt-5.4-mini") to their canonical form ("copilot.gpt-5.4-mini")
// instead of discarding the user's selection.
func ResolveModelID(input ModelID) (ModelID, bool) {
	if input == "" {
		return input, false
	}
	if _, ok := SupportedModels[input]; ok {
		return input, true
	}

	suffix := "." + string(input)
	var match ModelID
	count := 0
	for id, m := range SupportedModels {
		if m.APIModel == string(input) || strings.HasSuffix(string(id), suffix) {
			if id != match {
				count++
			}
			match = id
		}
	}
	if count == 1 {
		return match, true
	}
	return input, false
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
