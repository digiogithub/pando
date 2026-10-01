package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extevents"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
)

// Model auto mode (PANDO-EP-0015): at the start of every user turn of an Auto
// session the agent asks a decision model which configured route the prompt
// belongs to, and runs the turn on that route's model. The chosen model is a
// turn-scoped session override; agents.coder.model is never touched.

// ReasonRouteUnusable is the routing reason used when the decision matched a
// route but none of its models can serve the turn (unknown, disabled, cooling
// down, no attachment support, context too small). The turn runs on the coder.
const ReasonRouteUnusable = "route_unusable"

// routerGraceOnTimeout is added to the configured router timeout for the outer
// deadline so the engine's own (typed) timeout error wins the race.
const routerGraceOnTimeout = 50 * time.Millisecond

// RoutingInfo summarises the last Auto routing decision of a session. It is
// also the payload of the routing notice (AgentEvent.Routing).
type RoutingInfo struct {
	// RouteID is the matched route, empty when nothing matched.
	RouteID string `json:"routeId,omitempty"`
	// Model is the model that is (or last was) answering the turn. It changes
	// when the turn fails over.
	Model models.ModelID `json:"model"`
	// Matched reports that a route matched with enough probability.
	Matched     bool    `json:"matched"`
	Probability float64 `json:"probability"`
	Confidence  float64 `json:"confidence"`
	// Reason: matched, no_match, low_probability, low_confidence, router_error,
	// no_routes, route_unusable, failover.
	Reason string `json:"reason"`
	// FallbackUsed is true when Model is a failover candidate rather than the
	// first choice.
	FallbackUsed bool `json:"fallbackUsed"`
	// ErrorClass is the typed router error (unreachable, unauthorized, ...)
	// when the router was unavailable.
	ErrorClass      string           `json:"errorClass,omitempty"`
	RouterProvider  string           `json:"routerProvider,omitempty"`
	RouterModel     string           `json:"routerModel,omitempty"`
	RouterLatencyMs int64            `json:"routerLatencyMs"`
	RouterCostUSD   *float64         `json:"routerCostUsd,omitempty"`
	Candidates      []models.ModelID `json:"candidates,omitempty"`
	// Kind classifies the notice: "routed", "no_match", "router_unavailable",
	// "route_unusable" or "failover".
	Kind string `json:"kind"`
	// Notice is the human readable line (same text as the system message).
	Notice string `json:"notice"`
}

// Routing notice kinds (RoutingInfo.Kind).
const (
	RoutingKindRouted            = "routed"
	RoutingKindNoMatch           = "no_match"
	RoutingKindRouterUnavailable = "router_unavailable"
	RoutingKindRouteUnusable     = "route_unusable"
	RoutingKindFailover          = "failover"
)

// autoTurnState is the per-turn state of an Auto run, alive between
// beginAutoTurn and endAutoTurn.
type autoTurnState struct {
	mu sync.Mutex
	// cands is the usable candidate chain, cands[idx] is the current model.
	cands []models.ModelID
	idx   int
	info  RoutingInfo
	// prevOverride is the session model override before the turn, restored at
	// the end of it.
	prevOverride models.ModelID
	// modelChanged reports that the routed model differs from the model that
	// answered the previous turn (history hygiene needed).
	modelChanged bool
	// attempts records failed candidates for the final error summary.
	attempts []failoverAttempt
	// compacted is set once a context-length error triggered a compaction.
	compacted bool
	// quietSwitch suppresses the generic "Model switched" message while a
	// failover switch is applied (the Auto notice replaces it).
	quietSwitch bool
}

// autoTurns maps sessionID -> *autoTurnState for runs in flight.
var autoTurns sync.Map

// lastRoutings maps sessionID -> RoutingInfo of the most recent Auto turn.
var lastRoutings sync.Map

func autoTurnFor(sessionID string) *autoTurnState {
	if sessionID == "" {
		return nil
	}
	v, ok := autoTurns.Load(sessionID)
	if !ok {
		return nil
	}
	t, _ := v.(*autoTurnState)
	return t
}

type systemInitiatedRunKey struct{}

// withSystemInitiatedRun marks ctx as belonging to a run the system started
// (delegation resurrection), which never routes.
func withSystemInitiatedRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemInitiatedRunKey{}, true)
}

