// Package modelrouter implements the pure decision engine of model auto mode:
// it turns (config, prompt, short history) into a routing Decision using a
// System One decision provider. It holds no per-session state.
package modelrouter

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone"
)

// Decision reasons.
const (
	ReasonMatched        = "matched"
	ReasonNoMatch        = "no_match"
	ReasonLowProbability = "low_probability"
	ReasonLowConfidence  = "low_confidence"
	ReasonRouterError    = "router_error"
	ReasonNoRoutes       = "no_routes"
	ReasonDisabled       = "disabled"
)

// Error classes carried in Decision.ErrClass.
const (
	ErrClassUnreachable   = "unreachable"
	ErrClassUnauthorized  = "unauthorized"
	ErrClassModelNotFound = "model_not_found"
	ErrClassTimeout       = "timeout"
	ErrClassTooLarge      = "too_large"
	ErrClassBadRequest    = "bad_request"
	ErrClassMalformed     = "malformed"
	ErrClassServer        = "server"
)

const (
	questionName     = "task"
	noneKey          = config.ModelAutoModeReservedRouteID
	noneDescription  = "None of the above / general or follow-up request"
	instructionsText = "Which task category best describes the user's latest request?"
	budgetTTL        = 5 * time.Minute
)

// Decision is the outcome of routing one prompt.
type Decision struct {
	RouteID       string // "" when no match
	Matched       bool
	Probability   float64
	Confidence    float64
	Probabilities map[string]float64
	// Candidates is [primary, fb1, fb2] of the route, or [coder] on no match or failure.
	Candidates     []models.ModelID
	Reason         string
	Err            error
	ErrClass       string
	RouterProvider string
	RouterModel    string
	LatencyMs      int64
	CostUSD        *float64
	InputTokens    int
}

// Input is what the engine needs to route one prompt.
type Input struct {
	Prompt          string
	History         []string // previous user prompts, newest last
	AttachmentNames []string
	HasAttachments  bool
	CoderModel      models.ModelID
}

// EngineOption customises NewEngine.
type EngineOption func(*Engine)

// WithProvider injects a decision provider (tests).
func WithProvider(p systemone.DecisionProvider) EngineOption {
	return func(e *Engine) { e.provider = p }
}

// Engine routes prompts. It is safe for concurrent use.
type Engine struct {
	cfg      config.ModelAutoModeConfig
	provider systemone.DecisionProvider

	mu        sync.Mutex
	budget    int
	budgetAt  time.Time
	budgetSet bool
}

// NewEngine builds an engine from the auto mode configuration.
func NewEngine(cfg config.ModelAutoModeConfig, opts ...EngineOption) (*Engine, error) {
	e := &Engine{cfg: cfg}
	for _, o := range opts {
		o(e)
	}
	if e.provider == nil {
		p, err := systemone.NewProvider(systemone.ProviderKind(cfg.Router.EffectiveProvider()), systemone.Options{
			BaseURL:   cfg.Router.EffectiveBaseURL(),
			APIKey:    cfg.Router.EffectiveAPIKey(),
			Headers:   cfg.Router.Headers,
			Timeout:   cfg.EffectiveTimeout(),
			KeepAlive: cfg.Router.KeepAlive,
		})
		if err != nil {
			return nil, err
		}
		e.provider = p
	}
	return e, nil
}

// Provider returns the decision provider used by the engine.
func (e *Engine) Provider() systemone.DecisionProvider { return e.provider }

var (
	cacheMu  sync.Mutex
	cacheKey string
	cacheEng *Engine
)

// ForConfig returns the process-wide engine for cfg, rebuilding it when the
// configuration content changes (hot reload).
func ForConfig(cfg config.ModelAutoModeConfig) (*Engine, error) {
	key := configKey(cfg)
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheEng != nil && cacheKey == key {
		return cacheEng, nil
	}
	e, err := NewEngine(cfg)
	if err != nil {
		return nil, err
	}
	cacheKey, cacheEng = key, e
	return e, nil
}

func configKey(cfg config.ModelAutoModeConfig) string {
	b, _ := json.Marshal(struct {
		C   config.ModelAutoModeConfig
		Key string
		URL string
	}{cfg, cfg.Router.EffectiveAPIKey(), cfg.Router.EffectiveBaseURL()})
	return string(b)
}

