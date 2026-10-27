package client

import (
	"context"
	"encoding/json"
	"errors"
)

// Model auto mode and the shared decision model (settings pages
// "Auto mode" and "Decision model").

// FieldError is one validation error of a config PUT (`field` is the full
// path, e.g. `modelAutoMode.routes[0].description`).
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ParseAPIError splits a failed config call into the message and the field
// errors of the server's {"error","errors":[...]} body.
func ParseAPIError(err error) (string, []FieldError) {
	if err == nil {
		return "", nil
	}
	var ae *APIError
	if errors.As(err, &ae) {
		var body struct {
			Error  string       `json:"error"`
			Errors []FieldError `json:"errors"`
		}
		if json.Unmarshal([]byte(ae.Body), &body) == nil && (body.Error != "" || len(body.Errors) > 0) {
			if body.Error == "" {
				body.Error = ae.Error()
			}
			return body.Error, body.Errors
		}
	}
	return err.Error(), nil
}

// AutoModelID is the reserved model id of the "Auto" entry.
const AutoModelID = "auto"

// ModelAutoRoute is one route of model auto mode.
type ModelAutoRoute struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Model       string   `json:"model"`
	Fallbacks   []string `json:"fallbacks"`
	Disabled    bool     `json:"disabled"`
}

// ModelAutoMode is GET/PUT /api/v1/config/model-auto-mode.
type ModelAutoMode struct {
	Enabled        bool             `json:"enabled"`
	DefaultAuto    bool             `json:"defaultAuto"`
	AutoSelected   bool             `json:"autoSelected,omitempty"`
	Threshold      float64          `json:"threshold"`
	MinConfidence  float64          `json:"minConfidence"`
	HistoryPrompts int              `json:"historyPrompts"`
	Routes         []ModelAutoRoute `json:"routes"`
	Warnings       []string         `json:"warnings,omitempty"`
}

// GetModelAutoMode reads the auto mode configuration.
func (c *Client) GetModelAutoMode(ctx context.Context) (ModelAutoMode, error) {
	var m ModelAutoMode
	err := c.Get(ctx, "/api/v1/config/model-auto-mode", &m)
	return m, err
}

// PutModelAutoMode saves the editable block (never the decision provider).
func (c *Client) PutModelAutoMode(ctx context.Context, m ModelAutoMode) (ModelAutoMode, error) {
	body := struct {
		Enabled        bool             `json:"enabled"`
		DefaultAuto    bool             `json:"defaultAuto"`
		Threshold      float64          `json:"threshold"`
		MinConfidence  float64          `json:"minConfidence"`
		HistoryPrompts int              `json:"historyPrompts"`
		Routes         []ModelAutoRoute `json:"routes"`
	}{m.Enabled, m.DefaultAuto, m.Threshold, m.MinConfidence, m.HistoryPrompts, m.Routes}
	if body.Routes == nil {
		body.Routes = []ModelAutoRoute{}
	}
	var out ModelAutoMode
	err := c.Put(ctx, "/api/v1/config/model-auto-mode", body, &out)
	return out, err
}

// PlaygroundDecision is the routing decision of the playground.
type PlaygroundDecision struct {
	RouteID        string             `json:"routeId"`
	Matched        bool               `json:"matched"`
	Probability    float64            `json:"probability"`
	Confidence     float64            `json:"confidence"`
	Probabilities  map[string]float64 `json:"probabilities,omitempty"`
	Candidates     []string           `json:"candidates,omitempty"`
	Reason         string             `json:"reason"`
	Error          string             `json:"error,omitempty"`
	RouterProvider string             `json:"routerProvider,omitempty"`
	RouterModel    string             `json:"routerModel,omitempty"`
	LatencyMs      int64              `json:"latencyMs"`
	CostUSD        *float64           `json:"costUsd,omitempty"`
}

// PlaygroundResponse is POST /api/v1/model-auto-mode/playground.
type PlaygroundResponse struct {
	Decision         PlaygroundDecision `json:"decision"`
	UsableCandidates []string           `json:"usableCandidates,omitempty"`
	Skipped          map[string]string  `json:"skipped,omitempty"`
}

// ModelAutoPlayground routes a prompt against the (unsaved) config.
func (c *Client) ModelAutoPlayground(ctx context.Context, prompt string, history []string, cfg ModelAutoMode) (PlaygroundResponse, error) {
	var out PlaygroundResponse
	err := c.Post(ctx, "/api/v1/model-auto-mode/playground", map[string]any{
		"prompt": prompt, "history": history, "config": cfg,
	}, &out)
	return out, err
}

// DecisionRouter is the router block of the decision model config.
type DecisionRouter struct {
	Provider         string            `json:"provider"`
	BaseURL          string            `json:"baseURL"`
	Model            string            `json:"model"`
	KeepAlive        string            `json:"keepAlive"`
	Headers          map[string]string `json:"headers"`
	APIKey           string            `json:"apiKey,omitempty"`
	EffectiveBaseURL string            `json:"effectiveBaseURL,omitempty"`
	APIKeySet        bool              `json:"apiKeySet,omitempty"`
	APIKeyMasked     string            `json:"apiKeyMasked,omitempty"`
}

// DecisionModel is GET/PUT /api/v1/config/decision-model.
type DecisionModel struct {
	Router    DecisionRouter `json:"router"`
	TimeoutMs int            `json:"timeoutMs"`
	Warnings  []string       `json:"warnings,omitempty"`
}

