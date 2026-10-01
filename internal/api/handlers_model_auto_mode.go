package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/llm/models"
)

const (
	modelAutoFieldPrefix = "modelAutoMode."
	maskedKeyPrefix      = "••••"
	routerCallTimeout    = 20 * time.Second
	playgroundMaxPrompt  = 64 * 1024
)

// ModelAutoRouterResponse is the router block returned by GET. The API key is
// never included: only whether one is stored and a masked tail of it.
type ModelAutoRouterResponse struct {
	Provider         config.DecisionProviderKind `json:"provider"`
	BaseURL          string                      `json:"baseURL"`
	EffectiveBaseURL string                      `json:"effectiveBaseURL"`
	Model            string                      `json:"model"`
	KeepAlive        string                      `json:"keepAlive,omitempty"`
	Headers          map[string]string           `json:"headers,omitempty"`
	APIKeySet        bool                        `json:"apiKeySet"`
	APIKeyMasked     string                      `json:"apiKeyMasked"`
}

// ModelAutoModeResponse is the body of GET/PUT /api/v1/config/model-auto-mode.
type ModelAutoModeResponse struct {
	Enabled        bool                    `json:"enabled"`
	DefaultAuto    bool                    `json:"defaultAuto"`
	Selected       *bool                   `json:"selected,omitempty"`
	AutoSelected   bool                    `json:"autoSelected"`
	Router         ModelAutoRouterResponse `json:"router"`
	Threshold      float64                 `json:"threshold"`
	MinConfidence  float64                 `json:"minConfidence"`
	TimeoutMs      int                     `json:"timeoutMs"`
	HistoryPrompts int                     `json:"historyPrompts"`
	Routes         []config.ModelAutoRoute `json:"routes"`
	Warnings       []string                `json:"warnings"`
}

// modelAutoModeRequest is the PUT body: the block itself plus clearApiKey.
type modelAutoModeRequest struct {
	config.ModelAutoModeConfig
	ClearAPIKey bool `json:"clearApiKey"`
}

type modelAutoFieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// buildModelAutoModeResponse renders the block. The read-only router/timeoutMs
// are an alias of the shared decision model, kept for one release.
func buildModelAutoModeResponse(m config.ModelAutoModeConfig, d config.DecisionModelConfig) ModelAutoModeResponse {
	_, warnings := config.ValidateModelAutoMode(m, d)
	_, dWarnings := config.ValidateDecisionModel(d)
	warnings = append(warnings, dWarnings...)
	if warnings == nil {
		warnings = []string{}
	}
	routes := m.Routes
	if routes == nil {
		routes = []config.ModelAutoRoute{}
	}
	return ModelAutoModeResponse{
		Enabled:        m.Enabled,
		DefaultAuto:    m.DefaultAuto,
		Selected:       m.Selected,
		AutoSelected:   m.AutoSelected(),
		Router:         buildDecisionRouterResponse(d.Router),
		Threshold:      m.Threshold,
		MinConfidence:  m.MinConfidence,
		TimeoutMs:      d.TimeoutMs,
		HistoryPrompts: m.HistoryPrompts,
		Routes:         routes,
		Warnings:       warnings,
	}
}

// modelAutoValidationErrors validates the block together with the decision
// model it runs with. Both are reported under the modelAutoMode. prefix so the
// legacy router.* field names keep matching what the current UIs expect.
func modelAutoValidationErrors(m config.ModelAutoModeConfig, d config.DecisionModelConfig, checkDecision bool) []config.FieldError {
	var errs []config.FieldError
	if checkDecision {
		dErrs, _ := config.ValidateDecisionModel(d)
		errs = append(errs, dErrs...)
	}
	aErrs, _ := config.ValidateModelAutoMode(m, d)
	for _, e := range aErrs {
		if e.Field == "router.model" && checkDecision && hasFieldError(errs, "router.model") {
			continue
		}
		errs = append(errs, e)
	}
	return errs
}

