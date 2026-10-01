package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extevents"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/persona"
	"github.com/digiogithub/pando/internal/message"
)

// Persona auto-select with a decision model (PANDO-EP-0017). When the
// persona-selector agent has useDecisionModel on, the persona of a user prompt
// is chosen by the model auto mode decision router (one /v1/systemone choice
// question). The agent's own LLM is only the fallback for a router that cannot
// answer. A router that answers "none" or with a low probability keeps the
// persona of the session's previous turn (sticky) so the system prompt, and the
// provider prompt cache, do not flip on short follow-ups.

// Persona routing sources (PersonaRoutingInfo.Source).
const (
	PersonaSourceDecision = "decision"
	PersonaSourceLLM      = "llm"
	PersonaSourceSticky   = "sticky"
	PersonaSourceDefault  = "default"
)

// personaDecisionCooldown is how long a failed decision router is skipped (the
// turn goes straight to the LLM fallback) so an unreachable router does not add
// its timeout to every prompt. It is measured with autoClock.
const personaDecisionCooldown = 60 * time.Second

// personaLLMFallbackTimeout bounds the LLM fallback so a slow model never holds
// the prompt back for long.
const personaLLMFallbackTimeout = 20 * time.Second

// personaStateMax bounds the per-session and cooldown maps in a long-lived
// process.
const personaStateMax = 4096

// PersonaRoutingInfo summarises the last auto-selection of a session. It never
// carries the prompt text.
type PersonaRoutingInfo struct {
	// Persona is the applied persona, empty when none.
	Persona string
	// Source: "decision" (router matched), "llm" (fallback model chose),
	// "sticky" (previous persona kept) or "default" (assistant / none).
	Source string
	// Reason is the decision outcome: matched, no_match, low_probability,
	// no_personas, router_error or no_router.
	Reason      string
	Probability float64
	LatencyMs   int64
	CostUSD     *float64
	// ErrClass is the typed router error when the decision model was unavailable.
	ErrClass string
	// Changed reports that Persona differs from the previous turn's persona.
	Changed bool
}

// personaRequest carries what the persona step needs for one run.
type personaRequest struct {
	// Prompt is the text the LLM fallback classifies.
	Prompt string
	// Query is the text the decision model classifies; Prompt when empty.
	Query string
	// History is the recent user prompts (newest last) added to the state.
	History []string
	// Eligible marks a user turn of the coder agent. Other runs (system
	// initiated, subagents) reuse the session's remembered persona.
	Eligible bool
	// ParentSessionID is the parent of a delegated child session. A non-eligible
	// run without a persona of its own inherits the parent's remembered one.
	ParentSessionID string
	// Combined, when set, asks the engine to take the model auto mode decision
	// in the same request as the persona decision.
	Combined *combinedRoute
}

// combinedRoute carries the model auto mode input in and its decision out.
type combinedRoute struct {
	In   modelrouter.Input
	Dec  modelrouter.Decision
	Done bool
}

// personaOutcome is the result of the persona step.
type personaOutcome struct {
	Content string
	Name    string
	// Info is set when the decision model option drove the selection.
	Info *PersonaRoutingInfo
	// Warning is the one-per-session-and-class degradation notice, if due.
	Warning string
}

// --- per-session state ------------------------------------------------------

type personaSession struct {
	applied string
	last    PersonaRoutingInfo
	hasLast bool
	// used is the personaUseSeq value of the last read or write; the smallest
	// one is evicted first when the map is full.
	used uint64
}

type personaCooldown struct {
	until time.Time
	class string
}

var (
	personaStateMu    sync.Mutex
	personaSessions   = map[string]*personaSession{}
	personaCooldowns  = map[string]personaCooldown{}
	personaRefresherF func()
	personaUseSeq     uint64
)

// SetPersonaManagerRefresher installs a hook the agent calls before every
// auto-selection so the app can reload the persona manager when the persona
// path changed. It must be cheap when nothing changed.
func SetPersonaManagerRefresher(fn func()) {
	personaMu.Lock()
	defer personaMu.Unlock()
	personaRefresherF = fn
}

func refreshPersonaManager() {
	personaMu.RLock()
	fn := personaRefresherF
	personaMu.RUnlock()
	if fn != nil {
		fn()
	}
}

