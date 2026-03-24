package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// SettingsResponse is the JSON representation of current application settings.
type SettingsResponse struct {
	HomeDirectory      string `json:"home_directory"`
	WorkingDirectory   string `json:"working_directory"`
	DefaultModel       string `json:"default_model"`
	DefaultProvider    string `json:"default_provider"`
	Theme              string `json:"theme"`
	Debug              bool   `json:"debug"`
	LogFile            string `json:"log_file,omitempty"`
	AutoCompact        bool   `json:"auto_compact"`
	SkillsEnabled      bool   `json:"skills_enabled"`
	DataDirectory      string `json:"data_directory"`
}

// SettingsUpdateRequest contains the fields that can be updated via PUT /api/v1/settings.
type SettingsUpdateRequest struct {
	DefaultModel    *string `json:"default_model,omitempty"`
	DefaultProvider *string `json:"default_provider,omitempty"`
	Theme           *string `json:"theme,omitempty"`
	Debug           *bool   `json:"debug,omitempty"`
	AutoCompact     *bool   `json:"auto_compact,omitempty"`
	SkillsEnabled   *bool   `json:"skills_enabled,omitempty"`
}

// ProviderStatus describes a configured provider and whether it has an API key set.
type ProviderStatus struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	HasAPIKey bool   `json:"has_api_key"`
	BaseURL   string `json:"base_url,omitempty"`
	UseOAuth  bool   `json:"use_oauth,omitempty"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	homeDir, _ := os.UserHomeDir()

	// Derive default model and provider from the coder agent config.
	defaultModel := ""
	defaultProvider := ""
	if agent, ok := cfg.Agents[config.AgentCoder]; ok {
		defaultModel = string(agent.Model)
	}
	// Derive provider from the first configured (non-disabled) provider.
	for provider, providerCfg := range cfg.Providers {
		if !providerCfg.Disabled {
			defaultProvider = string(provider)
			break
		}
	}

	resp := SettingsResponse{
		HomeDirectory:    homeDir,
		WorkingDirectory: cfg.WorkingDir,
		DefaultModel:     defaultModel,
		DefaultProvider:  defaultProvider,
		Theme:            cfg.TUI.Theme,
		Debug:            cfg.Debug,
		LogFile:          cfg.LogFile,
		AutoCompact:      cfg.AutoCompact,
		SkillsEnabled:    cfg.Skills.Enabled,
		DataDirectory:    cfg.Data.Directory,
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	// Apply theme update (uses the dedicated config helper that also persists).
	if req.Theme != nil {
		if err := config.UpdateTheme(*req.Theme); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update theme: "+err.Error())
			return
		}
	}

	// Apply model update for the coder agent.
	if req.DefaultModel != nil {
		if err := config.UpdateAgentModel(config.AgentCoder, models.ModelID(*req.DefaultModel)); err != nil {
			writeError(w, http.StatusBadRequest, "failed to update model: "+err.Error())
			return
		}
	}

	// Return the updated settings.
	s.handleGetSettings(w, r)
}

func (s *Server) handleGetProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	providers := make([]ProviderStatus, 0, len(cfg.Providers))
	for name, providerCfg := range cfg.Providers {
		providers = append(providers, ProviderStatus{
			Name:      string(name),
			Enabled:   !providerCfg.Disabled,
			HasAPIKey: providerCfg.APIKey != "",
			BaseURL:   providerCfg.BaseURL,
			UseOAuth:  providerCfg.UseOAuth,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": providers,
	})
}
