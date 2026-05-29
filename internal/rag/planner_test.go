package rag

import (
	"context"
	"strings"
	"testing"
)

func TestHeuristicPlanner_RemovesImperativePrefixes(t *testing.T) {
	p := &HeuristicPlanner{}
	cases := []struct {
		prompt  string
		wantNot string // substring that should NOT appear in SemanticQuery
	}{
		{"analiza cómo funciona el context enrichment", "analiza"},
		{"review how context enrichment works", "review"},
		{"implementa un nuevo planner para RAG", "implementa"},
		{"explain the ContextEnricher struct", "explain"},
	}
	for _, c := range cases {
		plan, err := p.Plan(context.Background(), c.prompt)
		if err != nil {
			t.Fatalf("Plan(%q) error: %v", c.prompt, err)
		}
		if strings.HasPrefix(strings.ToLower(plan.SemanticQuery), c.wantNot) {
			t.Errorf("SemanticQuery %q should not start with %q", plan.SemanticQuery, c.wantNot)
		}
	}
}

func TestHeuristicPlanner_CodeQueryContainsIdentifiers(t *testing.T) {
	p := &HeuristicPlanner{}
	prompt := "analiza cómo funciona ContextEnricher y SetContextEnricher en pando"
	plan, err := p.Plan(context.Background(), prompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.CodeQuery, "ContextEnricher") {
		t.Errorf("CodeQuery %q should contain 'ContextEnricher'", plan.CodeQuery)
	}
	if !strings.Contains(plan.CodeQuery, "SetContextEnricher") {
		t.Errorf("CodeQuery %q should contain 'SetContextEnricher'", plan.CodeQuery)
	}
}

func TestHeuristicPlanner_EventsQueryIsShort(t *testing.T) {
	p := &HeuristicPlanner{}
	longPrompt := strings.Repeat("context enrichment remembrances pando implementation details ", 10)
	plan, err := p.Plan(context.Background(), longPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.EventsQuery) > 100 {
		t.Errorf("EventsQuery too long (%d chars): %q", len(plan.EventsQuery), plan.EventsQuery)
	}
}

func TestHeuristicPlanner_FallbackOnEmptyPrompt(t *testing.T) {
	p := &HeuristicPlanner{}
	plan, err := p.Plan(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic; queries may be empty but plan is valid.
	_ = plan
}
