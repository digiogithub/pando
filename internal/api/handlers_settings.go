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
	HomeDirectory    string `json:"home_directory"`
	WorkingDirectory string `json:"working_directory"`
	DefaultModel     string `json:"default_model"`
	DefaultProvider  string `json:"default_provider"`
	Theme            string `json:"theme"`
	Debug            bool   `json:"debug"`
	LogFile          string `json:"log_file,omitempty"`
	AutoCompact      bool   `json:"auto_compact"`
	SkillsEnabled    bool   `json:"skills_enabled"`
	DataDirectory    string `json:"data_directory"`
	ShowHiddenFiles  bool   `json:"show_hidden_files"`
	LLMCacheEnabled  bool   `json:"llm_cache_enabled"`
	EvaluatorEnabled bool   `json:"evaluator_enabled"`
	JudgeModel       string `json:"judge_model"`

	ToolDiscoveryEnabled        bool   `json:"tool_discovery_enabled"`
	ToolDiscoveryMode           string `json:"tool_discovery_mode"`
	ToolDiscoveryMaxDirectTools int    `json:"tool_discovery_max_direct_tools"`
	ToolDiscoverySearchLimit    int    `json:"tool_discovery_search_limit"`
}

// SettingsUpdateRequest contains the fields that can be updated via PUT /api/v1/settings.
type SettingsUpdateRequest struct {
	DefaultModel     *string `json:"default_model,omitempty"`
	DefaultProvider  *string `json:"default_provider,omitempty"`
	Theme            *string `json:"theme,omitempty"`
	Debug            *bool   `json:"debug,omitempty"`
	AutoCompact      *bool   `json:"auto_compact,omitempty"`
	SkillsEnabled    *bool   `json:"skills_enabled,omitempty"`
	ShowHiddenFiles  *bool   `json:"show_hidden_files,omitempty"`
	LLMCacheEnabled  *bool   `json:"llm_cache_enabled,omitempty"`
	EvaluatorEnabled *bool   `json:"evaluator_enabled,omitempty"`
	JudgeModel       *string `json:"judge_model,omitempty"`

	ToolDiscoveryEnabled        *bool   `json:"tool_discovery_enabled,omitempty"`
	ToolDiscoveryMode           *string `json:"tool_discovery_mode,omitempty"`
	ToolDiscoveryMaxDirectTools *int    `json:"tool_discovery_max_direct_tools,omitempty"`
	ToolDiscoverySearchLimit    *int    `json:"tool_discovery_search_limit,omitempty"`
}

// ProviderStatus describes a configured provider and whether it has an API key set.
type ProviderStatus struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	HasAPIKey bool   `json:"has_api_key"`
	BaseURL   string `json:"base_url,omitempty"`
	UseOAuth  bool   `json:"use_oauth,omitempty"`
}

// buildSettingsResponse builds the settings response from the current config.
func buildSettingsResponse() (*SettingsResponse, error) {
	cfg := config.Get()
	if cfg == nil {
		return nil, nil
	}

	homeDir, _ := os.UserHomeDir()

	// Derive default model from the coder agent config.
	defaultModel := ""
	if agent, ok := cfg.Agents[config.AgentCoder]; ok {
		defaultModel = string(agent.Model)
	}

	// Derive provider from the first configured (non-disabled) provider.
	defaultProvider := ""
	for provider, providerCfg := range cfg.Providers {
		if !providerCfg.Disabled {
			defaultProvider = string(provider)
			break
		}
	}

	return &SettingsResponse{
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
		ShowHiddenFiles:  cfg.TUI.ShowHiddenFiles,
		LLMCacheEnabled:  cfg.LLMCache.Enabled,
		EvaluatorEnabled: cfg.Evaluator.Enabled,
		JudgeModel:       string(cfg.Evaluator.Model),

		ToolDiscoveryEnabled:        cfg.ToolDiscovery.Enabled,
		ToolDiscoveryMode:           toolDiscoveryModeOrDefault(cfg.ToolDiscovery.Mode),
		ToolDiscoveryMaxDirectTools: intOrDefault(cfg.ToolDiscovery.MaxDirectTools, 64),
		ToolDiscoverySearchLimit:    intOrDefault(cfg.ToolDiscovery.SearchLimit, 8),
	}, nil
}

// toolDiscoveryModeOrDefault normalizes an empty mode to "auto".
func toolDiscoveryModeOrDefault(mode string) string {
	if mode == "" {
		return "auto"
	}
	return mode
}

// intOrDefault returns fallback when value <= 0 so unset config surfaces its
// effective default in the UI.
func intOrDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

// handleSettings dispatches GET and PUT requests for /api/v1/settings.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetSettings(w, r)
	case http.MethodPut:
		s.handlePutSettings(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := buildSettingsResponse()
	if err != nil || resp == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {

	var req SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if config.Get() == nil {
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

	if req.Debug != nil {
		if err := config.UpdateDebug(*req.Debug); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update debug setting")
			return
		}
	}

	if req.AutoCompact != nil {
		if err := config.UpdateAutoCompact(*req.AutoCompact); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update auto compact setting")
			return
		}
	}

	if req.SkillsEnabled != nil {
		if err := config.UpdateSkillsEnabled(*req.SkillsEnabled); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update skills enabled setting")
			return
		}
	}

	if req.ShowHiddenFiles != nil {
		if err := config.UpdateShowHiddenFiles(*req.ShowHiddenFiles); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update show hidden files setting")
			return
		}
	}

	if req.LLMCacheEnabled != nil {
		if err := config.UpdateLLMCache(*req.LLMCacheEnabled); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update llm cache setting")
			return
		}
	}

	if req.ToolDiscoveryEnabled != nil || req.ToolDiscoveryMode != nil ||
		req.ToolDiscoveryMaxDirectTools != nil || req.ToolDiscoverySearchLimit != nil {
		td := config.Get().ToolDiscovery
		if req.ToolDiscoveryEnabled != nil {
			td.Enabled = *req.ToolDiscoveryEnabled
		}
		if req.ToolDiscoveryMode != nil {
			switch *req.ToolDiscoveryMode {
			case "", "auto", "always", "off":
				td.Mode = *req.ToolDiscoveryMode
			default:
				writeError(w, http.StatusBadRequest, "invalid tool_discovery_mode (expected auto, always or off)")
				return
			}
		}
		if req.ToolDiscoveryMaxDirectTools != nil {
			if *req.ToolDiscoveryMaxDirectTools < 0 {
				writeError(w, http.StatusBadRequest, "tool_discovery_max_direct_tools must be >= 0")
				return
			}
			td.MaxDirectTools = *req.ToolDiscoveryMaxDirectTools
		}
		if req.ToolDiscoverySearchLimit != nil {
			if *req.ToolDiscoverySearchLimit < 0 {
				writeError(w, http.StatusBadRequest, "tool_discovery_search_limit must be >= 0")
				return
			}
			td.SearchLimit = *req.ToolDiscoverySearchLimit
		}
		if err := config.UpdateToolDiscovery(td); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update tool discovery settings")
			return
		}
	}

	if req.EvaluatorEnabled != nil || req.JudgeModel != nil {
		eval := config.Get().Evaluator
		if req.EvaluatorEnabled != nil {
			eval.Enabled = *req.EvaluatorEnabled
		}
		if req.JudgeModel != nil {
			eval.Model = models.ModelID(*req.JudgeModel)
		}
		if err := config.UpdateEvaluator(eval); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update evaluator settings")
			return
		}
	}

	// Return the updated settings.
	resp, err := buildSettingsResponse()
	if err != nil || resp == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
