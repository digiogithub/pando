package modelrouter

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/rag"
)

// Relevance filter reasons (rag.FilterResult.Reason) besides the error classes.
const (
	RelevanceReasonNoRouter   = "no_router"
	RelevanceReasonHosted     = "hosted_provider"
	RelevanceReasonNoCands    = "no_candidates"
	RelevanceReasonPartialPfx = "partial:"

	relevanceUseful    = "useful"
	relevanceNotUseful = "not_useful"

	// relevanceGrace is added to the decision timeout for the whole Filter call
	// (batches run concurrently, each one bounded by the decision timeout).
	relevanceGrace = 250 * time.Millisecond
	// relevanceBodySlack keeps the request body safely under MaxBodyBytes: it
	// covers the JSON envelope, keys and escaping that the estimate ignores.
	relevanceBodySlack = 6 * 1024
	// relevanceQuestionOverheadBytes is the fixed JSON cost of one question.
	relevanceQuestionOverheadBytes = 96
)

const (
	relevanceInstructionsHead = "Context item retrieved for the user's request (source: %s):\n"
	relevanceInstructionsTail = "\n\nIs this item useful to carry out the user's request?"
	relevanceUsefulDesc       = "Information needed to carry out the user's request"
	relevanceNotUsefulDesc    = "Unrelated to the request, or only similar wording"
)

// RelevanceOptions configures NewRelevanceFilter. Every func is optional and is
// evaluated on each Filter call, so hot reload needs no rebuild; a nil func
// reads the current global config (config.Get()).
type RelevanceOptions struct {
	// Decision returns the shared decision model. Default: config.Get().DecisionModel.
	Decision func() config.DecisionModelConfig
	// Threshold is the minimum p(useful) to keep a candidate. Default: remembrances config.
	Threshold func() float64
	// MaxCandidates caps how many candidates are judged per call. Default: remembrances config.
	MaxCandidates func() int
	// MaxCandidateChars caps the text of one candidate. Default: remembrances config.
	MaxCandidateChars func() int
	// LocalOnly skips hosted (typesafe/custom) providers. Default: remembrances config.
	LocalOnly func() bool
	// HistoryPrompts returns previous user prompts (oldest first) added to the state.
	// Default: none.
	HistoryPrompts func() []string
	// HistoryCount is how many previous prompts to include. Default: config
	// ModelAutoMode.HistoryPrompts.
	HistoryCount func() int
}

type relevanceFilter struct{ o RelevanceOptions }

// NewRelevanceFilter returns a rag.RelevanceFilter driven by the shared
// decision model.
//
// Rule for candidates that are not judged (resolves "max candidates"): only the
// first MaxCandidates non-pinned candidates, taken in the deterministic order
// code -> kb -> events -> memory, are asked. Candidates beyond the cap, and
// candidates that do not fit any request (token budget, MaxBodyBytes), are
// KEPT: the filter never drops what the model has not seen. Kept candidates
// stay subject to the existing *MaxChars/TotalMaxChars budgets. Pinned
// candidates are never asked and always kept.
//
// The filter is fail-open: any error, timeout, malformed answer, empty router
// model or engine build failure keeps everything with Applied=false and the
// error class in Reason. When only some batches fail, the failed batch is kept,
// the others still apply, Applied=true and Reason is "partial:<class>".
func NewRelevanceFilter(opts RelevanceOptions) rag.RelevanceFilter {
	return &relevanceFilter{o: opts}
}

func (f *relevanceFilter) decision() config.DecisionModelConfig {
	if f.o.Decision != nil {
		return f.o.Decision()
	}
	if c := config.Get(); c != nil {
		return c.DecisionModel
	}
	return config.DecisionModelConfig{}
}

func (f *relevanceFilter) rem() config.RemembrancesConfig {
	if c := config.Get(); c != nil {
		return c.Remembrances
	}
	return config.RemembrancesConfig{}
}

func (f *relevanceFilter) threshold() float64 {
	if f.o.Threshold != nil {
		if t := f.o.Threshold(); t > 0 {
			return t
		}
		return config.DefaultDecisionFilterThreshold
	}
	return f.rem().DecisionFilterThreshold()
}

