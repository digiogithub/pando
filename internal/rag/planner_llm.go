package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
)

// llmPlannerResponse is the JSON shape the LLM is expected to return.
type llmPlannerResponse struct {
	Intent           string   `json:"intent"`
	KBQuery          string   `json:"kb_query"`
	CodeQuery        string   `json:"code_query"`
	EventsQuery      string   `json:"events_query"`
	PreferredSources []string `json:"preferred_sources"`
	KBResults        int      `json:"kb_results"`
	CodeResults      int      `json:"code_results"`
	EventsResults    int      `json:"events_results"`
}

// LLMPlanner uses a dedicated LLM provider to produce an EnrichmentPlan from the raw
// user prompt. If the primary provider fails it falls back to an optional secondary
// provider, and if that also fails it falls back to the HeuristicPlanner so that
// context enrichment continues to work even when the model is unavailable.
type LLMPlanner struct {
	primary   PlannerProvider
	secondary PlannerProvider // optional coder fallback; may be nil
	fallback  *HeuristicPlanner
	// systemPromptFn is injected from outside to avoid importing llm/prompt (no cycle there,
	// but keeping the dependency explicit makes the package more portable).
	systemPromptFn func(providerName string) string
}

// NewLLMPlanner creates an LLMPlanner.
// systemPromptFn builds the system prompt for the agent given the provider name.
// secondary may be nil (no coder fallback).
func NewLLMPlanner(primary, secondary PlannerProvider, systemPromptFn func(string) string) *LLMPlanner {
	return &LLMPlanner{
		primary:        primary,
		secondary:      secondary,
		fallback:       &HeuristicPlanner{},
		systemPromptFn: systemPromptFn,
	}
}

// Plan implements QueryPlanner. It asks the LLM for an optimised retrieval plan.
// On any error it degrades gracefully through secondary → heuristic.
func (p *LLMPlanner) Plan(ctx context.Context, userPrompt string) (*EnrichmentPlan, error) {
	plan, err := p.callProvider(ctx, p.primary, userPrompt)
	if err == nil {
		logging.Debug("context enricher: LLM planner succeeded with primary")
		return plan, nil
	}

	logging.Debug("context enricher: LLM planner primary failed, trying secondary", "error", err)

	if p.secondary != nil {
		plan, err = p.callProvider(ctx, p.secondary, userPrompt)
		if err == nil {
			logging.Debug("context enricher: LLM planner succeeded with secondary (fallback)")
			return plan, nil
		}
		logging.Debug("context enricher: LLM planner secondary also failed, using heuristic", "error", err)
	}

	return p.fallback.Plan(ctx, userPrompt)
}

func (p *LLMPlanner) callProvider(ctx context.Context, prov PlannerProvider, userPrompt string) (*EnrichmentPlan, error) {
	if prov == nil {
		return nil, fmt.Errorf("provider is nil")
	}

	systemMsg := ""
	if p.systemPromptFn != nil {
		systemMsg = p.systemPromptFn(prov.ProviderName())
	}

	raw, err := prov.SendRequest(ctx, systemMsg, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("provider call failed: %w", err)
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty response from provider")
	}

	// Strip optional markdown fences the model might add despite instructions.
	if after, found := strings.CutPrefix(raw, "```json"); found {
		raw = after
	} else if after, found := strings.CutPrefix(raw, "```"); found {
		raw = after
	}
	if before, found := strings.CutSuffix(raw, "```"); found {
		raw = before
	}
	raw = strings.TrimSpace(raw)

	var parsed llmPlannerResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse planner JSON: %w (raw: %.120s)", err, raw)
	}

	plan := &EnrichmentPlan{
		SemanticQuery: coalesce(parsed.KBQuery, parsed.Intent),
		CodeQuery:     coalesce(parsed.CodeQuery, parsed.KBQuery),
		EventsQuery:   coalesce(parsed.EventsQuery, parsed.KBQuery),
		KBResults:     parsed.KBResults,
		CodeResults:   parsed.CodeResults,
		EventsResults: parsed.EventsResults,
	}

	return plan, nil
}

func coalesce(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