func hasFieldError(errs []config.FieldError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func writeModelAutoValidation(w http.ResponseWriter, errs []config.FieldError, prefix string) {
	out := make([]modelAutoFieldError, len(errs))
	for i, e := range errs {
		out[i] = modelAutoFieldError{Field: prefix + e.Field, Message: e.Message}
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error":  "invalid modelAutoMode configuration",
		"errors": out,
	})
}

// handleConfigModelAutoMode handles GET/PUT /api/v1/config/model-auto-mode.
func (s *Server) handleConfigModelAutoMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, buildModelAutoModeResponse(cfg.ModelAutoMode, cfg.DecisionModel))
	case http.MethodPut:
		s.handlePutConfigModelAutoMode(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePutConfigModelAutoMode(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	var req modelAutoModeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	m := req.ModelAutoModeConfig
	m.LegacyRouter, m.LegacyTimeoutMs = nil, 0

	// The decision provider moved to /api/v1/config/decision-model. Until the
	// UIs migrate, a router still sent here is forwarded to the shared block.
	d := cfg.DecisionModel
	var warnings []string
	forwardRouter := req.LegacyRouter != nil
	clearKey := false
	if forwardRouter {
		warnings = append(warnings, "modelAutoMode.router is deprecated: it was saved to decisionModel.router; use PUT /api/v1/config/decision-model")
		d.Router = *req.LegacyRouter
		d.TimeoutMs = req.LegacyTimeoutMs
		// The UI echoes the masked key back when the user did not touch the field.
		if strings.HasPrefix(strings.TrimSpace(d.Router.APIKey), maskedKeyPrefix) {
			d.Router.APIKey = ""
		}
		clearKey = req.ClearAPIKey && strings.TrimSpace(d.Router.APIKey) == ""
	}

	// Validate against the decision model the block will run with.
	effective := d
	if forwardRouter && strings.TrimSpace(d.Router.APIKey) == "" {
		effective.Router.APIKey = cfg.DecisionModel.Router.APIKey
	}
	if errs := modelAutoValidationErrors(m, effective, forwardRouter); len(errs) > 0 {
		writeModelAutoValidation(w, errs, modelAutoFieldPrefix)
		return
	}
	if forwardRouter {
		if err := config.UpdateDecisionModel(d); err != nil {
			writeConfigError(w, http.StatusBadRequest, "failed to update decisionModel: "+err.Error(), err)
			return
		}
		if clearKey {
			if err := config.ClearDecisionModelAPIKey(); err != nil {
				writeConfigError(w, http.StatusInternalServerError, "failed to clear router API key: "+err.Error(), err)
				return
			}
		}
	}
	if err := config.UpdateModelAutoMode(m); err != nil {
		writeConfigError(w, http.StatusBadRequest, "failed to update modelAutoMode: "+err.Error(), err)
		return
	}
	cur := config.Get()
	resp := buildModelAutoModeResponse(cur.ModelAutoMode, cur.DecisionModel)
	resp.Warnings = append(resp.Warnings, warnings...)
	writeJSON(w, http.StatusOK, resp)
}

// PlaygroundRequest is the body of POST /api/v1/model-auto-mode/playground.
type PlaygroundRequest struct {
	Prompt          string   `json:"prompt"`
	History         []string `json:"history,omitempty"`
	AttachmentNames []string `json:"attachmentNames,omitempty"`
	HasAttachments  bool     `json:"hasAttachments,omitempty"`
	// Config is an optional unsaved draft of the whole block. Absent means the saved block.
	Config *config.ModelAutoModeConfig `json:"config,omitempty"`
	// Decision is an optional unsaved draft of the shared decision model.
	Decision *config.DecisionModelConfig `json:"decision,omitempty"`
}

// PlaygroundDecision is the JSON form of modelrouter.Decision.
type PlaygroundDecision struct {
	RouteID        string             `json:"routeId"`
	Matched        bool               `json:"matched"`
	Probability    float64            `json:"probability"`
	Confidence     float64            `json:"confidence"`
	Probabilities  map[string]float64 `json:"probabilities"`
	Candidates     []string           `json:"candidates"`
	Reason         string             `json:"reason"`
	ErrClass       string             `json:"errClass,omitempty"`
	Error          string             `json:"error,omitempty"`
	RouterProvider string             `json:"routerProvider"`
	RouterModel    string             `json:"routerModel"`
	LatencyMs      int64              `json:"latencyMs"`
	CostUSD        *float64           `json:"costUsd,omitempty"`
	InputTokens    int                `json:"inputTokens,omitempty"`
}

// handleModelAutoPlayground handles POST /api/v1/model-auto-mode/playground: a
// dry-run of the routing decision for a sample prompt. Only the decision
// provider is called; no chat model is ever invoked.
func (s *Server) handleModelAutoPlayground(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	var req PlaygroundRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "'prompt' field required")
		return
	}
	if len(req.Prompt) > playgroundMaxPrompt {
		writeError(w, http.StatusRequestEntityTooLarge, "prompt too large")
		return
	}

	m := cfg.ModelAutoMode
	dec := cfg.DecisionModel
	if req.Config != nil {
		m = *req.Config
		// A draft may still carry a router (legacy UI): it overrides the shared one for this dry-run.
		if m.LegacyRouter != nil {
			dec.Router = draftRouter(cfg.DecisionModel.Router, *m.LegacyRouter)
			dec.TimeoutMs = m.LegacyTimeoutMs
		}
		if req.Decision != nil {
			dec = *req.Decision
			dec.Router = draftRouter(cfg.DecisionModel.Router, dec.Router)
		}
		m.LegacyRouter, m.LegacyTimeoutMs = nil, 0
		if errs := modelAutoValidationErrors(m, dec, true); len(errs) > 0 {
			writeModelAutoValidation(w, errs, modelAutoFieldPrefix)
			return
		}
	}
	secret := dec.Router.EffectiveAPIKey()

	var engine *modelrouter.Engine
	var err error
	if req.Config != nil {
		engine, err = modelrouter.NewEngine(dec)
	} else {
		engine, err = modelrouter.ForConfig(dec)
	}
	if err != nil {
		writeScrubbedJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, secret)
		return
	}

	coder := models.ModelID("")
	if a, ok := cfg.Agents[config.AgentCoder]; ok {
		coder = a.Model
	}
	in := modelrouter.Input{
		Prompt:          req.Prompt,
		History:         req.History,
		AttachmentNames: req.AttachmentNames,
		HasAttachments:  req.HasAttachments || len(req.AttachmentNames) > 0,
		CoderModel:      coder,
	}
	ctx, cancel := context.WithTimeout(r.Context(), routerCallTimeout)
	defer cancel()
	d := engine.Route(ctx, m, in)

	budget := engine.ContextBudget(ctx)
	state := modelrouter.BuildState(in, m.HistoryPrompts, budget)

	usable, skipped := modelrouter.FilterCandidates(d.Candidates, modelrouter.DefaultLookup(cfg), in.HasAttachments, modelrouter.EstimateTokens(state))
	cands := make([]string, len(d.Candidates))
	for i, c := range d.Candidates {
		cands[i] = string(c)
	}
	usableOut := make([]string, len(usable))
	for i, c := range usable {
		usableOut[i] = string(c)
	}
	skippedOut := make(map[string]string, len(skipped))
	for id, why := range skipped {
		skippedOut[string(id)] = why
	}
	pdec := PlaygroundDecision{
		RouteID: d.RouteID, Matched: d.Matched, Probability: d.Probability, Confidence: d.Confidence,
		Probabilities: d.Probabilities, Candidates: cands, Reason: d.Reason, ErrClass: d.ErrClass,
		RouterProvider: d.RouterProvider, RouterModel: d.RouterModel, LatencyMs: d.LatencyMs,
		CostUSD: d.CostUSD, InputTokens: d.InputTokens,
	}
	if d.Err != nil {
		pdec.Error = d.Err.Error()
	}
	writeScrubbedJSON(w, http.StatusOK, map[string]any{
		"decision":         pdec,
		"state":            state,
		"usableCandidates": usableOut,
		"skipped":          skippedOut,
	}, secret)
}
