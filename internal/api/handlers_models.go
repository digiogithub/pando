package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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
// It dynamically queries each configured (non-disabled) provider for their models.
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

	result := make([]ModelInfo, 0)

	for provider, providerCfg := range cfg.Providers {
		if providerCfg.Disabled {
			continue
		}

		// Get bearer token for Copilot
		bearerToken := ""
		if provider == models.ProviderCopilot {
			if token, err := auth.LoadGitHubOAuthToken(); err == nil && token != "" {
				bearerToken = token
			} else if session, err := auth.LoadCopilotSession(); err == nil && session != nil {
				bearerToken = session.AccessToken
			}
			if bearerToken == "" {
				continue
			}
		}

		fetched, err := models.FetchModelsFromProvider(ctx, provider, providerCfg.APIKey, bearerToken, providerCfg.BaseURL)
		if err != nil {
			continue
		}

		for _, m := range fetched {
			name := m.Name
			if name == "" {
				name = m.ID
			}

			// Register the model in SupportedModels using the raw ID so that
			// handleSetActiveModel can find it when the web-ui sends the model ID back.
			modelID := models.ModelID(m.ID)
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
					Provider:         provider,
					APIModel:         m.ID,
					ContextWindow:    contextWindow,
					DefaultMaxTokens: maxTokens,
				})
			}

			result = append(result, ModelInfo{
				ID:          m.ID,
				Name:        name,
				Provider:    string(provider),
				Description: m.Description,
				Badges:      badgesForModel(m.ID),
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": result,
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