func (f *relevanceFilter) maxCandidates() int {
	if f.o.MaxCandidates != nil {
		if n := f.o.MaxCandidates(); n > 0 {
			return n
		}
		return config.DefaultDecisionFilterMaxCandidates
	}
	return f.rem().DecisionFilterMaxCandidates()
}

func (f *relevanceFilter) maxChars() int {
	if f.o.MaxCandidateChars != nil {
		if n := f.o.MaxCandidateChars(); n > 0 {
			return n
		}
		return config.DefaultDecisionFilterMaxCandidateChars
	}
	return f.rem().DecisionFilterMaxCandidateChars()
}

func (f *relevanceFilter) localOnly() bool {
	if f.o.LocalOnly != nil {
		return f.o.LocalOnly()
	}
	return f.rem().DecisionFilterLocalOnly()
}

func (f *relevanceFilter) history() []string {
	if f.o.HistoryPrompts == nil {
		return nil
	}
	n := 0
	if f.o.HistoryCount != nil {
		n = f.o.HistoryCount()
	} else if c := config.Get(); c != nil {
		n = c.ModelAutoMode.HistoryPrompts
	}
	if n <= 0 {
		return nil
	}
	h := f.o.HistoryPrompts()
	return lastN(h, n)
}

// relevanceSourceOrder is the deterministic asking order.
var relevanceSourceOrder = map[string]int{
	rag.SourceCode: 0, rag.SourceKB: 1, rag.SourceEvents: 2, rag.SourceMemory: 3,
}

type relevanceQ struct {
	idx  int // index into the candidate slice
	name string
	q    systemone.Question
	cost int // estimated tokens
	size int // estimated body bytes
}

type relevanceBatch struct {
	qs     []relevanceQ
	failed string // error class when the batch failed
	probs  map[int]float64
}

// Filter implements rag.RelevanceFilter.
func (f *relevanceFilter) Filter(ctx context.Context, prompt string, cands []rag.RelevanceCandidate) ([]bool, rag.FilterResult) {
	start := time.Now()
	keep := make([]bool, len(cands))
	for i := range keep {
		keep[i] = true
	}
	finish := func(res rag.FilterResult) ([]bool, rag.FilterResult) {
		counts := rag.NewFilterResult(cands, keep)
		res.Kept, res.Dropped, res.BySource = counts.Kept, counts.Dropped, counts.BySource
		res.Latency = time.Since(start)
		return keep, res
	}
	if len(cands) == 0 {
		return finish(rag.FilterResult{})
	}

	dec := f.decision()
	if strings.TrimSpace(dec.Router.Model) == "" {
		return finish(rag.FilterResult{Reason: RelevanceReasonNoRouter})
	}
	if f.localOnly() && dec.Router.EffectiveProvider() != config.DecisionProviderOllama {
		return finish(rag.FilterResult{Reason: RelevanceReasonHosted})
	}
	eng, err := ForConfig(dec)
	if err != nil {
		return finish(rag.FilterResult{Reason: ClassifyError(err)})
	}

	ctx, cancel := context.WithTimeout(ctx, dec.EffectiveTimeout()+relevanceGrace)
	defer cancel()

	// Shared state: the prompt (and history) truncated to a share of the context.
	budget := eng.ContextBudget(ctx)
	usable := budget - SafetyMargin(budget)
	state := BuildState(Input{Prompt: prompt, History: f.history()}, len(f.history()), usable/3)
	remaining := usable - EstimateTokens(state)
	bodyRoom := systemone.MaxBodyBytes - relevanceBodySlack - len(state)

	// Order, cap and size the questions.
	order := make([]int, 0, len(cands))
	for i, c := range cands {
		if !c.Pinned {
			order = append(order, i)
		}
	}
	sortStable(order, func(a, b int) bool {
		return relevanceSourceOrder[cands[a].Source] < relevanceSourceOrder[cands[b].Source]
	})
	if mc := f.maxCandidates(); len(order) > mc {
		order = order[:mc]
	}

	maxChars := f.maxChars()
	var batches []*relevanceBatch
	var cur *relevanceBatch
	curTokens, curBytes := 0, 0
	for n, i := range order {
		rq := buildRelevanceQuestion(i, n+1, cands[i], maxChars)
		if rq.cost > remaining || rq.size > bodyRoom {
			continue // cannot be asked at all: kept unseen
		}
		if cur == nil || len(cur.qs) >= systemone.MaxQuestions || curTokens+rq.cost > remaining || curBytes+rq.size > bodyRoom {
			cur = &relevanceBatch{probs: map[int]float64{}}
			batches = append(batches, cur)
			curTokens, curBytes = 0, 0
		}
		cur.qs = append(cur.qs, rq)
		curTokens += rq.cost
		curBytes += rq.size
	}
	if len(batches) == 0 {
		return finish(rag.FilterResult{Reason: RelevanceReasonNoCands})
	}

	var wg sync.WaitGroup
	for _, b := range batches {
		wg.Add(1)
		go func(b *relevanceBatch) {
			defer wg.Done()
			f.runBatch(ctx, eng, state, b)
		}(b)
	}
	wg.Wait()

	threshold := f.threshold()
	res := rag.FilterResult{Probabilities: map[string]float64{}}
	failedClass, okBatches := "", 0
	for _, b := range batches {
		if b.failed != "" {
			if failedClass == "" {
				failedClass = b.failed
			}
			continue
		}
		okBatches++
		for _, rq := range b.qs {
			p := b.probs[rq.idx]
			res.Probabilities[cands[rq.idx].ID] = p
			if p < threshold {
				keep[rq.idx] = false
			}
		}
	}
	switch {
	case okBatches == 0:
		res.Probabilities = nil
		res.Reason = failedClass
	case failedClass != "":
		res.Applied = true
		res.Reason = RelevanceReasonPartialPfx + failedClass
	default:
		res.Applied = true
	}
	return finish(res)
}

