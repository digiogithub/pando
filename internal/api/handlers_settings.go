package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/caveman"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/tui/styles"
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
	NerdFonts        bool   `json:"nerd_fonts"`
	LLMCacheEnabled  bool   `json:"llm_cache_enabled"`
	// ModelsDevEnabled controls the models.dev catalog that completes model
	// pricing/limits the providers do not report.
	ModelsDevEnabled bool `json:"models_dev_enabled"`
	// ImageAutoResize toggles the image resize/recompress pipeline before send.
	ImageAutoResize bool `json:"image_auto_resize"`
	// ImageUseFilesAPI opts into the Anthropic beta Messages API + Files API
	// (uploads images once, references by file_id across turns).
	ImageUseFilesAPI bool   `json:"image_use_files_api"`
	EvaluatorEnabled bool   `json:"evaluator_enabled"`
	JudgeModel       string `json:"judge_model"`
	// OutputFilterEnabled is the inverse of Bash.OutputFilterDisabled: RTK-style
	// command-output compression. True means compression is on (the default).
	OutputFilterEnabled bool `json:"output_filter_enabled"`
	// CavemanDefaultMode is the global output-brevity default ("" = off, or
	// lite|full|ultra). Sessions that ran /caveman keep their own choice.
	CavemanDefaultMode string `json:"caveman_default_mode"`

	ToolDiscoveryEnabled        bool   `json:"tool_discovery_enabled"`
	ToolDiscoveryMode           string `json:"tool_discovery_mode"`
	ToolDiscoveryMaxDirectTools int    `json:"tool_discovery_max_direct_tools"`
	ToolDiscoverySearchLimit    int    `json:"tool_discovery_search_limit"`

	// Delegation (mesnada delegated-task conclusions + agent-loop resurrection).
	DelegationEnabled                  bool   `json:"delegation_enabled"`
	DelegationInjectIntoLiveLoop       bool   `json:"delegation_inject_into_live_loop"`
	DelegationResurrectIdleLoop        bool   `json:"delegation_resurrect_idle_loop"`
	DelegationSynthesizeFallback       bool   `json:"delegation_synthesize_fallback"`
	DelegationMaxResurrections         int    `json:"delegation_max_resurrections"`
	DelegationMaxDepth                 int    `json:"delegation_max_depth"`
	DelegationMaxConcurrent            int    `json:"delegation_max_concurrent"`
	DelegationResurrectionTimeout      string `json:"delegation_resurrection_timeout"`
	DelegationReuseWarmInstances       bool   `json:"delegation_reuse_warm_instances"`
	DelegationAutoStartWarm            bool   `json:"delegation_auto_start_warm"`
	DelegationWarmIdleTimeout          string `json:"delegation_warm_idle_timeout"`
	DelegationWarmQueueDepth           int    `json:"delegation_warm_queue_depth"`
	DelegationAllowExternalWarmTargets bool   `json:"delegation_allow_external_warm_targets"`
	DelegationAcceptDelegations        bool   `json:"delegation_accept_delegations"`
	// Integrity gate + anti-thrash breaker. Exposed as positive "enabled" flags
	// for the UI even though the config stores them inverted (…Disabled).
	DelegationConclusionGate      bool   `json:"delegation_conclusion_gate"`
	DelegationBreaker             bool   `json:"delegation_breaker"`
	DelegationMaxTaskRetries      int    `json:"delegation_max_task_retries"`
	DelegationRateLimitCooldown   string `json:"delegation_rate_limit_cooldown"`
	DelegationRecentSuccessWindow string `json:"delegation_recent_success_window"`
	// Durable delegation event log, also exposed as a positive flag.
	DelegationEventLog           bool `json:"delegation_event_log"`
	DelegationEventLogMaxEntries int  `json:"delegation_event_log_max_entries"`
	// Claim-lease dispatcher (orchestrator scheduling).
	OrchestratorMaxParallel      int    `json:"orchestrator_max_parallel"`
	OrchestratorMaxPerEngine     int    `json:"orchestrator_max_per_engine"`
	OrchestratorClaimTTL         string `json:"orchestrator_claim_ttl"`
	OrchestratorDispatchInterval string `json:"orchestrator_dispatch_interval"`
}