// LastPersonaRouting returns the last auto-selection of the session.
func LastPersonaRouting(sessionID string) (PersonaRoutingInfo, bool) {
	sessionID = strings.TrimSpace(sessionID)
	personaStateMu.Lock()
	defer personaStateMu.Unlock()
	s, ok := personaSessions[sessionID]
	if !ok || !s.hasLast {
		return PersonaRoutingInfo{}, false
	}
	touchPersonaSession(s)
	return s.last, true
}

// AppliedAutoPersona returns the persona the auto-selection applied to the
// session on its last user turn and where the choice came from (a
// PersonaSource* value; "llm" when the decision model option is off). The name
// is empty when nothing was applied yet.
func AppliedAutoPersona(sessionID string) (name, source string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", ""
	}
	if info, ok := LastPersonaRouting(sessionID); ok {
		return info.Persona, info.Source
	}
	if n := rememberedPersona(sessionID); n != "" {
		return n, PersonaSourceLLM
	}
	return "", ""
}

// touchPersonaSession marks s as the most recently used. The caller holds
// personaStateMu.
func touchPersonaSession(s *personaSession) {
	personaUseSeq++
	s.used = personaUseSeq
}

// ForgetSessionPersona drops the remembered persona and the persona warning
// state of a session.
func ForgetSessionPersona(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	personaStateMu.Lock()
	delete(personaSessions, sessionID)
	personaStateMu.Unlock()

	autoWarnMu.Lock()
	defer autoWarnMu.Unlock()
	prefix := sessionID + "\x00persona:"
	for k := range autoWarned {
		if strings.HasPrefix(k, prefix) {
			delete(autoWarned, k)
		}
	}
}

func rememberedPersona(sessionID string) string {
	personaStateMu.Lock()
	defer personaStateMu.Unlock()
	if s, ok := personaSessions[sessionID]; ok {
		touchPersonaSession(s)
		return s.applied
	}
	return ""
}

// recordPersona stores the applied persona (and optionally the routing info)
// of the session. Callers pass sessionID "" for contexts without a session.
func recordPersona(sessionID, name string, info *PersonaRoutingInfo) {
	if sessionID == "" {
		return
	}
	personaStateMu.Lock()
	defer personaStateMu.Unlock()
	s, ok := personaSessions[sessionID]
	if !ok {
		if len(personaSessions) >= personaStateMax {
			// Evict the least recently used session; eviction is rare so a
			// scan is fine.
			oldest, oldestUsed := "", uint64(0)
			for k, v := range personaSessions {
				if oldest == "" || v.used < oldestUsed {
					oldest, oldestUsed = k, v.used
				}
			}
			delete(personaSessions, oldest)
		}
		s = &personaSession{}
		personaSessions[sessionID] = s
	}
	touchPersonaSession(s)
	s.applied = name
	if info != nil {
		s.last, s.hasLast = *info, true
	}
}

func personaCooldownFor(key string) (personaCooldown, bool) {
	personaStateMu.Lock()
	defer personaStateMu.Unlock()
	c, ok := personaCooldowns[key]
	if !ok {
		return personaCooldown{}, false
	}
	if !autoClock().Before(c.until) {
		delete(personaCooldowns, key)
		return personaCooldown{}, false
	}
	return c, true
}

func startPersonaCooldown(key, class string) {
	personaStateMu.Lock()
	defer personaStateMu.Unlock()
	if len(personaCooldowns) >= personaStateMax {
		personaCooldowns = map[string]personaCooldown{}
	}
	personaCooldowns[key] = personaCooldown{until: autoClock().Add(personaDecisionCooldown), class: class}
}

// resetPersonaState forgets sessions and cooldowns (tests).
func resetPersonaState() {
	personaStateMu.Lock()
	defer personaStateMu.Unlock()
	personaSessions = map[string]*personaSession{}
	personaCooldowns = map[string]personaCooldown{}
}

// personaRouterKey identifies the router a cooldown applies to. It never
// includes the API key.
func personaRouterKey(dec config.DecisionModelConfig) string {
	return string(dec.Router.EffectiveProvider()) + "|" + dec.Router.EffectiveBaseURL() + "|" + dec.Router.Model
}