// GetDecisionModel reads the shared decision model config.
func (c *Client) GetDecisionModel(ctx context.Context) (DecisionModel, error) {
	var d DecisionModel
	err := c.Get(ctx, "/api/v1/config/decision-model", &d)
	return d, err
}

// PutDecisionModel saves the decision model; clearKey also drops the
// stored API key.
func (c *Client) PutDecisionModel(ctx context.Context, d DecisionModel, clearKey bool) (DecisionModel, error) {
	r := d.Router
	r.EffectiveBaseURL, r.APIKeySet, r.APIKeyMasked = "", false, ""
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	body := map[string]any{"router": r, "timeoutMs": d.TimeoutMs}
	if clearKey {
		body["clearApiKey"] = true
	}
	var out DecisionModel
	err := c.Put(ctx, "/api/v1/config/decision-model", body, &out)
	return out, err
}

func routerPayload(r DecisionRouter) map[string]any {
	h := r.Headers
	if h == nil {
		h = map[string]string{}
	}
	m := map[string]any{"provider": r.Provider, "baseURL": r.BaseURL, "model": r.Model, "keepAlive": r.KeepAlive, "headers": h}
	if r.APIKey != "" {
		m["apiKey"] = r.APIKey
	}
	return m
}

// RouterModel is a model offered by the decision provider.
type RouterModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// RouterModels is POST /api/v1/decision-model/router/models.
type RouterModels struct {
	Models      []RouterModel `json:"models"`
	Status      string        `json:"status"`
	Hint        string        `json:"hint,omitempty"`
	Error       string        `json:"error,omitempty"`
	Suggestions []string      `json:"suggestions,omitempty"`
}

// DecisionRouterModels lists the models of the draft router.
func (c *Client) DecisionRouterModels(ctx context.Context, r DecisionRouter, showAll bool) (RouterModels, error) {
	var out RouterModels
	err := c.Post(ctx, "/api/v1/decision-model/router/models", map[string]any{"router": routerPayload(r), "showAll": showAll}, &out)
	return out, err
}

// RouterPullJob is an Ollama pull in progress.
type RouterPullJob struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	State     string `json:"state"`
	Status    string `json:"status,omitempty"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	Error     string `json:"error,omitempty"`
}

// DecisionRouterPull starts a pull of model on the draft router.
func (c *Client) DecisionRouterPull(ctx context.Context, model string, r DecisionRouter) (RouterPullJob, error) {
	var out RouterPullJob
	err := c.Post(ctx, "/api/v1/decision-model/router/pull", map[string]any{"model": model, "router": routerPayload(r)}, &out)
	return out, err
}

// DecisionRouterPullStatus polls a pull job.
func (c *Client) DecisionRouterPullStatus(ctx context.Context, id string) (RouterPullJob, error) {
	var out RouterPullJob
	err := c.Get(ctx, "/api/v1/decision-model/router/pull/"+Esc(id), &out)
	return out, err
}

// HealthReport is the decision router health.
type HealthReport struct {
	OK              bool     `json:"ok"`
	Kind            string   `json:"kind,omitempty"`
	Model           string   `json:"model,omitempty"`
	Version         string   `json:"version,omitempty"`
	Reachable       bool     `json:"reachable"`
	Authorized      bool     `json:"authorized"`
	VersionOK       bool     `json:"versionOK"`
	ModelPresent    bool     `json:"modelPresent"`
	IsDecisionModel bool     `json:"isDecisionModel"`
	Remote          bool     `json:"remote"`
	LatencyMs       int64    `json:"latencyMs"`
	Problems        []string `json:"problems,omitempty"`
}

// RouterTest is POST /api/v1/decision-model/router/test.
type RouterTest struct {
	OK       bool          `json:"ok"`
	Problems []string      `json:"problems,omitempty"`
	Report   *HealthReport `json:"report,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// DecisionRouterTest checks the connection of the draft router.
func (c *Client) DecisionRouterTest(ctx context.Context, r DecisionRouter) (RouterTest, error) {
	var out RouterTest
	err := c.Post(ctx, "/api/v1/decision-model/router/test", map[string]any{"router": routerPayload(r)}, &out)
	return out, err
}

// DecisionHealth is GET /api/v1/decision-model/router/health (saved router).
type DecisionHealth struct {
	OK     bool          `json:"ok"`
	Report *HealthReport `json:"report,omitempty"`
	Error  string        `json:"error,omitempty"`
}

// DecisionRouterHealth reads the health of the saved router.
func (c *Client) DecisionRouterHealth(ctx context.Context) (DecisionHealth, error) {
	var out DecisionHealth
	err := c.Get(ctx, "/api/v1/decision-model/router/health", &out)
	return out, err
}

// MesnadaEngine is a detected subagent engine.
type MesnadaEngine struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Binary    string `json:"binary"`
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Enabled   bool   `json:"enabled"`
	Locked    bool   `json:"locked"`
}

// MesnadaEngines is GET /api/v1/mesnada/engines.
func (c *Client) MesnadaEngines(ctx context.Context) ([]MesnadaEngine, error) {
	var out struct {
		Engines []MesnadaEngine `json:"engines"`
	}
	err := c.Get(ctx, "/api/v1/mesnada/engines", &out)
	return out.Engines, err
}
