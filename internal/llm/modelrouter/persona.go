package modelrouter

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/logging"
)

// Persona decision reasons not shared with model routing.
const (
	ReasonNoPersonas = "no_personas"
	ReasonNoRouter   = "no_router"
)

// PersonaThreshold is the minimum probability of the chosen persona.
const PersonaThreshold = 0.60

const (
	personaQuestionName     = "persona"
	personaInstructionsText = "Which assistant persona is best suited to handle the user's latest request?"

	// maxPersonas leaves one of the 26 choice criteria for "none".
	maxPersonas            = systemone.MaxChoiceCriteria - 1
	maxPersonaDescription  = 500
	maxPersonaKeyLen       = 40
	fallbackPersonaKeyBase = "persona"
)

// PersonaOption is one persona the router may pick.
type PersonaOption struct {
	Name        string // persona name as known to the persona manager
	Description string // when this persona applies (<= 500 chars)
}

// PersonaInput is what the engine needs to pick a persona for one prompt.
type PersonaInput struct {
	Prompt   string
	History  []string // previous user prompts, newest last (may be empty)
	Personas []PersonaOption
}

// PersonaDecision is the outcome of classifying one prompt into a persona.
type PersonaDecision struct {
	Persona     string // "" when not matched
	Matched     bool
	Probability float64
	Confidence  float64
	// Probabilities is keyed by persona NAME plus the reserved "none". A persona
	// literally named "none" is reported under its criterion key (e.g. "none_2")
	// so it never overwrites the real "none" entry.
	Probabilities  map[string]float64
	Reason         string
	Err            error
	ErrClass       string
	RouterProvider string
	RouterModel    string
	LatencyMs      int64
	CostUSD        *float64 // 0/nil when the request was shared with the model decision (see RouteWithPersona)
	InputTokens    int
	Offered        int      // personas actually sent
	Dropped        []string // personas left out by the 25 cap
}

// personaQuestion is the persona question plus the key -> name mapping.
type personaQuestion struct {
	q        systemone.Question
	names    map[string]string // criterion key -> persona name
	overhead int
	offered  int
	dropped  []string
}

// RoutePersona classifies in.Prompt into one of in.Personas. It never fails:
// any router problem yields Reason ReasonRouterError (or ReasonNoRouter when no
// router model is configured) with Err/ErrClass set.
func (e *Engine) RoutePersona(ctx context.Context, in PersonaInput) PersonaDecision {
	pd, pq, ok := e.personaBase(in)
	if !ok {
		return pd
	}
	task := Input{Prompt: in.Prompt, History: in.History}
	resp, latency, err := e.ask(ctx, task, map[string]systemone.Question{personaQuestionName: pq.q}, pq.overhead)
	pd.LatencyMs = latency
	e.finishPersona(&pd, pq, resp, err)
	return pd
}

// RouteWithPersona takes the model-route decision and the persona decision
// from ONE /v1/systemone request carrying two questions ("task" and
// "persona"). The state comes from in; p.Prompt and p.History are ignored in
// that case. The request's CostUSD and InputTokens are attributed to the model
// Decision only and left nil/0 on the PersonaDecision so the cost is not
// counted twice; LatencyMs is reported on both. When either question has nothing to ask (no enabled routes, no
// personas, no router model) the decisions are taken separately, which sends
// at most one question.
func (e *Engine) RouteWithPersona(ctx context.Context, in Input, p PersonaInput) (Decision, PersonaDecision) {
	routes := e.cfg.EnabledRoutes()
	pd, pq, ok := e.personaBase(p)
	if len(routes) == 0 || !ok {
		if ok {
			pd = e.RoutePersona(ctx, PersonaInput{Prompt: in.Prompt, History: in.History, Personas: p.Personas})
		}
		return e.Route(ctx, in), pd
	}

	d := Decision{
		RouterProvider: pd.RouterProvider,
		RouterModel:    pd.RouterModel,
		Candidates:     coderOnly(in.CoderModel),
	}
	criteria := make([]systemone.Criterion, 0, len(routes)+1)
	overhead := questionOverhead(instructionsText) + pq.overhead
	for _, r := range routes {
		criteria = append(criteria, systemone.NewCriterion(r.ID, r.Description))
		overhead += EstimateTokens(r.ID) + EstimateTokens(r.Description) + 4
	}
	criteria = append(criteria, systemone.NewCriterion(noneKey, noneDescription))
	tq := systemone.Question{Type: "choice", Instructions: instructionsText, Criteria: criteria}

	resp, latency, err := e.ask(ctx, in, map[string]systemone.Question{
		questionName:        tq,
		personaQuestionName: pq.q,
	}, overhead)
	d.LatencyMs, pd.LatencyMs = latency, latency

	var ans systemone.Answer
	terr := err
	if terr == nil {
		ans, terr = answerFor(resp, questionName, tq)
	}
	if terr != nil {
		d.Reason, d.Err, d.ErrClass = ReasonRouterError, terr, classify(terr)
	} else {
		e.applyTaskAnswer(&d, routes, resp, ans)
	}
	e.finishPersona(&pd, pq, resp, err)
	pd.CostUSD, pd.InputTokens = nil, 0
	return d, pd
}

// personaBase prepares a decision and its question. ok is false when no
// question can be asked; pd then already carries the final reason.
func (e *Engine) personaBase(in PersonaInput) (pd PersonaDecision, pq personaQuestion, ok bool) {
	pd = PersonaDecision{
		RouterProvider: string(e.cfg.Router.EffectiveProvider()),
		RouterModel:    e.cfg.Router.Model,
	}
	if strings.TrimSpace(e.cfg.Router.Model) == "" {
		pd.Reason = ReasonNoRouter
		return pd, pq, false
	}
	pq = buildPersonaQuestion(in.Personas)
	pd.Offered, pd.Dropped = pq.offered, pq.dropped
	if pq.offered == 0 {
		pd.Reason = ReasonNoPersonas
		return pd, pq, false
	}
	return pd, pq, true
}