// --- selection --------------------------------------------------------------

// autoSelectActive reports whether persona auto-selection is on. It is read
// live from the configuration. A selector installed explicitly (not lazy) keeps
// its historical meaning of "on".
func autoSelectActive(sel *PersonaSelector) bool {
	if cfg := config.Get(); cfg != nil && cfg.PersonaAutoSelect.Enabled {
		return true
	}
	return sel != nil && !sel.lazy
}

func personaContentOf(mgr *persona.Manager, name string) string {
	if mgr == nil || name == "" {
		return ""
	}
	return mgr.GetPersona(name)
}

// autoSelectPersona is the auto-selection step of the persona priority.
func autoSelectPersona(ctx context.Context, req personaRequest) personaOutcome {
	sel := personaSelector()
	if !autoSelectActive(sel) {
		return personaOutcome{}
	}
	refreshPersonaManager()
	sid := sessionIDFromContext(ctx)

	// Option off: the original behaviour, one LLM call for every run.
	if !config.PersonaSelectorUsesDecisionModel() {
		return llmOnlySelection(ctx, sel, sid, req)
	}

	// Runs that are not a user turn of the coder reuse the remembered persona:
	// exactly one decision per user prompt, none per tool iteration. A delegated
	// child session without a persona of its own inherits its parent's (read
	// only: the parent's state is never written).
	if !req.Eligible {
		name := rememberedPersona(sid)
		if name == "" && req.ParentSessionID != "" && req.ParentSessionID != sid {
			name = rememberedPersona(req.ParentSessionID)
		}
		mgr := personaManager()
		if mgr != nil && !mgr.HasPersona(name) {
			name = ""
		}
		return personaOutcome{Name: name, Content: personaContentOf(mgr, name)}
	}

	return decidePersona(ctx, sel, sid, req)
}

// llmOnlySelection is the original behaviour (option off): one LLM call for
// every run, no sticky logic. The result of a user turn is remembered only so
// the session's applied persona can be reported.
func llmOnlySelection(ctx context.Context, sel *PersonaSelector, sid string, req personaRequest) personaOutcome {
	if sel == nil {
		return personaOutcome{}
	}
	name, err := sel.SelectPersonaName(ctx, req.Prompt)
	if err != nil {
		logging.Debug("PersonaSelector: selection call failed", "error", err)
		name = ""
	}
	if name != "" {
		logging.Debug("PersonaSelector: applying persona", "persona", name)
	}
	if req.Eligible {
		recordPersona(sid, name, nil)
	}
	return personaOutcome{Name: name, Content: personaContentOf(sel.personaMgr(), name)}
}

func personaOptions(mgr *persona.Manager) []modelrouter.PersonaOption {
	if mgr == nil {
		return nil
	}
	descs := mgr.Descriptions()
	names := make([]string, 0, len(descs))
	for n := range descs {
		names = append(names, n)
	}
	sort.Strings(names)
	opts := make([]modelrouter.PersonaOption, 0, len(names))
	for _, n := range names {
		opts = append(opts, modelrouter.PersonaOption{Name: n, Description: descs[n]})
	}
	return opts
}

