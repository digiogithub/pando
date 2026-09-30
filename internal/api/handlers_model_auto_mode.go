package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone"
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

func buildModelAutoModeResponse(m config.ModelAutoModeConfig) ModelAutoModeResponse {
	_, warnings := config.ValidateModelAutoMode(m)
	if warnings == nil {
		warnings = []string{}
	}
	routes := m.Routes
	if routes == nil {
		routes = []config.ModelAutoRoute{}
	}
	rawKey := strings.TrimSpace(m.Router.APIKey)
	masked := ""
	if rawKey != "" {
		if strings.HasPrefix(rawKey, "$") {
			masked = config.MaskAPIKey(rawKey)
		} else if eff := m.Router.EffectiveAPIKey(); eff != "" {
			masked = config.MaskAPIKey(eff)
		} else {
			masked = maskedKeyPrefix
		}
	}
	return ModelAutoModeResponse{
		Enabled:      m.Enabled,
		DefaultAuto:  m.DefaultAuto,
		Selected:     m.Selected,
		AutoSelected: m.AutoSelected(),
		Router: ModelAutoRouterResponse{
			Provider:         m.Router.EffectiveProvider(),
			BaseURL:          m.Router.BaseURL,
			EffectiveBaseURL: m.Router.EffectiveBaseURL(),
			Model:            m.Router.Model,
			KeepAlive:        m.Router.KeepAlive,
			Headers:          m.Router.Headers,
			APIKeySet:        rawKey != "",
			APIKeyMasked:     masked,
		},
		Threshold:      m.Threshold,
		MinConfidence:  m.MinConfidence,
		TimeoutMs:      m.TimeoutMs,
		HistoryPrompts: m.HistoryPrompts,
		Routes:         routes,
		Warnings:       warnings,
	}
}

