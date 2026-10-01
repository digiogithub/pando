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
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/logging"
)

const decisionModelFieldPrefix = "decisionModel."

// DecisionRouterResponse is the router block returned by GET. The API key is
// never included: only whether one is stored and a masked tail of it.
type DecisionRouterResponse = ModelAutoRouterResponse

// DecisionModelResponse is the body of GET/PUT /api/v1/config/decision-model.
type DecisionModelResponse struct {
	Router    DecisionRouterResponse `json:"router"`
	TimeoutMs int                    `json:"timeoutMs"`
	Warnings  []string               `json:"warnings"`
}

// decisionModelRequest is the PUT body: the block itself plus clearApiKey.
type decisionModelRequest struct {
	config.DecisionModelConfig
	ClearAPIKey bool `json:"clearApiKey"`
}

func savedDecisionModel() config.DecisionModelConfig {
	if c := config.Get(); c != nil {
		return c.DecisionModel
	}
	return config.DecisionModelConfig{}
}

func savedAutoMode() config.ModelAutoModeConfig {
	if c := config.Get(); c != nil {
		return c.ModelAutoMode
	}
	return config.ModelAutoModeConfig{}
}

func buildDecisionRouterResponse(r config.DecisionRouterConfig) DecisionRouterResponse {
	rawKey := strings.TrimSpace(r.APIKey)
	masked := ""
	if rawKey != "" {
		if strings.HasPrefix(rawKey, "$") {
			masked = config.MaskAPIKey(rawKey)
		} else if eff := r.EffectiveAPIKey(); eff != "" {
			masked = config.MaskAPIKey(eff)
		} else {
			masked = maskedKeyPrefix
		}
	}
	return DecisionRouterResponse{
		Provider:         r.EffectiveProvider(),
		BaseURL:          r.BaseURL,
		EffectiveBaseURL: r.EffectiveBaseURL(),
		Model:            r.Model,
		KeepAlive:        r.KeepAlive,
		Headers:          r.Headers,
		APIKeySet:        rawKey != "",
		APIKeyMasked:     masked,
	}
}

func buildDecisionModelResponse(d config.DecisionModelConfig) DecisionModelResponse {
	_, warnings := config.ValidateDecisionModel(d)
	if warnings == nil {
		warnings = []string{}
	}
	return DecisionModelResponse{
		Router:    buildDecisionRouterResponse(d.Router),
		TimeoutMs: d.TimeoutMs,
		Warnings:  warnings,
	}
}

func writeDecisionModelValidation(w http.ResponseWriter, errs []config.FieldError) {
	out := make([]modelAutoFieldError, len(errs))
	for i, e := range errs {
		out[i] = modelAutoFieldError{Field: decisionModelFieldPrefix + e.Field, Message: e.Message}
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error":  "invalid decisionModel configuration",
		"errors": out,
	})
}

// handleConfigDecisionModel handles GET/PUT /api/v1/config/decision-model.
func (s *Server) handleConfigDecisionModel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, buildDecisionModelResponse(cfg.DecisionModel))
	case http.MethodPut:
		s.handlePutConfigDecisionModel(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePutConfigDecisionModel(w http.ResponseWriter, r *http.Request) {
	if config.Get() == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	var req decisionModelRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	d := req.DecisionModelConfig
	// The UI echoes the masked key back when the user did not touch the field.
	if strings.HasPrefix(strings.TrimSpace(d.Router.APIKey), maskedKeyPrefix) {
		d.Router.APIKey = ""
	}
	clearKey := req.ClearAPIKey && strings.TrimSpace(d.Router.APIKey) == ""

	if errs, _ := config.ValidateDecisionModel(d); len(errs) > 0 {
		writeDecisionModelValidation(w, errs)
		return
	}
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
	writeJSON(w, http.StatusOK, buildDecisionModelResponse(config.Get().DecisionModel))
}

// handleDecisionModelAPIKey handles DELETE /api/v1/config/decision-model/api-key.
func (s *Server) handleDecisionModelAPIKey(w http.ResponseWriter, r *http.Request) {
	if config.Get() == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	if err := config.ClearDecisionModelAPIKey(); err != nil {
		writeConfigError(w, http.StatusInternalServerError, "failed to clear router API key: "+err.Error(), err)
		return
	}
	writeJSON(w, http.StatusOK, buildDecisionModelResponse(config.Get().DecisionModel))
}

var legacyRouterAliasOnce sync.Once

// legacyRouterAlias serves the deprecated /api/v1/model-auto-mode/router/*
// routes with the decision-model handler, logging the deprecation once.
func legacyRouterAlias(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		legacyRouterAliasOnce.Do(func() {
			logging.Warn("deprecated API: /api/v1/model-auto-mode/router/* is an alias, use /api/v1/decision-model/router/*")
		})
		w.Header().Set("Deprecation", "true")
		h(w, r)
	}
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
func resolveRouter(w http.ResponseWriter, r *http.Request) (config.DecisionModelConfig, bool, error) {
	saved := savedDecisionModel()
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

// handleDecisionRouterModels handles GET|POST /api/v1/decision-model/router/models.
func (s *Server) handleDecisionRouterModels(w http.ResponseWriter, r *http.Request) {
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
	p, err := modelrouter.ProviderWithTimeout(m.Router, routerCallTimeout)
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

// handleDecisionRouterTest handles POST /api/v1/decision-model/router/test.
// It probes the saved router, or the draft router in the body, without caching.
func (s *Server) handleDecisionRouterTest(w http.ResponseWriter, r *http.Request) {
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
	p, err := modelrouter.ProviderWithTimeout(m.Router, routerCallTimeout)
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

// handleDecisionRouterHealth handles GET /api/v1/decision-model/router/health:
// the cached (60s) health of the saved router.
func (s *Server) handleDecisionRouterHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	m := savedDecisionModel()
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

// handleDecisionRouterPull handles POST /api/v1/decision-model/router/pull
// {model, router?}. It starts an Ollama pull of a suggested decision model on
// the router's effective base URL and returns the job (202); poll
// GET /api/v1/decision-model/router/pull/{id} for progress.
func (s *Server) handleDecisionRouterPull(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model  string                       `json:"model"`
		Router *config.DecisionRouterConfig `json:"router"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	saved := savedDecisionModel()
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

// handleDecisionRouterPullJob handles GET /api/v1/decision-model/router/pull/{id}.
func (s *Server) handleDecisionRouterPullJob(w http.ResponseWriter, r *http.Request) {
	job, ok := setupOllama.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
