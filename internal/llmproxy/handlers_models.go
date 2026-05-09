package llmproxy

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/auth"
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

// handleListModels handles GET /v1/models, returning an OpenAI-compatible model list.
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

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	accounts := config.GetProviderAccounts()

	type accountEntry struct {
		account     config.ProviderAccount
		bearerToken string
		skip        bool
	}

	entries := make([]accountEntry, 0, len(accounts))
	for _, acc := range accounts {
		if acc.Disabled {
			continue
		}
		entry := accountEntry{account: acc}
		if acc.Type == models.ProviderCopilot {
			if token, err := auth.LoadGitHubOAuthToken(); err == nil && token != "" {
				entry.bearerToken = token
			} else if session, err := auth.LoadCopilotSession(); err == nil && session != nil {
				entry.bearerToken = session.AccessToken
			}
			if entry.bearerToken == "" {
				entry.skip = true
			}
		}
		entries = append(entries, entry)
	}

	type accountResult struct {
		provider models.ModelProvider
		items    []openAIModel
	}

	resultCh := make(chan accountResult, len(entries))
	var wg sync.WaitGroup

	for _, e := range entries {
		e := e
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc := e.account
			if e.skip {
				resultCh <- accountResult{provider: acc.Type}
				return
			}

			fetched, err := models.FetchModelsFromProvider(ctx, acc.Type, acc.APIKey, e.bearerToken, acc.BaseURL)
			if err != nil || len(fetched) == 0 {
				resultCh <- accountResult{provider: acc.Type}
				return
			}

			items := make([]openAIModel, 0, len(fetched))
			for _, m := range fetched {
				modelID := models.NormalizeModelID(m.ID)
				created := m.Created
				if created == 0 {
					created = 1715000000
				}
				items = append(items, openAIModel{
					ID:      string(modelID),
					Object:  "model",
					Created: created,
					OwnedBy: string(acc.Type),
				})
			}
			resultCh <- accountResult{provider: acc.Type, items: items}
		}()
	}

	wg.Wait()
	close(resultCh)

	allModels := make([]openAIModel, 0)
	for res := range resultCh {
		allModels = append(allModels, res.items...)
	}

	// Fallback: if no dynamic models, use static SupportedModels for configured providers.
	if len(allModels) == 0 {
		seenProviders := make(map[models.ModelProvider]bool)
		for _, acc := range accounts {
			if !acc.Disabled {
				seenProviders[acc.Type] = true
			}
		}
		for provider, providerCfg := range cfg.Providers {
			if !providerCfg.Disabled {
				seenProviders[provider] = true
			}
		}
		for provider := range seenProviders {
			for _, m := range models.SupportedModels {
				if m.Provider != provider {
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
	}

	writeJSON(w, http.StatusOK, openAIModelList{
		Object: "list",
		Data:   allModels,
	})
}

// handleGetModel handles GET /v1/models/{id}, returning an OpenAI-compatible model object.
func (s *LLMProxyServer) handleGetModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Search SupportedModels for a matching model.
	for _, m := range models.SupportedModels {
		if string(m.ID) == id {
			writeJSON(w, http.StatusOK, openAIModel{
				ID:      string(m.ID),
				Object:  "model",
				Created: 1715000000,
				OwnedBy: string(m.Provider),
			})
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": map[string]string{
			"message": "Model not found",
			"type":    "invalid_request_error",
			"code":    "model_not_found",
		},
	})
}
