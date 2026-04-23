package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// ModelInfo describes a model available for selection.
type ModelInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Provider    string   `json:"provider"`
	AccountID   string   `json:"accountId,omitempty"`
	Description string   `json:"description"`
	Badges      []string `json:"badges"`
}

// badgesForModel returns heuristic badges based on model ID.
func badgesForModel(id string) []string {
	id = strings.ToLower(id)
	switch {
	case strings.Contains(id, "opus") || strings.Contains(id, "gpt-4o") && !strings.Contains(id, "mini") || strings.Contains(id, "large"):
		return []string{"capable"}
	case strings.Contains(id, "haiku") || strings.Contains(id, "mini") || strings.Contains(id, "flash") || strings.Contains(id, "small"):
		return []string{"fast", "cost"}
	case strings.Contains(id, "sonnet") || strings.Contains(id, "gpt-4"):
		return []string{"fast", "cost"}
	default:
		return []string{"fast"}
	}
}

// handleListModels handles GET /api/v1/models.
// Fetches models from all configured (non-disabled) provider accounts in parallel and
// returns both the model list and a per-account error map for diagnostics.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
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

	// Count non-disabled accounts per provider type (for display label disambiguation)
	typeCount := make(map[models.ModelProvider]int)
	for _, acc := range accounts {
		if !acc.Disabled {
			typeCount[acc.Type]++
		}
	}

	type accountEntry struct {
		account     config.ProviderAccount
		bearerToken string
		skip        bool
		skipReason  string
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
				entry.skipReason = "no GitHub OAuth token found — run 'pando auth login'"
			}
		}
		entries = append(entries, entry)
	}

	type accountResult struct {
		accountID string
		provider  models.ModelProvider
		items     []ModelInfo
		err       string
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
				resultCh <- accountResult{accountID: acc.ID, provider: acc.Type, err: e.skipReason}
				return
			}

			fetched, err := models.FetchModelsFromProvider(ctx, acc.Type, acc.APIKey, e.bearerToken, acc.BaseURL)
			if err != nil {
				resultCh <- accountResult{accountID: acc.ID, provider: acc.Type, err: err.Error()}
				return
			}

			sameTypeCount := typeCount[acc.Type]
			items := make([]ModelInfo, 0, len(fetched))
			for _, m := range fetched {
				name := m.Name
				if name == "" {
					name = m.ID
				}

				modelID := models.NormalizeModelID(m.ID)
				if _, exists := models.SupportedModels[modelID]; !exists {
					contextWindow := m.ContextWindow
					if contextWindow <= 0 {
						contextWindow = 128_000
					}
					maxTokens := int64(4096)
					if contextWindow < maxTokens {
						maxTokens = contextWindow / 2
					}
					models.RegisterDynamicModel(models.Model{
						ID:               modelID,
						Name:             name,
						Provider:         acc.Type,
						APIModel:         m.ID,
						ContextWindow:    contextWindow,
						DefaultMaxTokens: maxTokens,
						AccountID:        acc.ID,
					})
				}

				// Disambiguate display name when multiple accounts share the same provider type
				displayName := name
				if sameTypeCount > 1 {
					displayName = acc.ID + ": " + name
				}
				items = append(items, ModelInfo{
					ID:          string(modelID),
					Name:        displayName,
					Provider:    string(acc.Type),
					AccountID:   acc.ID,
					Description: m.Description,
					Badges:      badgesForModel(m.ID),
				})
			}
			resultCh <- accountResult{accountID: acc.ID, provider: acc.Type, items: items}
		}()
	}

	wg.Wait()
	close(resultCh)

	allModels := make([]ModelInfo, 0)
	providerErrors := make(map[string]string)

	for res := range resultCh {
		if res.err != "" {
			key := res.accountID
			if key == "" {
				key = string(res.provider)
			}
			providerErrors[key] = res.err
		} else {
			allModels = append(allModels, res.items...)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": allModels,
		"errors": providerErrors,
	})
}

// handleSetActiveModel handles PUT /api/v1/models/active.
func (s *Server) handleSetActiveModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid request body: 'model' field required")
		return
	}

	if config.Get() == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	if err := config.UpdateAgentModel(config.AgentCoder, models.ModelID(req.Model)); err != nil {
		writeError(w, http.StatusBadRequest, "failed to update model: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"model": req.Model})
}
