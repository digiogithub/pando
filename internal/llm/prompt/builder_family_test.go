package prompt

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilderFamilyTemplateSelection(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		contains string // expected content from family-specific template
	}{
		{
			name:     "claude-4-uses-family-template",
			provider: "anthropic",
			model:    "claude-opus-4-6",
			contains: "Claude 4",
		},
		{
			name:     "claude-3-uses-family-template",
			provider: "anthropic",
			model:    "claude-3-haiku-20240307",
			contains: "Claude 3",
		},
		{
			name:     "o-series-uses-family-template",
			provider: "openai",
			model:    "o3-mini",
			contains: "Reasoning Model",
		},
		{
			name:     "gpt-5-uses-family-template",
			provider: "openai",
			model:    "gpt-5.4",
			contains: "GPT-5",
		},
		{
			name:     "gpt-4-falls-back-to-provider",
			provider: "openai",
			model:    "gpt-4o",
			contains: "Provider Guidelines", // from openai.md.tpl fallback
		},
		{
			name:     "gemini-3-uses-family-template",
			provider: "gemini",
			model:    "gemini-3-pro",
			contains: "Gemini 3",
		},
		{
			name:     "ollama-uses-provider-template",
			provider: "ollama",
			model:    "llama3:latest",
			contains: "Guidelines",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &PromptData{
				Provider:    tt.provider,
				Model:       tt.model,
				ModelFamily: ClassifyModelFamily(tt.provider, tt.model),
				WorkingDir:  "/test",
				Platform:    "linux",
				Date:        "5/31/2026",
			}
			builder := NewPromptBuilder("coder", tt.provider, data, nil)
			result, err := builder.Build(context.Background())
			require.NoError(t, err)
			assert.True(t, strings.Contains(result, tt.contains),
				"expected prompt to contain %q for %s/%s, got:\n%s", tt.contains, tt.provider, tt.model, result[:min(len(result), 500)])
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