// decidePersona runs the decision model path with its sticky and LLM
// fallbacks. It never fails: every problem degrades to a persona or none.
func decidePersona(ctx context.Context, sel *PersonaSelector, sid string, req personaRequest) personaOutcome {
	cfg := config.Get()
	var auto config.ModelAutoModeConfig
	var dec config.DecisionModelConfig
	if cfg != nil {
		auto = cfg.ModelAutoMode
		dec = cfg.DecisionModel
	}
	mgr := personaManager()
	pd := modelrouter.PersonaDecision{}
	fromCooldown := false

	key := personaRouterKey(dec)
	if c, cooling := personaCooldownFor(key); cooling {
		fromCooldown = true
		pd = modelrouter.PersonaDecision{Reason: modelrouter.ReasonRouterError, ErrClass: c.class}
		logging.Debug("persona_auto: decision model cooling down, using the fallback model", "error_class", c.class)
	} else if engine, err := modelrouter.ForConfig(dec); err != nil {
		pd = modelrouter.PersonaDecision{Reason: modelrouter.ReasonRouterError, Err: err, ErrClass: "config"}
	} else {
		routeCtx, cancel := context.WithTimeout(ctx, dec.EffectiveTimeout()+routerGraceOnTimeout)
		query := req.Query
		if query == "" {
			query = req.Prompt
		}
		pin := modelrouter.PersonaInput{Prompt: query, History: req.History, Personas: personaOptions(mgr), HistoryPrompts: auto.HistoryPrompts}
		if req.Combined != nil {
			req.Combined.Dec, pd = engine.RouteWithPersona(routeCtx, auto, req.Combined.In, pin)
			req.Combined.Done = true
		} else {
			pd = engine.RoutePersona(routeCtx, pin)
		}
		cancel()
	}
	if pd.Reason == modelrouter.ReasonNoRouter && pd.ErrClass == "" {
		pd.ErrClass = "no_router"
	}

	prev := rememberedPersona(sid)

	// The caller gave up (turn cancelled, caller deadline) while the router was
	// working: the answer is meaningless and the failure is not the router's.
	// No cooldown, warning, fallback call, event or state change; keep the
	// current persona. The router's own timeout does not set ctx.Err().
	if ctx.Err() != nil {
		if req.Combined != nil {
			// Let the model route take its own decision as it did before.
			req.Combined.Done = false
		}
		name := ""
		switch {
		case prev != "" && mgr != nil && mgr.HasPersona(prev):
			name = prev
		case mgr != nil && mgr.HasPersona(config.DefaultPersona):
			name = config.DefaultPersona
		}
		return personaOutcome{Name: name, Content: personaContentOf(mgr, name)}
	}
	name, source := "", ""
	var warning string

	switch pd.Reason {
	case modelrouter.ReasonMatched:
		if mgr != nil && mgr.HasPersona(pd.Persona) {
			name, source = pd.Persona, PersonaSourceDecision
		}
	case modelrouter.ReasonRouterError, modelrouter.ReasonNoRouter:
		if pd.Reason == modelrouter.ReasonRouterError && !fromCooldown {
			startPersonaCooldown(key, pd.ErrClass)
		}
		if warnAutoOnce(sid, "persona:"+pd.ErrClass) {
			warning = fmt.Sprintf("Persona auto-select: decision model unavailable (%s), using the fallback model",
				routerErrorWording(pd.ErrClass))
			logging.Warn("persona_auto: decision model unavailable", "session_id", sid,
				"error_class", pd.ErrClass, "error", pd.Err)
		} else {
			logging.Debug("persona_auto: decision model unavailable (already warned)",
				"session_id", sid, "error_class", pd.ErrClass, "error", pd.Err)
		}
		if llm := llmFallback(ctx, sel, mgr, req.Prompt); llm != "" {
			name, source = llm, PersonaSourceLLM
		}
	}

	if source == "" {
		// Sticky: the previous persona of the session; assistant when there is
		// none; no persona when even that does not exist.
		switch {
		case prev != "" && mgr != nil && mgr.HasPersona(prev):
			name, source = prev, PersonaSourceSticky
		case mgr != nil && mgr.HasPersona(config.DefaultPersona):
			name, source = config.DefaultPersona, PersonaSourceDefault
		default:
			name, source = "", PersonaSourceDefault
		}
	}

	info := &PersonaRoutingInfo{
		Persona:     name,
		Source:      source,
		Reason:      pd.Reason,
		Probability: pd.Probability,
		LatencyMs:   pd.LatencyMs,
		CostUSD:     pd.CostUSD,
		ErrClass:    pd.ErrClass,
		Changed:     name != prev,
	}
	recordPersona(sid, name, info)

	// One Info line and one external event per decision. Ids, names and
	// numbers only: Info records can be shipped by opt-in telemetry, so never
	// the prompt.
	logging.Info("persona_auto: decision",
		"session_id", sid,
		"persona", name,
		"source", source,
		"reason", pd.Reason,
		"probability", pd.Probability,
		"router_latency_ms", pd.LatencyMs,
		"cost_usd", costForLog(pd.CostUSD),
		"error_class", pd.ErrClass,
	)
	extevents.PersonaRouted(extevents.PersonaRoutedInfo{
		SessionID:   sid,
		Persona:     name,
		Source:      source,
		Reason:      pd.Reason,
		Probability: pd.Probability,
		LatencyMs:   pd.LatencyMs,
		CostUSD:     pd.CostUSD,
		ErrorClass:  pd.ErrClass,
		Changed:     info.Changed,
	})
	return personaOutcome{Name: name, Content: personaContentOf(mgr, name), Info: info, Warning: warning}
}

