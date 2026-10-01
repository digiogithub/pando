package rag

import (
	"context"
	"time"
)

// Candidate sources reported to a RelevanceFilter.
const (
	SourceCode   = "code"
	SourceKB     = "kb"
	SourceEvents = "events"
	SourceMemory = "memory"
)

// RelevanceCandidate is one retrieved item that may be injected into the prompt.
type RelevanceCandidate struct {
	// Source is "code", "kb", "events" or "memory".
	Source string
	// ID identifies the item inside its source (path, symbol, event id, memory key).
	ID string
	// Text is the excerpt that would be injected, used to judge relevance.
	Text string
	// Score is the retrieval (embedding/hybrid) score.
	Score float64
	// Pinned marks candidates that must never be dropped (pinned memory scopes).
	// Filters may still look at them but the caller keeps them regardless.
	Pinned bool
}

// FilterResult describes what a RelevanceFilter did on one call.
type FilterResult struct {
	// Applied is true when the decision model judged at least one candidate.
	Applied bool
	// Kept and Dropped count the candidates after filtering.
	Kept, Dropped int
	// BySource holds {kept, dropped} per candidate source.
	BySource map[string][2]int
	// Latency is the wall-clock time spent in the filter.
	Latency time.Duration
	// Reason is empty on full success, otherwise the error class
	// (unreachable, timeout, ...) or "no_router", "hosted_provider", "partial".
	Reason string
	// Probabilities maps candidate ID to p(useful) for the judged candidates.
	Probabilities map[string]float64
}

// RelevanceFilter decides which retrieved candidates are useful for a prompt.
// Implementations must be fail-open: on any problem they keep everything and
// report Applied=false. keep has the same length as cands.
type RelevanceFilter interface {
	Filter(ctx context.Context, prompt string, cands []RelevanceCandidate) (keep []bool, res FilterResult)
}

// NewFilterResult builds a FilterResult with per-source counts from keep.
// It is a helper for filter implementations and for the callers of Filter.
func NewFilterResult(cands []RelevanceCandidate, keep []bool) FilterResult {
	res := FilterResult{BySource: map[string][2]int{}}
	for i, c := range cands {
		k := keep == nil || (i < len(keep) && keep[i])
		bs := res.BySource[c.Source]
		if k {
			res.Kept++
			bs[0]++
		} else {
			res.Dropped++
			bs[1]++
		}
		res.BySource[c.Source] = bs
	}
	return res
}

type noRelevanceFilterKey struct{}

// WithoutRelevanceFilter returns a context under which the enrichment and
// memory pipelines skip the relevance filter and keep every candidate. The
// agent uses it for turns that are not eligible for filtering (subagents,
// the context-enricher agent, resumed delegations).
func WithoutRelevanceFilter(ctx context.Context) context.Context {
	return context.WithValue(ctx, noRelevanceFilterKey{}, true)
}

// RelevanceFilterDisabled reports whether ctx came from WithoutRelevanceFilter.
func RelevanceFilterDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(noRelevanceFilterKey{}).(bool)
	return v
}

// applyRelevanceFilter runs f over cands and returns the keep mask plus the
// result with counts filled in. A nil filter, an empty candidate list or a
// malformed mask keeps everything. Pinned candidates are always kept.
func applyRelevanceFilter(ctx context.Context, f RelevanceFilter, prompt string, cands []RelevanceCandidate) ([]bool, FilterResult) {
	allKept := make([]bool, len(cands))
	for i := range allKept {
		allKept[i] = true
	}
	if f == nil || len(cands) == 0 || RelevanceFilterDisabled(ctx) {
		return allKept, FilterResult{}
	}
	start := time.Now()
	keep, res := f.Filter(ctx, prompt, cands)
	if len(keep) != len(cands) {
		keep = allKept
		res.Applied = false
		if res.Reason == "" {
			res.Reason = "malformed"
		}
	}
	for i, c := range cands {
		if c.Pinned {
			keep[i] = true
		}
	}
	counts := NewFilterResult(cands, keep)
	res.Kept, res.Dropped, res.BySource = counts.Kept, counts.Dropped, counts.BySource
	if res.Latency <= 0 {
		res.Latency = time.Since(start)
	}
	return keep, res
}