func isSystemInitiatedRun(ctx context.Context) bool {
	v, _ := ctx.Value(systemInitiatedRunKey{}).(bool)
	return v
}

// SetSessionAutoMode sets the explicit per-session Auto flag. Turning it on
// also drops any manual model override so the next turn routes; turning it off
// keeps the session on the configured coder model (or whatever model the caller
// selects next).
func SetSessionAutoMode(sessionID string, auto bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	sessionLLMOverridesMu.Lock()
	defer sessionLLMOverridesMu.Unlock()

	current := SessionLLMOverridesFor(sessionID)
	v := auto
	current.AutoMode = &v
	if auto {
		current.Model = ""
	}
	storeSessionLLMOverrides(sessionID, current)
}

// SessionAutoMode reports whether the session runs in Auto mode: the explicit
// per-session flag when set, otherwise the global selection. It is always false
// when modelAutoMode.enabled is off.
func SessionAutoMode(sessionID string) bool {
	cfg := config.Get()
	if cfg == nil || !cfg.ModelAutoMode.Enabled {
		return false
	}
	if explicit := SessionLLMOverridesFor(sessionID).AutoMode; explicit != nil {
		return *explicit
	}
	return cfg.ModelAutoMode.AutoSelected()
}

// LastRoutedModel returns the model the last Auto turn of the session ran on.
func LastRoutedModel(sessionID string) (models.ModelID, bool) {
	info, ok := LastRouting(sessionID)
	if !ok || info.Model == "" {
		return "", false
	}
	return info.Model, true
}

// LastRouting returns the summary of the last Auto routing decision.
func LastRouting(sessionID string) (RoutingInfo, bool) {
	v, ok := lastRoutings.Load(strings.TrimSpace(sessionID))
	if !ok {
		return RoutingInfo{}, false
	}
	info, _ := v.(RoutingInfo)
	return info, true
}

// SetSessionAutoMode, SessionAutoMode, LastRoutedModel and LastRouting are also
// exposed as methods on the agent so surfaces holding a Service can reach them
// through the AutoModeController interface.
func (a *agent) SetSessionAutoMode(sessionID string, auto bool) { SetSessionAutoMode(sessionID, auto) }
func (a *agent) SessionAutoMode(sessionID string) bool          { return SessionAutoMode(sessionID) }
func (a *agent) LastRoutedModel(sessionID string) (models.ModelID, bool) {
	return LastRoutedModel(sessionID)
}
func (a *agent) LastRouting(sessionID string) (RoutingInfo, bool) { return LastRouting(sessionID) }

// AutoModeController is implemented by the coder agent. It is kept out of
// Service so test doubles of Service do not have to implement it; callers
// type-assert (or use the package-level functions, which are session keyed).
type AutoModeController interface {
	SetSessionAutoMode(sessionID string, auto bool)
	SessionAutoMode(sessionID string) bool
	LastRoutedModel(sessionID string) (models.ModelID, bool)
	LastRouting(sessionID string) (RoutingInfo, bool)
}

var _ AutoModeController = (*agent)(nil)

// setAutoTurnModelOverride applies a turn-scoped model override without
// touching the Auto flag (SetSessionModelOverride would switch Auto off).
func setAutoTurnModelOverride(sessionID string, model models.ModelID) {
	sessionLLMOverridesMu.Lock()
	defer sessionLLMOverridesMu.Unlock()
	current := SessionLLMOverridesFor(sessionID)
	current.Model = model
	storeSessionLLMOverrides(sessionID, current)
}

// autoTurnEligible reports whether this run starts an Auto-routed user turn.
// Only the coder agent routes; subagents, title, summarizer, persona selector
// and evaluator use their own agent names and never do. Resurrection runs are
// system initiated and keep whatever model the session already has.
func (a *agent) autoTurnEligible(ctx context.Context, sessionID string) bool {
	return a.agentName == config.AgentCoder &&
		sessionID != "" &&
		!isCleanModeContext(ctx) &&
		!isSystemInitiatedRun(ctx) &&
		SessionAutoMode(sessionID)
}

// routerErrorClass maps the engine's error class to the wording used in
// notices ("model not found", "unauthorized", ...).
func routerErrorWording(class string) string {
	class = strings.TrimSpace(class)
	if class == "" {
		return "error"
	}
	return strings.ReplaceAll(class, "_", " ")
}

