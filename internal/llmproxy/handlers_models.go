package llmproxy

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// openAIModel represents an OpenAI-compatible model object.
type openAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// openAIModelList represents an OpenAI-compatible list of models.
type openAIModelList struct {
	Object string        `json:"object"`
	Data   []openAIModel `json:"data"`
}

// handleListModels handles GET /v1/models, returning an OpenAI-compatible model
// list. Models are served from the shared registry (the same source normal mode
// uses), so the IDs are provider/account-prefixed (e.g. "openai.gpt-4o",
// "work.gpt-4o") and never collide between providers. Every listed ID is
// directly usable by /v1/chat/completions and /v1/messages.
func (s *LLMProxyServer) handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	accounts := activeAccounts()

	// Build the set of configured providers and account IDs so we only expose
	// models the proxy can actually serve.
	configuredProviders := make(map[models.ModelProvider]bool)
	configuredAccountIDs := make(map[string]bool)
	for _, acc := range accounts {
		configuredProviders[acc.Type] = true
		if acc.ID != "" {
			configuredAccountIDs[acc.ID] = true
		}
	}
	// Honour the legacy Providers map as well.
	for provider, providerCfg := range cfg.Providers {
		if !providerCfg.Disabled {
			configuredProviders[provider] = true
		}
	}

	// If the registry has no models for the configured providers yet (e.g. the
	// background refresh loop has not completed at startup), refresh on demand so
	// the listing mirrors normal mode.
	if !registryHasModelsForProviders(configuredProviders) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		refreshAccountModels(ctx, accounts)
	}

	allModels := collectRegistryModels(configuredProviders, configuredAccountIDs)

	// Fallback: if still empty, expose static models for the configured providers.
	if len(allModels) == 0 {
		for _, m := range models.SupportedModels() {
			if !configuredProviders[m.Provider] {
				continue
			}
			allModels = append(allModels, openAIModel{
				ID:      string(m.ID),
				Object:  "model",
				Created: 1715000000,
				OwnedBy: string(m.Provider),
			})
		}
	}

	sort.Slice(allModels, func(i, j int) bool { return allModels[i].ID < allModels[j].ID })

	writeJSON(w, http.StatusOK, openAIModelList{
		Object: "list",
		Data:   allModels,
	})
}

// registryHasModelsForProviders reports whether the registry contains at least
// one model for any of the configured providers.
func registryHasModelsForProviders(providers map[models.ModelProvider]bool) bool {
	for _, m := range models.GetAllModels() {
		if providers[m.Provider] {
			return true
		}
	}
	return false
}

// collectRegistryModels returns the registry models that belong to a configured
// provider (and a configured account, when the model is account-scoped),
// deduplicated by ID.
func collectRegistryModels(providers map[models.ModelProvider]bool, accountIDs map[string]bool) []openAIModel {
	seen := make(map[string]bool)
	out := make([]openAIModel, 0)
	for _, m := range models.GetAllModels() {
		if !providers[m.Provider] {
			continue
		}
		// Account-scoped dynamic models are only listed when their account exists.
		if m.AccountID != "" && !accountIDs[m.AccountID] {
			continue
		}
		id := string(m.ID)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, openAIModel{
			ID:      id,
			Object:  "model",
			Created: 1715000000,
			OwnedBy: string(m.Provider),
		})
	}
	return out
}

// handleGetModel handles GET /v1/models/{id}, returning an OpenAI-compatible
// model object. The ID is resolved with the same logic as chat/messages, so
// prefixed IDs, raw upstream IDs and aliases all work.
func (s *LLMProxyServer) handleGetModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if resolved, ok := resolveModel(id); ok {
		writeJSON(w, http.StatusOK, openAIModel{
			ID:      string(resolved.model.ID),
			Object:  "model",
			Created: 1715000000,
			OwnedBy: string(resolved.model.Provider),
		})
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": map[string]string{
			"message": "Model not found",
			"type":    "invalid_request_error",
			"code":    "model_not_found",
		},
	})
}