func costForLog(c *float64) float64 {
	if c == nil {
		return 0
	}
	return *c
}

// llmFallback asks the persona-selector LLM. It returns "" when the model is
// unavailable, fails or answers none; the caller then keeps the sticky persona.
func llmFallback(ctx context.Context, sel *PersonaSelector, mgr *persona.Manager, prompt string) string {
	if sel == nil {
		return ""
	}
	callCtx, cancel := context.WithTimeout(ctx, personaLLMFallbackTimeout)
	defer cancel()
	name, err := sel.SelectPersonaName(callCtx, prompt)
	if err != nil {
		logging.Debug("persona_auto: fallback model unavailable", "error", err)
		return ""
	}
	if name == "" || mgr == nil || !mgr.HasPersona(name) {
		return ""
	}
	return name
}

// personaNotice is the status line shown when the applied persona changes.
func personaNotice(info PersonaRoutingInfo) string {
	switch info.Source {
	case PersonaSourceDecision:
		return fmt.Sprintf("Persona: %s (p=%.2f, %d ms)", info.Persona, info.Probability, info.LatencyMs)
	case PersonaSourceLLM:
		return fmt.Sprintf("Persona: %s (fallback model)", info.Persona)
	case PersonaSourceDefault:
		return fmt.Sprintf("Persona: %s (default)", info.Persona)
	default:
		return fmt.Sprintf("Persona: %s", info.Persona)
	}
}

// --- agent glue -------------------------------------------------------------

// personaEligible reports whether the run is a user turn that may take a
// persona decision: the coder agent, not system initiated.
func (a *agent) personaEligible(ctx context.Context, sessionID string) bool {
	return a.agentName == config.AgentCoder &&
		sessionID != "" &&
		!isCleanModeContext(ctx) &&
		!isSystemInitiatedRun(ctx)
}

// resolvePersona picks the persona for one run, announces a change and, when
// route is set, lets the engine take the model auto mode decision in the same
// request as the persona decision.
func (a *agent) resolvePersona(
	ctx context.Context,
	sessionID, parentSessionID, content, query string,
	priorMsgs []message.Message,
	route *combinedRoute,
	eventCh chan<- AgentEvent,
) string {
	req := personaRequest{
		Prompt:          content,
		Query:           query,
		Eligible:        a.personaEligible(ctx, sessionID),
		ParentSessionID: parentSessionID,
		Combined:        route,
	}
	if cfg := config.Get(); cfg != nil {
		req.History = recentUserPrompts(priorMsgs, cfg.ModelAutoMode.HistoryPrompts)
	}
	out := resolvePersonaContent(ctx, req)
	a.announcePersona(sessionID, eventCh, out)
	return out.Content
}

// announcePersona emits the degradation warning (once per session and class)
// and the persona notice, which only appears when the applied persona changed.
func (a *agent) announcePersona(sessionID string, eventCh chan<- AgentEvent, out personaOutcome) {
	if out.Warning != "" {
		a.emitSystemNotice(sessionID, eventCh, out.Warning)
	}
	if out.Info != nil && out.Info.Changed && out.Info.Persona != "" {
		a.emitSystemNotice(sessionID, eventCh, personaNotice(*out.Info))
	}
}

// emitSystemNotice publishes a one-line system message to every client and
// records it as a run status message, like emitRoutingNotice but without a
// model routing payload.
func (a *agent) emitSystemNotice(sessionID string, eventCh chan<- AgentEvent, text string) {
	a.addRunStatusMessage(sessionID, text)
	ev := AgentEvent{
		Type:          AgentEventTypeSystemMessage,
		SessionID:     sessionID,
		SystemMessage: text + "\n",
	}
	a.publishEvent(ev)
	if eventCh != nil {
		select {
		case eventCh <- ev:
		default:
		}
	}
}