// runBatch sends one request and records p(useful) per candidate, or the error
// class when the request or any of its answers is unusable.
func (f *relevanceFilter) runBatch(ctx context.Context, eng *Engine, state string, b *relevanceBatch) {
	qs := make(map[string]systemone.Question, len(b.qs))
	for _, rq := range b.qs {
		qs[rq.name] = rq.q
	}
	resp, _, err := eng.Ask(ctx, state, qs)
	if err != nil {
		b.failed = ClassifyError(err)
		return
	}
	for _, rq := range b.qs {
		p, ok := usefulProbability(resp.Answers[rq.name])
		if !ok {
			b.failed = ErrClassMalformed
			b.probs = nil
			return
		}
		b.probs[rq.idx] = p
	}
}

// usefulProbability extracts p(useful) from an answer, validating the choice.
func usefulProbability(ans systemone.Answer) (float64, bool) {
	if ans.Choice != relevanceUseful && ans.Choice != relevanceNotUseful {
		return 0, false
	}
	if p, ok := ans.Probabilities[relevanceUseful]; ok {
		return p, true
	}
	if ans.Choice == relevanceUseful {
		return 1, true
	}
	return 0, true
}

func buildRelevanceQuestion(idx, n int, c rag.RelevanceCandidate, maxChars int) relevanceQ {
	text := ellipsizeRunes(strings.TrimSpace(c.Text), maxChars)
	instr := fmt.Sprintf(relevanceInstructionsHead, c.Source) + text + relevanceInstructionsTail
	q := systemone.Question{
		Type:         "choice",
		Instructions: instr,
		Criteria: []systemone.Criterion{
			systemone.NewCriterion(relevanceUseful, relevanceUsefulDesc),
			systemone.NewCriterion(relevanceNotUseful, relevanceNotUsefulDesc),
		},
	}
	fixed := len(relevanceUsefulDesc) + len(relevanceNotUsefulDesc) + len(relevanceUseful) + len(relevanceNotUseful)
	size := len(instr)*2 + fixed + relevanceQuestionOverheadBytes // *2: JSON escaping headroom
	return relevanceQ{
		idx:  idx,
		name: "c" + strconv.Itoa(n),
		q:    q,
		cost: EstimateTokens(instr) + EstimateTokens(relevanceUsefulDesc) + EstimateTokens(relevanceNotUsefulDesc) + 12,
		size: size,
	}
}

func ellipsizeRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max])) + "…"
}

// sortStable is an insertion sort (inputs are tiny) that keeps equal elements in order.
func sortStable(a []int, less func(x, y int) bool) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && less(a[j], a[j-1]); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