// finishPersona turns the outcome of a request into the persona decision.
// err is the transport failure of the whole request, if any.
func (e *Engine) finishPersona(pd *PersonaDecision, pq personaQuestion, resp *systemone.Response, err error) {
	var ans systemone.Answer
	if err == nil {
		ans, err = answerFor(resp, personaQuestionName, pq.q)
	}
	if err != nil {
		pd.Reason = ReasonRouterError
		pd.Err = err
		pd.ErrClass = classify(err)
		return
	}
	pd.InputTokens = resp.Usage.InputTokens
	pd.CostUSD = resp.Usage.Cost
	pd.Confidence = ans.Confidence
	pd.Probabilities = make(map[string]float64, len(ans.Probabilities))
	for k, v := range ans.Probabilities {
		if name, ok := pq.names[k]; ok && !strings.EqualFold(name, noneKey) {
			k = name
		}
		pd.Probabilities[k] = v
	}
	pd.Probability = ans.Probabilities[ans.Choice]

	name, isPersona := pq.names[ans.Choice]
	switch {
	case ans.Choice == noneKey || !isPersona:
		pd.Reason = ReasonNoMatch
	case pd.Probability < PersonaThreshold:
		pd.Reason = ReasonLowProbability
	default:
		pd.Reason = ReasonMatched
		pd.Matched = true
		pd.Persona = name
	}
}

// PersonaEngineFor returns an engine usable for persona decisions from the
// model auto mode config, regardless of cfg.Enabled: only the router block
// and the timeout are used, and no model route is required. It shares the
// process-wide engine cache with ForConfig.
func PersonaEngineFor(cfg config.ModelAutoModeConfig) (*Engine, error) {
	return ForConfig(cfg)
}

// PersonaCap reports how many of n personas are offered to the decision model
// and how many are dropped by the 25 cap.
func PersonaCap(n int) (offered, dropped int) {
	if n > maxPersonas {
		return maxPersonas, n - maxPersonas
	}
	return n, 0
}

// buildPersonaQuestion builds the persona choice question. Personas are
// de-duplicated by name, sorted by name and capped at 25; each gets a safe,
// unique, deterministic criterion key (see personaKeys).
func buildPersonaQuestion(opts []PersonaOption) personaQuestion {
	seen := map[string]bool{}
	list := make([]PersonaOption, 0, len(opts))
	for _, o := range opts {
		o.Name = strings.TrimSpace(o.Name)
		if o.Name == "" || seen[o.Name] {
			continue
		}
		seen[o.Name] = true
		o.Description = truncateRunes(strings.TrimSpace(o.Description), maxPersonaDescription)
		list = append(list, o)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	pq := personaQuestion{names: map[string]string{}}
	if len(list) > maxPersonas {
		for _, o := range list[maxPersonas:] {
			pq.dropped = append(pq.dropped, o.Name)
		}
		warnDroppedPersonas(pq.dropped)
		list = list[:maxPersonas]
	}
	pq.offered = len(list)
	if len(list) == 0 {
		return pq
	}

	keys := personaKeys(list)
	criteria := make([]systemone.Criterion, 0, len(list)+1)
	pq.overhead = questionOverhead(personaInstructionsText)
	for i, o := range list {
		criteria = append(criteria, systemone.NewCriterion(keys[i], o.Description))
		pq.names[keys[i]] = o.Name
		pq.overhead += EstimateTokens(keys[i]) + EstimateTokens(o.Description) + 4
	}
	criteria = append(criteria, systemone.NewCriterion(noneKey, noneDescription))
	pq.q = systemone.Question{Type: "choice", Instructions: personaInstructionsText, Criteria: criteria}
	return pq
}

// personaKeys maps each persona (already sorted by name) to a criterion key:
// the lower-cased name with every run of characters outside [a-z0-9] replaced
// by "_", trimmed and shortened. Empty slugs become "persona"; a slug equal to
// the reserved "none" or already taken gets a numeric suffix (_2, _3, ...).
// Because the input order is deterministic, so are the keys.
func personaKeys(list []PersonaOption) []string {
	used := map[string]bool{noneKey: true}
	keys := make([]string, len(list))
	for i, o := range list {
		base := slugify(o.Name)
		if base == "" {
			base = fallbackPersonaKeyBase
		}
		key := base
		for n := 2; used[key]; n++ {
			key = base + "_" + strconv.Itoa(n)
		}
		used[key] = true
		keys[i] = key
	}
	return keys
}

func slugify(s string) string {
	var b strings.Builder
	pendingSep := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingSep && b.Len() > 0 {
				b.WriteByte('_')
			}
			pendingSep = false
			b.WriteRune(r)
		} else {
			pendingSep = true
		}
	}
	out := b.String()
	if len(out) > maxPersonaKeyLen {
		out = strings.TrimRight(out[:maxPersonaKeyLen], "_")
	}
	return out
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// warnedDropped remembers the dropped sets already logged in this process.
var warnedDropped sync.Map

// warnDroppedPersonas logs once per distinct dropped set that the 25-persona
// cap left some personas out of the decision model's choices.
func warnDroppedPersonas(dropped []string) {
	if _, seen := warnedDropped.LoadOrStore(strings.Join(dropped, "\x00"), struct{}{}); seen {
		return
	}
	logging.Warn("Persona decision model offers at most 25 personas; some were left out",
		"dropped_count", len(dropped), "dropped", strings.Join(dropped, ", "))
}