// autoRouteInput builds the decision input of a turn: the prompt, the recent
// user prompts, the attachment names and the coder model. hasAttachments tells
// whether the turn carries attachments the chosen model must be able to read.
func autoRouteInput(
	auto config.ModelAutoModeConfig,
	userPrompt string,
	priorMsgs []message.Message,
	attachmentParts []message.ContentPart,
) (in modelrouter.Input, hasAttachments bool) {
	coder, _ := configuredAgentModel()

	var attachmentNames []string
	for _, part := range attachmentParts {
		switch p := part.(type) {
		case message.BinaryContent:
			hasAttachments = true
			name := filepath.Base(p.Path)
			if p.Path == "" {
				name = "attachment"
			}
			attachmentNames = append(attachmentNames, name)
		case message.ImageURLContent:
			hasAttachments = true
			attachmentNames = append(attachmentNames, "image")
		}
	}

	return modelrouter.Input{
		Prompt:          userPrompt,
		History:         recentUserPrompts(priorMsgs, auto.HistoryPrompts),
		AttachmentNames: attachmentNames,
		HasAttachments:  hasAttachments,
		CoderModel:      coder,
	}, hasAttachments
}

// beginAutoTurn routes the prompt, builds the candidate chain, applies the
// first candidate as the turn's session model override and announces the
// decision. The returned parts are the attachments to send (dropped when the
// chosen model cannot read them). It never fails: any problem degrades to the
// coder model. pre, when non-nil, is a decision already taken for this turn
// (the combined persona + model request); the router is then not called again.
func (a *agent) beginAutoTurn(
	ctx context.Context,
	sessionID, userPrompt string,
	priorMsgs []message.Message,
	attachmentParts []message.ContentPart,
	eventCh chan<- AgentEvent,
	pre *modelrouter.Decision,
) (*autoTurnState, []message.ContentPart) {
	cfg := config.Get()
	if cfg == nil {
		return nil, attachmentParts
	}
	auto := cfg.ModelAutoMode
	coder, _ := configuredAgentModel()

	in, hasAttachments := autoRouteInput(auto, userPrompt, priorMsgs, attachmentParts)

	var dec modelrouter.Decision
	if pre != nil {
		dec = *pre
	} else if engine, err := modelrouter.ForConfig(auto); err != nil {
		dec = modelrouter.Decision{
			Reason:         modelrouter.ReasonRouterError,
			Err:            err,
			ErrClass:       "config",
			Candidates:     []models.ModelID{coder},
			RouterProvider: string(auto.Router.EffectiveProvider()),
			RouterModel:    auto.Router.Model,
		}
	} else {
		routeCtx, cancel := context.WithTimeout(ctx, auto.EffectiveTimeout()+routerGraceOnTimeout)
		dec = engine.Route(routeCtx, in)
		cancel()
	}
	if len(dec.Candidates) == 0 && coder != "" {
		dec.Candidates = []models.ModelID{coder}
	}

	// Candidate chain: drop models cooling down after an auth/not-found
	// failover, then capability and context-window filtering.
	cands := dec.Candidates
	if dec.Matched {
		cands = dropCoolingDown(cands)
	}
	historyTokens := int(estimateMessagesTokens(priorMsgs)) + modelrouter.EstimateTokens(userPrompt)
	usable, skipped := modelrouter.FilterCandidates(cands, modelrouter.DefaultLookup(cfg), hasAttachments, historyTokens)
	reason := dec.Reason
	unusable := false
	if len(usable) == 0 {
		if dec.Matched {
			unusable = true
			reason = ReasonRouteUnusable
			logging.Debug("model_auto: route has no usable model", "route", dec.RouteID, "skipped", skipped)
		}
		if coder == "" {
			logging.Warn("model_auto: no coder model to fall back to")
			return nil, attachmentParts
		}
		usable = []models.ModelID{coder}
	}

	chosen := usable[0]
	info := RoutingInfo{
		RouteID:         dec.RouteID,
		Model:           chosen,
		Matched:         dec.Matched && !unusable,
		Probability:     dec.Probability,
		Confidence:      dec.Confidence,
		Reason:          reason,
		ErrorClass:      dec.ErrClass,
		RouterProvider:  dec.RouterProvider,
		RouterModel:     dec.RouterModel,
		RouterLatencyMs: dec.LatencyMs,
		RouterCostUSD:   dec.CostUSD,
		Candidates:      append([]models.ModelID(nil), usable...),
	}

	turn := &autoTurnState{
		cands:        usable,
		info:         info,
		prevOverride: SessionLLMOverridesFor(sessionID).Model,
		modelChanged: lastAssistantModel(priorMsgs) != "" && lastAssistantModel(priorMsgs) != chosen,
	}
	autoTurns.Store(sessionID, turn)
	setAutoTurnModelOverride(sessionID, chosen)

	// The chosen model may not read attachments (only reachable when the coder
	// itself is the last resort): send the turn without them.
	if m, ok := models.SupportedModels()[chosen]; ok && hasAttachments && !m.SupportsAttachments {
		logging.Debug("model_auto: dropping attachments the chosen model cannot read", "model", chosen)
		attachmentParts = nil
	}

	a.announceRouting(sessionID, eventCh, turn, dec, skipped)
	return turn, attachmentParts
}

