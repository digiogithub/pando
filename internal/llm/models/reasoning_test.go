package models

import (
	"reflect"
	"testing"
)

func TestReasoningEffortsForPrefersCatalog(t *testing.T) {
	m := Model{ReasoningEfforts: []string{"low", "high"}}
	if got := ReasoningEffortsFor(m); !reflect.DeepEqual(got, []string{"low", "high"}) {
		t.Fatalf("ReasoningEffortsFor() = %v, want the stored list", got)
	}
}

func TestReasoningEffortsForAnthropic(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want []string
	}{
		{"opus-4.7 adaptive", "claude-opus-4-7", []string{"low", "medium", "high", "xhigh", "max"}},
		{"sonnet-5 adaptive", "claude-sonnet-5", []string{"low", "medium", "high", "xhigh", "max"}},
		{"fable-5 adaptive", "claude-fable-5", []string{"low", "medium", "high", "xhigh", "max"}},
		{"opus-4.6", "claude-opus-4-6", []string{"low", "medium", "high", "max"}},
		{"sonnet-4.6", "claude-sonnet-4.6", []string{"low", "medium", "high", "max"}},
		{"opus-4.5", "claude-opus-4-5", []string{"low", "medium", "high"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReasoningEffortsFor(Model{Provider: ProviderAnthropic, APIModel: tt.id})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ReasoningEffortsFor(%s) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestReasoningEffortsForOpenAI(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want []string
	}{
		{"gpt-5-pro", "gpt-5-pro", []string{"high"}},
		{"gpt-5 chat", "gpt-5-chat", []string{"medium"}},
		{"gpt-5.1", "gpt-5.1", []string{"none", "low", "medium", "high"}},
		{"gpt-5.2", "gpt-5.2", []string{"none", "low", "medium", "high", "xhigh"}},
		{"generic", "gpt-4o", []string{"low", "medium", "high"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReasoningEffortsFor(Model{Provider: ProviderOpenAI, APIModel: tt.id})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ReasoningEffortsFor(%s) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestDefaultReasoningEffortNeverInvalid(t *testing.T) {
	if got := DefaultReasoningEffort(Model{ReasoningEfforts: []string{"low", "high"}}); got != "high" {
		t.Fatalf("default without medium = %q, want high", got)
	}
	if got := DefaultReasoningEffort(Model{ReasoningEfforts: []string{"minimal", "low"}}); got != "minimal" {
		t.Fatalf("default without medium/high = %q, want the weakest value", got)
	}
	if got := DefaultReasoningEffort(Model{Provider: ProviderOpenAI, APIModel: "gpt-5-pro"}); got != "high" {
		t.Fatalf("gpt-5-pro default = %q, want high", got)
	}
	if got := DefaultReasoningEffort(Model{}); got != "medium" {
		t.Fatalf("unknown-model default = %q, want medium", got)
	}
}

func TestNormalizeReasoningEffortClamps(t *testing.T) {
	m := Model{Provider: ProviderOpenAI, APIModel: "gpt-5.1"}
	if got := NormalizeReasoningEffort(m, "medium"); got != "medium" {
		t.Fatalf("valid medium = %q, want medium", got)
	}
	if got := NormalizeReasoningEffort(m, "max"); got != "" {
		t.Fatalf("invalid max for gpt-5.1 = %q, want empty", got)
	}
	if got := NormalizeReasoningEffort(m, ""); got != "" {
		t.Fatalf("empty = %q, want empty", got)
	}
}