// SettingsUpdateRequest contains the fields that can be updated via PUT /api/v1/settings.
type SettingsUpdateRequest struct {
	DefaultModel        *string `json:"default_model,omitempty"`
	DefaultProvider     *string `json:"default_provider,omitempty"`
	Theme               *string `json:"theme,omitempty"`
	Debug               *bool   `json:"debug,omitempty"`
	AutoCompact         *bool   `json:"auto_compact,omitempty"`
	SkillsEnabled       *bool   `json:"skills_enabled,omitempty"`
	ShowHiddenFiles     *bool   `json:"show_hidden_files,omitempty"`
	NerdFonts           *bool   `json:"nerd_fonts,omitempty"`
	LLMCacheEnabled     *bool   `json:"llm_cache_enabled,omitempty"`
	ModelsDevEnabled    *bool   `json:"models_dev_enabled,omitempty"`
	ImageAutoResize     *bool   `json:"image_auto_resize,omitempty"`
	ImageUseFilesAPI    *bool   `json:"image_use_files_api,omitempty"`
	EvaluatorEnabled    *bool   `json:"evaluator_enabled,omitempty"`
	JudgeModel          *string `json:"judge_model,omitempty"`
	OutputFilterEnabled *bool   `json:"output_filter_enabled,omitempty"`
	CavemanDefaultMode  *string `json:"caveman_default_mode,omitempty"`

	ToolDiscoveryEnabled        *bool   `json:"tool_discovery_enabled,omitempty"`
	ToolDiscoveryMode           *string `json:"tool_discovery_mode,omitempty"`
	ToolDiscoveryMaxDirectTools *int    `json:"tool_discovery_max_direct_tools,omitempty"`
	ToolDiscoverySearchLimit    *int    `json:"tool_discovery_search_limit,omitempty"`

	DelegationEnabled                  *bool   `json:"delegation_enabled,omitempty"`
	DelegationInjectIntoLiveLoop       *bool   `json:"delegation_inject_into_live_loop,omitempty"`
	DelegationResurrectIdleLoop        *bool   `json:"delegation_resurrect_idle_loop,omitempty"`
	DelegationSynthesizeFallback       *bool   `json:"delegation_synthesize_fallback,omitempty"`
	DelegationMaxResurrections         *int    `json:"delegation_max_resurrections,omitempty"`
	DelegationMaxDepth                 *int    `json:"delegation_max_depth,omitempty"`
	DelegationMaxConcurrent            *int    `json:"delegation_max_concurrent,omitempty"`
	DelegationResurrectionTimeout      *string `json:"delegation_resurrection_timeout,omitempty"`
	DelegationReuseWarmInstances       *bool   `json:"delegation_reuse_warm_instances,omitempty"`
	DelegationAutoStartWarm            *bool   `json:"delegation_auto_start_warm,omitempty"`
	DelegationWarmIdleTimeout          *string `json:"delegation_warm_idle_timeout,omitempty"`
	DelegationWarmQueueDepth           *int    `json:"delegation_warm_queue_depth,omitempty"`
	DelegationAllowExternalWarmTargets *bool   `json:"delegation_allow_external_warm_targets,omitempty"`
	DelegationAcceptDelegations        *bool   `json:"delegation_accept_delegations,omitempty"`
	DelegationConclusionGate           *bool   `json:"delegation_conclusion_gate,omitempty"`
	DelegationBreaker                  *bool   `json:"delegation_breaker,omitempty"`
	DelegationMaxTaskRetries           *int    `json:"delegation_max_task_retries,omitempty"`
	DelegationRateLimitCooldown        *string `json:"delegation_rate_limit_cooldown,omitempty"`
	DelegationRecentSuccessWindow      *string `json:"delegation_recent_success_window,omitempty"`
	DelegationEventLog                 *bool   `json:"delegation_event_log,omitempty"`
	DelegationEventLogMaxEntries       *int    `json:"delegation_event_log_max_entries,omitempty"`

	OrchestratorMaxParallel      *int    `json:"orchestrator_max_parallel,omitempty"`
	OrchestratorMaxPerEngine     *int    `json:"orchestrator_max_per_engine,omitempty"`
	OrchestratorClaimTTL         *string `json:"orchestrator_claim_ttl,omitempty"`
	OrchestratorDispatchInterval *string `json:"orchestrator_dispatch_interval,omitempty"`
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
		HomeDirectory:       homeDir,
		WorkingDirectory:    cfg.WorkingDir,
		DefaultModel:        defaultModel,
		DefaultProvider:     defaultProvider,
		Theme:               cfg.TUI.Theme,
		Debug:               cfg.Debug,
		LogFile:             cfg.LogFile,
		AutoCompact:         cfg.AutoCompact,
		SkillsEnabled:       cfg.Skills.Enabled,
		DataDirectory:       cfg.Data.Directory,
		ShowHiddenFiles:     cfg.TUI.ShowHiddenFiles,
		NerdFonts:           cfg.NerdFontsEnabled(),
		LLMCacheEnabled:     cfg.LLMCache.Enabled,
		ModelsDevEnabled:    cfg.ModelsDev.Enabled,
		ImageAutoResize:     cfg.Image.AutoResize,
		ImageUseFilesAPI:    cfg.Image.UseFilesAPI,
		EvaluatorEnabled:    cfg.Evaluator.Enabled,
		JudgeModel:          string(cfg.Evaluator.Model),
		OutputFilterEnabled: !cfg.Bash.OutputFilterDisabled,
		CavemanDefaultMode:  cfg.CavemanDefaultMode(),

		ToolDiscoveryEnabled:        cfg.ToolDiscovery.Enabled,
		ToolDiscoveryMode:           toolDiscoveryModeOrDefault(cfg.ToolDiscovery.Mode),
		ToolDiscoveryMaxDirectTools: intOrDefault(cfg.ToolDiscovery.MaxDirectTools, 64),
		ToolDiscoverySearchLimit:    intOrDefault(cfg.ToolDiscovery.SearchLimit, 8),

		DelegationEnabled:                  cfg.Mesnada.Delegation.Enabled,
		DelegationInjectIntoLiveLoop:       cfg.Mesnada.Delegation.InjectIntoLiveLoop,
		DelegationResurrectIdleLoop:        cfg.Mesnada.Delegation.ResurrectIdleLoop,
		DelegationSynthesizeFallback:       cfg.Mesnada.Delegation.SynthesizeFallback,
		DelegationMaxResurrections:         intOrDefault(cfg.Mesnada.Delegation.MaxResurrections, 4),
		DelegationMaxDepth:                 intOrDefault(cfg.Mesnada.Delegation.MaxDepth, 3),
		DelegationMaxConcurrent:            intOrDefault(cfg.Mesnada.Delegation.MaxConcurrent, 8),
		DelegationResurrectionTimeout:      delegationTimeoutOrDefault(cfg.Mesnada.Delegation.ResurrectionTimeout),
		DelegationReuseWarmInstances:       cfg.Mesnada.Delegation.ReuseWarmInstances,
		DelegationAutoStartWarm:            cfg.Mesnada.Delegation.AutoStartWarmInstance,
		DelegationWarmIdleTimeout:          warmIdleTimeoutOrDefault(cfg.Mesnada.Delegation.WarmInstanceIdleTimeout),
		DelegationWarmQueueDepth:           cfg.Mesnada.Delegation.WarmQueueDepth,
		DelegationAllowExternalWarmTargets: cfg.Mesnada.Delegation.AllowExternalWarmTargets,
		DelegationAcceptDelegations:        cfg.Mesnada.Delegation.AcceptDelegations,
		// Inverted on the wire: the config stores an opt-OUT, the UI shows an
		// opt-IN toggle that is on by default.
		DelegationConclusionGate:      !cfg.Mesnada.Delegation.ConclusionGateDisabled,
		DelegationBreaker:             !cfg.Mesnada.Delegation.BreakerDisabled,
		DelegationMaxTaskRetries:      cfg.Mesnada.Delegation.MaxTaskRetries,
		DelegationRateLimitCooldown:   cfg.Mesnada.Delegation.RateLimitCooldown,
		DelegationRecentSuccessWindow: cfg.Mesnada.Delegation.RecentSuccessWindow,
		DelegationEventLog:            !cfg.Mesnada.Delegation.EventLogDisabled,
		DelegationEventLogMaxEntries:  intOrDefault(cfg.Mesnada.Delegation.EventLogMaxEntries, 5000),

		OrchestratorMaxParallel:      intOrDefault(cfg.Mesnada.Orchestrator.MaxParallel, 5),
		OrchestratorMaxPerEngine:     cfg.Mesnada.Orchestrator.MaxPerEngine,
		OrchestratorClaimTTL:         durationOrDefault(cfg.Mesnada.Orchestrator.ClaimTTL, "2m"),
		OrchestratorDispatchInterval: durationOrDefault(cfg.Mesnada.Orchestrator.DispatchInterval, "10s"),
	}, nil
}