// announceRouting records the decision, publishes the telemetry event, writes
// the Info log and emits the routing notice.
func (a *agent) announceRouting(sessionID string, eventCh chan<- AgentEvent, turn *autoTurnState, dec modelrouter.Decision, skipped map[models.ModelID]string) {
	info := turn.info
	var notice string
	switch {
	case dec.Reason == modelrouter.ReasonRouterError:
		info.Kind = RoutingKindRouterUnavailable
		notice = fmt.Sprintf("Auto: router unavailable (%s) → %s", routerErrorWording(dec.ErrClass), info.Model)
	case info.Reason == ReasonRouteUnusable:
		info.Kind = RoutingKindRouteUnusable
		notice = fmt.Sprintf("Auto: route %s has no usable model → %s", dec.RouteID, info.Model)
	case info.Matched:
		info.Kind = RoutingKindRouted
		notice = fmt.Sprintf("Auto: %s → %s (p=%.2f, %d ms via %s/%s)",
			info.RouteID, info.Model, info.Probability, info.RouterLatencyMs, info.RouterProvider, info.RouterModel)
	default:
		info.Kind = RoutingKindNoMatch
		if best, p, ok := bestRoute(dec.Probabilities); ok {
			notice = fmt.Sprintf("Auto: no confident match (best %s p=%.2f) → %s", best, p, info.Model)
		} else {
			notice = fmt.Sprintf("Auto: no confident match → %s", info.Model)
		}
	}
	info.Notice = notice
	turn.mu.Lock()
	turn.info = info
	turn.mu.Unlock()
	lastRoutings.Store(sessionID, info)

	// One Info line per decision. It carries ids, numbers and model names only:
	// Info records can be shipped by opt-in telemetry, so never the prompt.
	logging.Info("model_auto: decision",
		"session_id", sessionID,
		"route", info.RouteID,
		"model", string(info.Model),
		"reason", info.Reason,
		"probability", info.Probability,
		"confidence", info.Confidence,
		"router_provider", info.RouterProvider,
		"router_model", info.RouterModel,
		"router_latency_ms", info.RouterLatencyMs,
		"error_class", info.ErrorClass,
	)
	extevents.ModelRouted(extevents.ModelRoutedInfo{
		SessionID:       sessionID,
		RouteID:         info.RouteID,
		Model:           string(info.Model),
		Reason:          info.Reason,
		Probability:     info.Probability,
		Confidence:      info.Confidence,
		RouterProvider:  info.RouterProvider,
		RouterModel:     info.RouterModel,
		RouterLatencyMs: info.RouterLatencyMs,
		RouterCostUSD:   info.RouterCostUSD,
	})

	if info.Kind == RoutingKindRouterUnavailable {
		// Warn once per session and error class; afterwards Debug only. The
		// first notice IS the warning.
		if !warnAutoOnce(sessionID, "router:"+info.ErrorClass) {
			logging.Debug("model_auto: router unavailable (already warned)",
				"session_id", sessionID, "error_class", info.ErrorClass, "error", dec.Err)
			return
		}
		logging.Warn("model_auto: router unavailable", "session_id", sessionID,
			"error_class", info.ErrorClass, "error", dec.Err)
	}
	a.emitRoutingNotice(sessionID, eventCh, info)
}

