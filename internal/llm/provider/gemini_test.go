package provider

import (
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
	"google.golang.org/genai"
)

func TestGeminiBuildThinkingConfig(t *testing.T) {
	tests := []struct {
		name       string
		apiModel   string
		wantNil    bool
		wantBudget *int32
		wantLevel  genai.ThinkingLevel
	}{
		{
			name:      "gemini 3 uses high thinking level",
			apiModel:  "gemini-3.1-pro-preview-customtools",
			wantLevel: genai.ThinkingLevelHigh,
		},
		{
			name:      "gemini 3 with models prefix uses high thinking level",
			apiModel:  "models/gemini-3-flash",
			wantLevel: genai.ThinkingLevelHigh,
		},
		{
			name:       "gemini 2.5 uses thinking budget",
			apiModel:   "gemini-2.5-pro-preview-05-06",
			wantBudget: int32Ptr(2000),
		},
		{
			name:     "older gemini model has no thinking config",
			apiModel: "gemini-2.0-flash",
			wantNil:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &geminiClient{
				providerOptions: providerClientOptions{
					model: models.Model{APIModel: tc.apiModel},
				},
			}

			got := client.buildThinkingConfig()
			if tc.wantNil {
				if got != nil {
					t.Fatalf("buildThinkingConfig() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("buildThinkingConfig() = nil, want non-nil")
			}
			if !got.IncludeThoughts {
				t.Fatal("buildThinkingConfig().IncludeThoughts = false, want true")
			}
			if tc.wantBudget != nil {
				if got.ThinkingBudget == nil {
					t.Fatal("buildThinkingConfig().ThinkingBudget = nil, want non-nil")
				}
				if *got.ThinkingBudget != *tc.wantBudget {
					t.Fatalf("buildThinkingConfig().ThinkingBudget = %d, want %d", *got.ThinkingBudget, *tc.wantBudget)
				}
				return
			}
			if got.ThinkingLevel != tc.wantLevel {
				t.Fatalf("buildThinkingConfig().ThinkingLevel = %v, want %v", got.ThinkingLevel, tc.wantLevel)
			}
		})
	}
}

func TestVisibleTextPartsFiltersThoughtContent(t *testing.T) {
	parts := []*genai.Part{
		{Text: "visible-1", Thought: false},
		{Text: "hidden-thought", Thought: true},
		{Text: "", Thought: false},
		{Text: "visible-2", Thought: false},
		nil,
	}

	got := visibleTextParts(parts)
	if len(got) != 2 {
		t.Fatalf("len(visibleTextParts()) = %d, want 2", len(got))
	}
	if got[0] != "visible-1" || got[1] != "visible-2" {
		t.Fatalf("visibleTextParts() = %v, want [visible-1 visible-2]", got)
	}

	joined := joinVisibleTextParts(parts)
	if joined != "visible-1visible-2" {
		t.Fatalf("joinVisibleTextParts() = %q, want %q", joined, "visible-1visible-2")
	}
}