func writeModelAutoValidation(w http.ResponseWriter, errs []config.FieldError) {
	out := make([]modelAutoFieldError, len(errs))
	for i, e := range errs {
		out[i] = modelAutoFieldError{Field: modelAutoFieldPrefix + e.Field, Message: e.Message}
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
		writeJSON(w, http.StatusOK, buildModelAutoModeResponse(cfg.ModelAutoMode))
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
	// The UI echoes the masked key back when the user did not touch the field.
	if strings.HasPrefix(strings.TrimSpace(m.Router.APIKey), maskedKeyPrefix) {
		m.Router.APIKey = ""
	}
	clearKey := req.ClearAPIKey && strings.TrimSpace(m.Router.APIKey) == ""

	if errs, _ := config.ValidateModelAutoMode(m); len(errs) > 0 {
		writeModelAutoValidation(w, errs)
		return
	}
	if err := config.UpdateModelAutoMode(m); err != nil {
		writeConfigError(w, http.StatusBadRequest, "failed to update modelAutoMode: "+err.Error(), err)
		return
	}
	if clearKey {
		if err := config.ClearModelAutoModeAPIKey(); err != nil {
			writeConfigError(w, http.StatusInternalServerError, "failed to clear router API key: "+err.Error(), err)
			return
		}
	}
	writeJSON(w, http.StatusOK, buildModelAutoModeResponse(config.Get().ModelAutoMode))
}

// draftRouter overlays an optional draft router block on the saved one. A draft
// without an API key reuses the stored key only when it targets the same
// provider and base URL, so a stored credential is never sent to a host the
// user typed but has not saved.
func draftRouter(saved, draft config.DecisionRouterConfig) config.DecisionRouterConfig {
	if strings.TrimSpace(draft.APIKey) != "" && !strings.HasPrefix(strings.TrimSpace(draft.APIKey), maskedKeyPrefix) {
		return draft
	}
	draft.APIKey = ""
	if saved.EffectiveProvider() == draft.EffectiveProvider() && saved.EffectiveBaseURL() == draft.EffectiveBaseURL() {
		draft.APIKey = saved.APIKey
	}
	return draft
}

func savedAutoMode() config.ModelAutoModeConfig {
	if c := config.Get(); c != nil {
		return c.ModelAutoMode
	}
	return config.ModelAutoModeConfig{}
}

// writeScrubbedJSON writes v as JSON with every secret replaced, so an upstream
// error message that echoes the credential can never reach the client.
func writeScrubbedJSON(w http.ResponseWriter, status int, v any, secrets ...string) {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(v)
	out := buf.Bytes()
	for _, sec := range secrets {
		sec = strings.TrimSpace(sec)
		if len(sec) < 4 {
			continue
		}
		for _, variant := range []string{sec, jsonEscape(sec)} {
			out = bytes.ReplaceAll(out, []byte(variant), []byte("***"))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}

// routerDraftBody is the optional body of the router endpoints.
type routerDraftBody struct {
	Router  *config.DecisionRouterConfig `json:"router"`
	ShowAll bool                         `json:"showAll"`
}

// resolveRouter reads the optional draft from the JSON body (POST) or the
// query (GET: provider, baseURL, model, showAll; never the key) and returns the
// router block to use.
func resolveRouter(w http.ResponseWriter, r *http.Request) (config.ModelAutoModeConfig, bool, error) {
	saved := savedAutoMode()
	m := saved
	showAll := r.URL.Query().Get("showAll") == "true" || r.URL.Query().Get("showAll") == "1"
	q := r.URL.Query()
	if q.Get("provider") != "" || q.Get("baseURL") != "" || q.Get("model") != "" {
		d := saved.Router
		if v := q.Get("provider"); v != "" {
			d.Provider = config.DecisionProviderKind(v)
		}
		if q.Has("baseURL") {
			d.BaseURL = q.Get("baseURL")
		}
		if v := q.Get("model"); v != "" {
			d.Model = v
		}
		d.APIKey = ""
		m.Router = draftRouter(saved.Router, d)
	}
	if r.Method == http.MethodPost && r.Body != nil {
		var body routerDraftBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			return m, false, err
		}
		showAll = showAll || body.ShowAll
		if body.Router != nil {
			m.Router = draftRouter(saved.Router, *body.Router)
		}
	}
	return m, showAll, nil
}

// handleModelAutoRouterModels handles GET|POST /api/v1/model-auto-mode/router/models.
func (s *Server) handleModelAutoRouterModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	m, showAll, err := resolveRouter(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	secret := m.Router.EffectiveAPIKey()
	p, err := modelrouter.ProviderFor(m.Router, routerCallTimeout)
	if err != nil {
		writeScrubbedJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, secret)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), routerCallTimeout)
	defer cancel()
	list, status, lerr := p.ListDecisionModels(ctx, showAll)
	if list == nil {
		list = []systemone.DecisionModel{}
	}
	resp := map[string]any{"models": list, "status": string(status)}
	if p.Kind() == systemone.KindOllama {
		resp["suggestions"] = ollamaDecisionSuggestions(ctx, p, list, showAll)
	}
	switch {
	case lerr != nil:
		resp["error"] = lerr.Error()
		resp["hint"] = "Could not list models; check the provider address and credentials, or type the model id."
	case status == systemone.ListUnsupported && len(list) == 0:
		resp["hint"] = "This provider cannot list models; type the model id."
	case status == systemone.ListUnsupported:
		resp["hint"] = "Ollama does not report model capabilities; upgrade to Ollama 0.35 or newer."
	case len(list) == 0 && p.Kind() == systemone.KindOllama:
		resp["hint"] = "No decision model installed. Try: " + systemone.OllamaPullHint
	case status == systemone.ListUnfiltered:
		resp["hint"] = "The provider does not mark decision models; showing the catalogue."
	}
	writeScrubbedJSON(w, http.StatusOK, resp, secret)
}

// handleModelAutoRouterTest handles POST /api/v1/model-auto-mode/router/test.
// It probes the saved router, or the draft router in the body, without caching.
func (s *Server) handleModelAutoRouterTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	m, _, err := resolveRouter(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	secret := m.Router.EffectiveAPIKey()
	p, err := modelrouter.ProviderFor(m.Router, routerCallTimeout)
	if err != nil {
		writeScrubbedJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": err.Error(),
		}, secret)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), routerCallTimeout)
	defer cancel()
	report := p.Health(ctx, strings.TrimSpace(m.Router.Model))
	writeScrubbedJSON(w, http.StatusOK, map[string]any{
		"ok":       report.OK,
		"report":   report,
		"problems": report.Problems,
	}, secret)
}

