package config

import "fmt"

// Defaults of the decision-model relevance filter applied to injected context.
const (
	DefaultDecisionFilterThreshold         = 0.60
	DefaultDecisionFilterMaxCandidates     = 32
	DefaultDecisionFilterMaxCandidateChars = 400
)

// DecisionFilterThreshold returns the keep threshold with the default applied.
func (r RemembrancesConfig) DecisionFilterThreshold() float64 {
	if r.ContextEnrichmentDecisionFilterThreshold <= 0 {
		return DefaultDecisionFilterThreshold
	}
	return r.ContextEnrichmentDecisionFilterThreshold
}

// DecisionFilterMaxCandidates returns the per-turn candidate cap with the default applied.
func (r RemembrancesConfig) DecisionFilterMaxCandidates() int {
	if r.ContextEnrichmentDecisionFilterMaxCandidates <= 0 {
		return DefaultDecisionFilterMaxCandidates
	}
	return r.ContextEnrichmentDecisionFilterMaxCandidates
}

// DecisionFilterMaxCandidateChars returns the per-candidate text cap with the default applied.
func (r RemembrancesConfig) DecisionFilterMaxCandidateChars() int {
	if r.ContextEnrichmentDecisionFilterMaxCandidateChars <= 0 {
		return DefaultDecisionFilterMaxCandidateChars
	}
	return r.ContextEnrichmentDecisionFilterMaxCandidateChars
}

// DecisionFilterLocalOnly reports whether the relevance filter must skip hosted
// decision providers. It is true unless the user opted in with AllowHosted.
func (r RemembrancesConfig) DecisionFilterLocalOnly() bool {
	return !r.ContextEnrichmentDecisionFilterAllowHosted
}

// ValidateDecisionFilter rejects out-of-range relevance filter settings.
// Zero values mean "use the default" and are valid.
func (r RemembrancesConfig) ValidateDecisionFilter() error {
	t := r.ContextEnrichmentDecisionFilterThreshold
	if t < 0 || t > 1 {
		return fmt.Errorf("context_enrichment_decision_filter_threshold must be in (0, 1], got %v", t)
	}
	if r.ContextEnrichmentDecisionFilterMaxCandidates < 0 {
		return fmt.Errorf("context_enrichment_decision_filter_max_candidates must be >= 1, got %d", r.ContextEnrichmentDecisionFilterMaxCandidates)
	}
	if r.ContextEnrichmentDecisionFilterMaxCandidateChars < 0 {
		return fmt.Errorf("context_enrichment_decision_filter_max_candidate_chars must be >= 1, got %d", r.ContextEnrichmentDecisionFilterMaxCandidateChars)
	}
	return nil
}
