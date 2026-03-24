package api

import (
	"encoding/json"
	"net/http"

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

// hardcodedModels is the list of well-known models returned by GET /api/v1/models.
var hardcodedModels = []ModelInfo{
	{
		ID:          "claude-opus-4-6",
		Name:        "Claude Opus 4.6",
		Provider:    "anthropic",
		Description: "Most capable Anthropic model",
		Badges:      []string{"capable", "fast"},
	},
	{
		ID:          "claude-sonnet-4-6",
		Name:        "Claude Sonnet 4.6",
		Provider:    "anthropic",
		Description: "Balanced performance and cost",
		Badges:      []string{"fast", "cost"},
	},
	{
		ID:          "claude-haiku-4-5",
		Name:        "Claude Haiku 4.5",
		Provider:    "anthropic",
		Description: "Fastest Anthropic model",
		Badges:      []string{"fast", "cost"},
	},
	{
		ID:          "gpt-4o",
		Name:        "GPT-4o",
		Provider:    "openai",
		Description: "OpenAI flagship multimodal model",
		Badges:      []string{"fast", "capable"},
	},
	{
		ID:          "gpt-4o-mini",
		Name:        "GPT-4o Mini",
		Provider:    "openai",
		Description: "Smaller, cost-efficient GPT-4o",
		Badges:      []string{"fast", "cost"},
	},
	{
		ID:          "gemini-2.0-flash",
		Name:        "Gemini 2.0 Flash",
		Provider:    "google",
		Description: "Google fast and efficient model",
		Badges:      []string{"fast", "cost"},
	},
}

// handleListModels handles GET /api/v1/models.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": hardcodedModels,
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