// Route classifies in.Prompt. It never fails: on any router failure it returns
// the coder model with Reason "router_error".
func (e *Engine) Route(ctx context.Context, in Input) Decision {
	routerModel := e.cfg.Router.Model
	d := Decision{
		RouterProvider: string(e.cfg.Router.EffectiveProvider()),
		RouterModel:    routerModel,
		Candidates:     coderOnly(in.CoderModel),
	}
	routes := e.cfg.EnabledRoutes()
	if len(routes) == 0 {
		d.Reason = ReasonNoRoutes
		return d
	}

	criteria := make([]systemone.Criterion, 0, len(routes)+1)
	overhead := EstimateTokens(instructionsText) + EstimateTokens(noneDescription) + 32
	for _, r := range routes {
		criteria = append(criteria, systemone.NewCriterion(r.ID, r.Description))
		overhead += EstimateTokens(r.ID) + EstimateTokens(r.Description) + 4
	}
	criteria = append(criteria, systemone.NewCriterion(noneKey, noneDescription))

	budget := e.contextBudget(ctx, routerModel) - overhead
	stateJSON := BuildState(in, e.cfg.HistoryPrompts, budget)

	req := systemone.Request{
		Model: routerModel,
		State: json.RawMessage(stateJSON),
		Questions: map[string]systemone.Question{
			questionName: {Type: "choice", Instructions: instructionsText, Criteria: criteria},
		},
	}
	if e.provider.Kind() == systemone.KindOllama {
		req.KeepAlive = e.provider.Client().KeepAlive()
	}

	callCtx, cancel := context.WithTimeout(ctx, e.cfg.EffectiveTimeout())
	defer cancel()
	start := time.Now()
	resp, err := e.provider.Client().Decide(callCtx, req)
	d.LatencyMs = time.Since(start).Milliseconds()
	if err == nil && (resp == nil || resp.Answers[questionName].Choice == "") {
		err = systemone.ErrMalformedResponse
	}
	if err != nil {
		d.Reason = ReasonRouterError
		d.Err = err
		d.ErrClass = classify(err)
		return d
	}

	d.InputTokens = resp.Usage.InputTokens
	d.CostUSD = resp.Usage.Cost
	ans := resp.Answers[questionName]
	d.Probabilities = ans.Probabilities
	d.Confidence = ans.Confidence
	d.Probability = ans.Probabilities[ans.Choice]

	if ans.Choice == noneKey {
		d.Reason = ReasonNoMatch
		return d
	}
	var route *config.ModelAutoRoute
	for i := range routes {
		if routes[i].ID == ans.Choice {
			route = &routes[i]
			break
		}
	}
	switch {
	case route == nil:
		d.Reason = ReasonNoMatch
	case d.Probability < e.cfg.EffectiveThreshold():
		d.Reason = ReasonLowProbability
	case e.cfg.MinConfidence > 0 && d.Confidence < e.cfg.MinConfidence:
		d.Reason = ReasonLowConfidence
	default:
		d.Reason = ReasonMatched
		d.Matched = true
		d.RouteID = route.ID
		d.Candidates = routeCandidates(*route)
	}
	return d
}

func coderOnly(coder models.ModelID) []models.ModelID {
	if coder == "" {
		return nil
	}
	return []models.ModelID{coder}
}

func routeCandidates(r config.ModelAutoRoute) []models.ModelID {
	out := make([]models.ModelID, 0, 1+len(r.Fallbacks))
	seen := map[models.ModelID]bool{}
	for _, id := range append([]models.ModelID{r.Model}, r.Fallbacks...) {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (e *Engine) contextBudget(ctx context.Context, model string) int {
	e.mu.Lock()
	if e.budgetSet && time.Since(e.budgetAt) < budgetTTL {
		b := e.budget
		e.mu.Unlock()
		return b
	}
	e.mu.Unlock()
	b := e.provider.ContextBudget(ctx, model)
	if b <= 0 {
		b = defaultBudgetTokens
	}
	e.mu.Lock()
	e.budget, e.budgetAt, e.budgetSet = b, time.Now(), true
	e.mu.Unlock()
	return b
}

func classify(err error) string {
	switch {
	case errors.Is(err, systemone.ErrUnauthorized):
		return ErrClassUnauthorized
	case errors.Is(err, systemone.ErrModelNotFound):
		return ErrClassModelNotFound
	case errors.Is(err, systemone.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return ErrClassTimeout
	case errors.Is(err, systemone.ErrTooLarge):
		return ErrClassTooLarge
	case errors.Is(err, systemone.ErrBadRequest):
		return ErrClassBadRequest
	case errors.Is(err, systemone.ErrMalformedResponse):
		return ErrClassMalformed
	case errors.Is(err, systemone.ErrUnreachable), errors.Is(err, context.Canceled):
		return ErrClassUnreachable
	case errors.Is(err, systemone.ErrServer), errors.Is(err, systemone.ErrRateLimited):
		return ErrClassServer
	}
	return ErrClassServer
}