// handleModelAutoRouterHealth handles GET /api/v1/model-auto-mode/router/health:
// the cached (60s) health of the saved router.
func (s *Server) handleModelAutoRouterHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	m := savedAutoMode()
	secret := m.Router.EffectiveAPIKey()
	ctx, cancel := context.WithTimeout(r.Context(), routerCallTimeout)
	defer cancel()
	report, err := modelrouter.RouterHealth(ctx, m)
	if err != nil {
		writeScrubbedJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()}, secret)
		return
	}
	writeScrubbedJSON(w, http.StatusOK, map[string]any{"ok": report.OK, "report": report, "problems": report.Problems}, secret)
}

// PlaygroundRequest is the body of POST /api/v1/model-auto-mode/playground.
type PlaygroundRequest struct {
	Prompt          string   `json:"prompt"`
	History         []string `json:"history,omitempty"`
	AttachmentNames []string `json:"attachmentNames,omitempty"`
	HasAttachments  bool     `json:"hasAttachments,omitempty"`
	// Config is an optional unsaved draft of the whole block. Absent means the saved block.
	Config *config.ModelAutoModeConfig `json:"config,omitempty"`
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
	if req.Config != nil {
		m = *req.Config
		m.Router = draftRouter(cfg.ModelAutoMode.Router, m.Router)
		if errs, _ := config.ValidateModelAutoMode(m); len(errs) > 0 {
			writeModelAutoValidation(w, errs)
			return
		}
	}
	secret := m.Router.EffectiveAPIKey()

	var engine *modelrouter.Engine
	var err error
	if req.Config != nil {
		engine, err = modelrouter.NewEngine(m)
	} else {
		engine, err = modelrouter.ForConfig(m)
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
	d := engine.Route(ctx, in)

	budget := engine.Provider().ContextBudget(ctx, m.Router.Model)
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
	dec := PlaygroundDecision{
		RouteID: d.RouteID, Matched: d.Matched, Probability: d.Probability, Confidence: d.Confidence,
		Probabilities: d.Probabilities, Candidates: cands, Reason: d.Reason, ErrClass: d.ErrClass,
		RouterProvider: d.RouterProvider, RouterModel: d.RouterModel, LatencyMs: d.LatencyMs,
		CostUSD: d.CostUSD, InputTokens: d.InputTokens,
	}
	if d.Err != nil {
		dec.Error = d.Err.Error()
	}
	writeScrubbedJSON(w, http.StatusOK, map[string]any{
		"decision":         dec,
		"state":            state,
		"usableCandidates": usableOut,
		"skipped":          skippedOut,
	}, secret)
}

// ollamaDecisionSuggestions returns the suggested decision models that are not
// installed. It needs the full installed list, so it re-lists unfiltered when
// the caller's list was filtered.
func ollamaDecisionSuggestions(ctx context.Context, p systemone.DecisionProvider, listed []systemone.DecisionModel, showAll bool) []string {
	installed := listed
	if !showAll {
		if all, _, err := p.ListDecisionModels(ctx, true); err == nil {
			installed = all
		}
	}
	names := make([]string, 0, len(installed))
	for _, m := range installed {
		names = append(names, m.ID)
	}
	return systemone.MissingSuggestedOllamaModels(names)
}

// handleModelAutoRouterPull handles POST /api/v1/model-auto-mode/router/pull
// {model, router?}. It starts an Ollama pull of a suggested decision model on
// the router's effective base URL and returns the job (202); poll
// GET /api/v1/model-auto-mode/router/pull/{id} for progress.
func (s *Server) handleModelAutoRouterPull(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model  string                       `json:"model"`
		Router *config.DecisionRouterConfig `json:"router"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	saved := savedAutoMode()
	router := saved.Router
	if body.Router != nil {
		router = draftRouter(saved.Router, *body.Router)
	}
	if router.EffectiveProvider() != config.DecisionProviderOllama {
		writeError(w, http.StatusBadRequest, "pull is only available for the Ollama decision provider")
		return
	}
	if !systemone.IsSuggestedOllamaModel(body.Model) {
		writeError(w, http.StatusBadRequest, "model "+strconv.Quote(body.Model)+" is not a suggested decision model ("+strings.Join(systemone.SuggestedOllamaModels, ", ")+")")
		return
	}
	job, err := setupOllama.Pull(router.EffectiveBaseURL(), body.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

// handleModelAutoRouterPullJob handles GET /api/v1/model-auto-mode/router/pull/{id}.
func (s *Server) handleModelAutoRouterPullJob(w http.ResponseWriter, r *http.Request) {
	job, ok := setupOllama.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