// durationOrDefault normalizes a blank duration string to fallback so GET
// responses always round-trip a parseable value.
func durationOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// delegationTimeoutOrDefault normalizes an empty resurrection timeout to "10m".
func delegationTimeoutOrDefault(timeout string) string {
	if timeout == "" {
		return "10m"
	}
	return timeout
}

// warmIdleTimeoutOrDefault normalizes an empty warm-instance idle timeout to "0"
// (idle auto-GC disabled), so GET responses round-trip a stable, parseable value.
func warmIdleTimeoutOrDefault(timeout string) string {
	if timeout == "" {
		return "0"
	}
	return timeout
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

	// Apply model update for the coder agent. Use the shared helper so the
	// running agent's provider is rebuilt, not just the persisted config.
	if req.DefaultModel != nil {
		if err := s.setCoderModel(models.ModelID(*req.DefaultModel)); err != nil {
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

	if req.NerdFonts != nil {
		if err := config.UpdateNerdFonts(*req.NerdFonts); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update nerd fonts setting")
			return
		}
		// Apply immediately so the running TUI's icon set swaps live.
		styles.SetNerdFonts(*req.NerdFonts)
	}

	if req.LLMCacheEnabled != nil {
		if err := config.UpdateLLMCache(*req.LLMCacheEnabled); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update llm cache setting")
			return
		}
	}

	if req.ModelsDevEnabled != nil {
		if err := config.UpdateModelsDev(*req.ModelsDevEnabled); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update models.dev setting")
			return
		}
	}

	if req.ImageAutoResize != nil {
		if err := config.UpdateImageAutoResize(*req.ImageAutoResize); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update image auto-resize setting")
			return
		}
	}

	if req.ImageUseFilesAPI != nil {
		if err := config.UpdateImageUseFilesAPI(*req.ImageUseFilesAPI); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update image files API setting")
			return
		}
	}

	if req.OutputFilterEnabled != nil {
		bashCfg := config.Get().Bash
		// The UI exposes "enabled"; the config stores the inverse "disabled" flag.
		bashCfg.OutputFilterDisabled = !*req.OutputFilterEnabled
		if err := config.UpdateBash(bashCfg); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update output filter setting")
			return
		}
	}

	if req.CavemanDefaultMode != nil {
		// An empty string is how "no default" is stored, so it is accepted here
		// even though ParseMode rejects it (a bare /caveman means full, not off).
		mode := caveman.ModeOff
		if raw := strings.TrimSpace(*req.CavemanDefaultMode); raw != "" {
			parsed, ok := caveman.ParseMode(raw)
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid caveman_default_mode (expected off, lite, full or ultra)")
				return
			}
			mode = parsed
		}
		cavemanCfg := config.Get().Caveman
		cavemanCfg.DefaultMode = string(mode)
		if err := config.UpdateCaveman(cavemanCfg); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update caveman setting: "+err.Error())
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

	if req.DelegationEnabled != nil || req.DelegationInjectIntoLiveLoop != nil ||
		req.DelegationResurrectIdleLoop != nil || req.DelegationSynthesizeFallback != nil ||
		req.DelegationMaxResurrections != nil || req.DelegationMaxDepth != nil ||
		req.DelegationMaxConcurrent != nil || req.DelegationResurrectionTimeout != nil ||
		req.DelegationReuseWarmInstances != nil || req.DelegationAutoStartWarm != nil ||
		req.DelegationWarmIdleTimeout != nil || req.DelegationWarmQueueDepth != nil ||
		req.DelegationAllowExternalWarmTargets != nil || req.DelegationAcceptDelegations != nil ||
		req.DelegationConclusionGate != nil || req.DelegationBreaker != nil ||
		req.DelegationMaxTaskRetries != nil || req.DelegationRateLimitCooldown != nil ||
		req.DelegationRecentSuccessWindow != nil ||
		req.DelegationEventLog != nil || req.DelegationEventLogMaxEntries != nil {
		del := config.Get().Mesnada.Delegation
		if req.DelegationEnabled != nil {
			del.Enabled = *req.DelegationEnabled
		}
		if req.DelegationInjectIntoLiveLoop != nil {
			del.InjectIntoLiveLoop = *req.DelegationInjectIntoLiveLoop
		}
		if req.DelegationResurrectIdleLoop != nil {
			del.ResurrectIdleLoop = *req.DelegationResurrectIdleLoop
		}
		if req.DelegationSynthesizeFallback != nil {
			del.SynthesizeFallback = *req.DelegationSynthesizeFallback
		}
		if req.DelegationMaxResurrections != nil {
			if *req.DelegationMaxResurrections < 0 {
				writeError(w, http.StatusBadRequest, "delegation_max_resurrections must be >= 0")
				return
			}
			del.MaxResurrections = *req.DelegationMaxResurrections
		}
		if req.DelegationMaxDepth != nil {
			if *req.DelegationMaxDepth < 0 {
				writeError(w, http.StatusBadRequest, "delegation_max_depth must be >= 0")
				return
			}
			del.MaxDepth = *req.DelegationMaxDepth
		}
		if req.DelegationMaxConcurrent != nil {
			if *req.DelegationMaxConcurrent < 0 {
				writeError(w, http.StatusBadRequest, "delegation_max_concurrent must be >= 0")
				return
			}
			del.MaxConcurrent = *req.DelegationMaxConcurrent
		}
		if req.DelegationResurrectionTimeout != nil {
			if _, err := time.ParseDuration(*req.DelegationResurrectionTimeout); err != nil {
				writeError(w, http.StatusBadRequest, "invalid delegation_resurrection_timeout (e.g. 10m, 1h)")
				return
			}
			del.ResurrectionTimeout = *req.DelegationResurrectionTimeout
		}
		if req.DelegationReuseWarmInstances != nil {
			del.ReuseWarmInstances = *req.DelegationReuseWarmInstances
		}
		if req.DelegationAutoStartWarm != nil {
			del.AutoStartWarmInstance = *req.DelegationAutoStartWarm
		}
		if req.DelegationWarmIdleTimeout != nil {
			// "0"/empty disables the idle auto-GC; any other value must be a valid
			// Go duration (e.g. 10m, 1h).
			if _, err := time.ParseDuration(*req.DelegationWarmIdleTimeout); err != nil {
				writeError(w, http.StatusBadRequest, "invalid delegation_warm_idle_timeout (0 to disable, e.g. 10m, 1h)")
				return
			}
			del.WarmInstanceIdleTimeout = *req.DelegationWarmIdleTimeout
		}
		if req.DelegationWarmQueueDepth != nil {
			if *req.DelegationWarmQueueDepth < 0 {
				writeError(w, http.StatusBadRequest, "delegation_warm_queue_depth must be >= 0")
				return
			}
			del.WarmQueueDepth = *req.DelegationWarmQueueDepth
		}
		if req.DelegationAllowExternalWarmTargets != nil {
			del.AllowExternalWarmTargets = *req.DelegationAllowExternalWarmTargets
		}
		if req.DelegationAcceptDelegations != nil {
			del.AcceptDelegations = *req.DelegationAcceptDelegations
		}
		// The UI sends positive "enabled" flags; the config stores the opt-out.
		if req.DelegationConclusionGate != nil {
			del.ConclusionGateDisabled = !*req.DelegationConclusionGate
		}
		if req.DelegationBreaker != nil {
			del.BreakerDisabled = !*req.DelegationBreaker
		}
		if req.DelegationMaxTaskRetries != nil {
			if *req.DelegationMaxTaskRetries < 0 {
				writeError(w, http.StatusBadRequest, "delegation_max_task_retries must be >= 0")
				return
			}
			del.MaxTaskRetries = *req.DelegationMaxTaskRetries
		}
		if req.DelegationRateLimitCooldown != nil {
			if _, err := time.ParseDuration(*req.DelegationRateLimitCooldown); err != nil {
				writeError(w, http.StatusBadRequest, "invalid delegation_rate_limit_cooldown (e.g. 5m, 30s)")
				return
			}
			del.RateLimitCooldown = *req.DelegationRateLimitCooldown
		}
		if req.DelegationRecentSuccessWindow != nil {
			if _, err := time.ParseDuration(*req.DelegationRecentSuccessWindow); err != nil {
				writeError(w, http.StatusBadRequest, "invalid delegation_recent_success_window (e.g. 2m, 30s)")
				return
			}
			del.RecentSuccessWindow = *req.DelegationRecentSuccessWindow
		}
		if req.DelegationEventLog != nil {
			del.EventLogDisabled = !*req.DelegationEventLog
		}
		if req.DelegationEventLogMaxEntries != nil {
			if *req.DelegationEventLogMaxEntries < 0 {
				writeError(w, http.StatusBadRequest, "delegation_event_log_max_entries must be >= 0")
				return
			}
			del.EventLogMaxEntries = *req.DelegationEventLogMaxEntries
		}
		if err := config.UpdateMesnadaDelegation(del); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update delegation settings")
			return
		}
	}

	if req.OrchestratorMaxParallel != nil || req.OrchestratorMaxPerEngine != nil ||
		req.OrchestratorClaimTTL != nil || req.OrchestratorDispatchInterval != nil {
		orch := config.Get().Mesnada.Orchestrator
		if req.OrchestratorMaxParallel != nil {
			if *req.OrchestratorMaxParallel < 1 {
				writeError(w, http.StatusBadRequest, "orchestrator_max_parallel must be >= 1")
				return
			}
			orch.MaxParallel = *req.OrchestratorMaxParallel
		}
		if req.OrchestratorMaxPerEngine != nil {
			if *req.OrchestratorMaxPerEngine < 0 {
				writeError(w, http.StatusBadRequest, "orchestrator_max_per_engine must be >= 0")
				return
			}
			orch.MaxPerEngine = *req.OrchestratorMaxPerEngine
		}
		if req.OrchestratorClaimTTL != nil {
			if _, err := time.ParseDuration(*req.OrchestratorClaimTTL); err != nil {
				writeError(w, http.StatusBadRequest, "invalid orchestrator_claim_ttl (e.g. 2m, 30s)")
				return
			}
			orch.ClaimTTL = *req.OrchestratorClaimTTL
		}
		if req.OrchestratorDispatchInterval != nil {
			if _, err := time.ParseDuration(*req.OrchestratorDispatchInterval); err != nil {
				writeError(w, http.StatusBadRequest, "invalid orchestrator_dispatch_interval (e.g. 10s, 1m)")
				return
			}
			orch.DispatchInterval = *req.OrchestratorDispatchInterval
		}
		if err := config.UpdateMesnadaOrchestrator(orch); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update orchestrator settings")
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
