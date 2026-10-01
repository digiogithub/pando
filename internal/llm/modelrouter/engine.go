// Package modelrouter implements the pure decision engine of model auto mode:
// it turns (config, prompt, short history) into a routing Decision using a
// System One decision provider. It holds no per-session state.
package modelrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
//
// The engine is keyed only on the shared decision model (provider, model,
// timeout); the per-consumer policy (threshold, routes, history) is passed to
// each call.
type Engine struct {
	dec      config.DecisionModelConfig
	provider systemone.DecisionProvider

	mu        sync.Mutex
	budget    int
	budgetAt  time.Time
	budgetSet bool
}

// NewEngine builds an engine from the shared decision model (provider, model
// and timeout).
func NewEngine(dec config.DecisionModelConfig, opts ...EngineOption) (*Engine, error) {
	e := &Engine{dec: dec}
	for _, o := range opts {
		o(e)
	}
	if e.provider == nil {
		p, err := systemone.NewProvider(systemone.ProviderKind(dec.Router.EffectiveProvider()), systemone.Options{
			BaseURL:   dec.Router.EffectiveBaseURL(),
			APIKey:    dec.Router.EffectiveAPIKey(),
			Headers:   dec.Router.Headers,
			Timeout:   dec.EffectiveTimeout(),
			KeepAlive: dec.Router.KeepAlive,
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

// ForConfig returns the process-wide engine for dec, rebuilding it when the
// decision model content changes (hot reload).
func ForConfig(dec config.DecisionModelConfig) (*Engine, error) {
	key := configKey(dec)
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheEng != nil && cacheKey == key {
		return cacheEng, nil
	}
	e, err := NewEngine(dec)
	if err != nil {
		return nil, err
	}
	cacheKey, cacheEng = key, e
	return e, nil
}

func configKey(dec config.DecisionModelConfig) string {
	b, _ := json.Marshal(struct {
		D       config.DecisionModelConfig
		Key     string
		URL     string
		Timeout time.Duration
	}{dec, dec.Router.EffectiveAPIKey(), dec.Router.EffectiveBaseURL(), dec.EffectiveTimeout()})
	return string(b)
}

// Route classifies in.Prompt against the routes and thresholds of policy. It
// never fails: on any router failure it returns the coder model with Reason
// "router_error".
func (e *Engine) Route(ctx context.Context, policy config.ModelAutoModeConfig, in Input) Decision {
	routerModel := e.dec.Router.Model
	d := Decision{
		RouterProvider: string(e.dec.Router.EffectiveProvider()),
		RouterModel:    routerModel,
		Candidates:     coderOnly(in.CoderModel),
	}
	routes := policy.EnabledRoutes()
	if len(routes) == 0 {
		d.Reason = ReasonNoRoutes
		return d
	}

	criteria := make([]systemone.Criterion, 0, len(routes)+1)
	overhead := questionOverhead(instructionsText)
	for _, r := range routes {
		criteria = append(criteria, systemone.NewCriterion(r.ID, r.Description))
		overhead += EstimateTokens(r.ID) + EstimateTokens(r.Description) + 4
	}
	criteria = append(criteria, systemone.NewCriterion(noneKey, noneDescription))

	q := systemone.Question{Type: "choice", Instructions: instructionsText, Criteria: criteria}
	resp, latency, err := e.ask(ctx, in, policy.HistoryPrompts, map[string]systemone.Question{questionName: q}, overhead)
	d.LatencyMs = latency
	var ans systemone.Answer
	if err == nil {
		ans, err = answerFor(resp, questionName, q)
	}
	if err != nil {
		d.Reason = ReasonRouterError
		d.Err = err
		d.ErrClass = classify(err)
		return d
	}
	applyTaskAnswer(&d, policy, routes, resp, ans)
	return d
}

// applyTaskAnswer fills d from the answer to the task question.
func applyTaskAnswer(d *Decision, policy config.ModelAutoModeConfig, routes []config.ModelAutoRoute, resp *systemone.Response, ans systemone.Answer) {
	d.InputTokens = resp.Usage.InputTokens
	d.CostUSD = resp.Usage.Cost
	d.Probabilities = ans.Probabilities
	d.Confidence = ans.Confidence
	d.Probability = ans.Probabilities[ans.Choice]

	if ans.Choice == noneKey {
		d.Reason = ReasonNoMatch
		return
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
	case d.Probability < policy.EffectiveThreshold():
		d.Reason = ReasonLowProbability
	case policy.MinConfidence > 0 && d.Confidence < policy.MinConfidence:
		d.Reason = ReasonLowConfidence
	default:
		d.Reason = ReasonMatched
		d.Matched = true
		d.RouteID = route.ID
		d.Candidates = routeCandidates(*route)
	}
}

// questionOverhead estimates the tokens a choice question costs besides its
// per-criterion text: instructions, the "none" criterion and request framing.
func questionOverhead(instructions string) int {
	return EstimateTokens(instructions) + EstimateTokens(noneDescription) + 32
}

// ask sends one System One request carrying qs. The shared state is built from
// in with a token budget that accounts for overhead (the estimated tokens of
// all questions and criteria). The answers are NOT validated: callers judge
// each one with answerFor. A non-nil error is a transport/protocol failure
// that affects every question.
func (e *Engine) ask(ctx context.Context, in Input, historyPrompts int, qs map[string]systemone.Question, overhead int) (*systemone.Response, int64, error) {
	budget := e.ContextBudget(ctx) - overhead
	stateJSON := BuildState(in, historyPrompts, budget)
	resp, latency, err := e.Ask(ctx, stateJSON, qs)
	return resp, latency.Milliseconds(), err
}

// Ask is the generic entry point for any consumer: it sends one System One
// request with the given JSON state (a JSON object string) and questions to the
// configured decision model. Answers are NOT validated (the lenient decode is
// used) so the caller judges each one. The caller is responsible for keeping
// state within ContextBudget (BuildState does that for the routing state).
// More than systemone.MaxQuestions questions, or a state/request over
// systemone.MaxBodyBytes, is rejected before anything is sent, with an error
// that ClassifyError maps to "bad_request" / "too_large". A non-nil error is a
// transport/protocol failure that affects every question.
func (e *Engine) Ask(ctx context.Context, state string, qs map[string]systemone.Question) (*systemone.Response, time.Duration, error) {
	if len(qs) > systemone.MaxQuestions {
		return nil, 0, fmt.Errorf("%w: %d questions, limit is %d", systemone.ErrBadRequest, len(qs), systemone.MaxQuestions)
	}
	if len(state) > systemone.MaxBodyBytes {
		return nil, 0, fmt.Errorf("%w: state is %d bytes, limit is %d", systemone.ErrTooLarge, len(state), systemone.MaxBodyBytes)
	}
	if state == "" {
		state = "{}"
	}
	req := systemone.Request{
		Model:     e.dec.Router.Model,
		State:     json.RawMessage(state),
		Questions: qs,
	}
	if e.provider.Kind() == systemone.KindOllama {
		req.KeepAlive = e.provider.Client().KeepAlive()
	}

	callCtx, cancel := context.WithTimeout(ctx, e.dec.EffectiveTimeout())
	defer cancel()
	start := time.Now()
	resp, err := e.provider.Client().DecideLenient(callCtx, req)
	latency := time.Since(start)
	if err == nil && resp == nil {
		err = systemone.ErrMalformedResponse
	}
	return resp, latency, err
}

// answerFor returns the answer to the named choice question or
// ErrMalformedResponse when it is missing or its choice is not a criterion.
func answerFor(resp *systemone.Response, name string, q systemone.Question) (systemone.Answer, error) {
	ans, ok := resp.Answers[name]
	if !ok || ans.Choice == "" {
		return ans, systemone.ErrMalformedResponse
	}
	for _, c := range q.Criteria {
		if c.Key == ans.Choice {
			return ans, nil
		}
	}
	return ans, systemone.ErrMalformedResponse
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

// ContextBudget returns the context window, in tokens, of the configured
// decision model (cached for a few minutes, with a conservative default when
// the provider cannot tell). Callers subtract their question overhead and keep
// SafetyMargin free.
func (e *Engine) ContextBudget(ctx context.Context) int {
	model := e.dec.Router.Model
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

// ClassifyError maps a decision-model failure to one of the ErrClass* values.
func ClassifyError(err error) string { return classify(err) }

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