// emitRoutingNotice publishes the notice as a system message carrying the
// structured RoutingInfo in AgentEvent.Routing, and records it as a run status
// message so surfaces that read the run summary see it too.
func (a *agent) emitRoutingNotice(sessionID string, eventCh chan<- AgentEvent, info RoutingInfo) {
	a.addRunStatusMessage(sessionID, info.Notice)
	routing := info
	ev := AgentEvent{
		Type:          AgentEventTypeSystemMessage,
		SessionID:     sessionID,
		SystemMessage: info.Notice + "\n",
		Routing:       &routing,
	}
	a.publishEvent(ev)
	if eventCh != nil {
		select {
		case eventCh <- ev:
		default:
		}
	}
}

// endAutoTurn closes the turn: the session goes back to "no override" (or the
// override it had before) unless somebody else changed it during the turn.
func (a *agent) endAutoTurn(sessionID string, turn *autoTurnState) {
	if turn == nil {
		return
	}
	autoTurns.CompareAndDelete(sessionID, turn)
	turn.mu.Lock()
	applied := turn.cands[turn.idx]
	prev := turn.prevOverride
	turn.mu.Unlock()
	if SessionLLMOverridesFor(sessionID).Model == applied {
		setAutoTurnModelOverride(sessionID, prev)
	}
}

// autoQuietSwitch reports whether the model switch being applied is an Auto
// failover whose announcement is the Auto notice.
func autoQuietSwitch(sessionID string) bool {
	turn := autoTurnFor(sessionID)
	if turn == nil {
		return false
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	return turn.quietSwitch
}

// recentUserPrompts returns up to n previous user prompts, newest last.
func recentUserPrompts(msgs []message.Message, n int) []string {
	if n <= 0 {
		return nil
	}
	var out []string
	for i := len(msgs) - 1; i >= 0 && len(out) < n; i-- {
		if msgs[i].Role != message.User {
			continue
		}
		if text := strings.TrimSpace(msgs[i].Content().Text); text != "" {
			out = append(out, text)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// lastAssistantModel is the model that answered the most recent assistant turn.
func lastAssistantModel(msgs []message.Message) models.ModelID {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == message.Assistant && msgs[i].Model != "" {
			return msgs[i].Model
		}
	}
	return ""
}

// bestRoute returns the most probable real route (not "none") of an answer.
func bestRoute(probs map[string]float64) (string, float64, bool) {
	keys := make([]string, 0, len(probs))
	for k := range probs {
		if k != config.ModelAutoModeReservedRouteID {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return "", 0, false
	}
	sort.Strings(keys)
	best := keys[0]
	for _, k := range keys[1:] {
		if probs[k] > probs[best] {
			best = k
		}
	}
	return best, probs[best], true
}

// --- warn-once bookkeeping -------------------------------------------------

var (
	autoWarnMu sync.Mutex
	autoWarned = map[string]struct{}{}
)

// autoWarnedMax bounds the set so a long-lived process cannot grow it forever.
const autoWarnedMax = 4096

// warnAutoOnce reports true the first time (sessionID, class) is seen.
func warnAutoOnce(sessionID, class string) bool {
	autoWarnMu.Lock()
	defer autoWarnMu.Unlock()
	key := sessionID + "\x00" + class
	if _, seen := autoWarned[key]; seen {
		return false
	}
	if len(autoWarned) >= autoWarnedMax {
		autoWarned = map[string]struct{}{}
	}
	autoWarned[key] = struct{}{}
	return true
}

// resetAutoWarnings forgets every warning (tests).
func resetAutoWarnings() {
	autoWarnMu.Lock()
	defer autoWarnMu.Unlock()
	autoWarned = map[string]struct{}{}
}

// --- provider retry budget -------------------------------------------------

// autoCandidateRetries is the retry budget of every Auto candidate except the
// last one, so a failing provider hands over to the next candidate quickly
// instead of backing off through the default budget.
var autoCandidateRetries = 2

// autoRetryBudget returns the reduced retry budget for the session's current
// Auto candidate, or 0 (default budget) when the session is not in an Auto turn
// or is on its last candidate.
func autoRetryBudget(sessionID string) int {
	turn := autoTurnFor(sessionID)
	if turn == nil {
		return 0
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if turn.idx < len(turn.cands)-1 {
		return autoCandidateRetries
	}
	return 0
}

// providerBuilderHook lets tests substitute the provider built for a model. It
// is nil in production.
var providerBuilderHook func(model models.Model, retries int) (provider.Provider, bool)
