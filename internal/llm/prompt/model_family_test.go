package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyModelFamily(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		want     ModelFamily
	}{
		// Anthropic — Claude 4.x
		{"claude-opus-4-6", "anthropic", "claude-opus-4-6", FamilyClaude4},
		{"claude-sonnet-4-6", "anthropic", "claude-sonnet-4-6", FamilyClaude4},
		{"claude-sonnet-4-20250514", "anthropic", "claude-sonnet-4-20250514", FamilyClaude4},
		{"claude-opus-4-5-20251101", "anthropic", "claude-opus-4-5-20251101", FamilyClaude4},
		{"claude-haiku-4-5-20251001", "anthropic", "claude-haiku-4-5-20251001", FamilyClaude4},
		// Anthropic — Claude 3.7 is reasoning-capable → Claude 4 family
		{"claude-3-7-sonnet", "anthropic", "claude-3-7-sonnet-20250219", FamilyClaude4},
		{"claude-3.7-sonnet", "anthropic", "claude-3.7-sonnet", FamilyClaude4},
		// Anthropic — Claude 3.x
		{"claude-3-haiku", "anthropic", "claude-3-haiku-20240307", FamilyClaude3},
		{"claude-3-opus", "anthropic", "claude-3-opus-20240229", FamilyClaude3},
		{"claude-3-5-sonnet", "anthropic", "claude-3-5-sonnet-20241022", FamilyClaude3},
		// Bedrock maps to Anthropic family
		{"bedrock-claude-4", "bedrock", "claude-sonnet-4-20250514", FamilyClaude4},
		{"bedrock-claude-3", "bedrock", "claude-3-haiku-20240307", FamilyClaude3},

		// OpenAI — o-series
		{"o1", "openai", "o1", FamilyOSeries},
		{"o1-mini", "openai", "o1-mini", FamilyOSeries},
		{"o1-pro", "openai", "o1-pro", FamilyOSeries},
		{"o3", "openai", "o3", FamilyOSeries},
		{"o3-mini", "openai", "o3-mini", FamilyOSeries},
		{"o4-mini", "openai", "o4-mini", FamilyOSeries},
		// OpenAI — GPT-5
		{"gpt-5", "openai", "gpt-5", FamilyGPT5},
		{"gpt-5.4", "openai", "gpt-5.4", FamilyGPT5},
		// OpenAI — GPT (standard)
		{"gpt-4o", "openai", "gpt-4o", FamilyGPT},
		{"gpt-4o-mini", "openai", "gpt-4o-mini", FamilyGPT},
		{"gpt-4.1", "openai", "gpt-4.1", FamilyGPT},
		{"gpt-4.1-mini", "openai", "gpt-4.1-mini", FamilyGPT},
		// Azure maps to OpenAI families
		{"azure-o3", "azure", "o3", FamilyOSeries},
		{"azure-gpt-4o", "azure", "gpt-4o", FamilyGPT},

		// Copilot — wraps multiple providers
		{"copilot-claude-4", "copilot", "claude-sonnet-4", FamilyClaude4},
		{"copilot-claude-3.5", "copilot", "claude-3.5-sonnet", FamilyClaude3},
		{"copilot-o3-mini", "copilot", "o3-mini", FamilyOSeries},
		{"copilot-gpt-5.4", "copilot", "gpt-5.4", FamilyGPT5},
		{"copilot-gpt-4o", "copilot", "gpt-4o", FamilyGPT},
		{"copilot-gemini-3", "copilot", "gemini-3.0-flash", FamilyGemini3},

		// Gemini
		{"gemini-3-pro", "gemini", "gemini-3-pro", FamilyGemini3},
		{"gemini-3.1-pro", "gemini", "gemini-3.1-pro", FamilyGemini3},
		{"gemini-2.5-pro", "gemini", "gemini-2.5-pro", FamilyGemini},
		{"gemini-2.0-flash", "gemini", "gemini-2.0-flash", FamilyGemini},
		// Antigravity maps to Gemini families
		{"antigravity-gemini-3", "antigravity", "gemini-3-pro", FamilyGemini3},

		// Ollama — no family classification
		{"ollama-llama", "ollama", "llama3:latest", FamilyDefault},
		// Unknown provider
		{"unknown-provider", "custom", "some-model", FamilyDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyModelFamily(tt.provider, tt.model)
			assert.Equal(t, tt.want, got, "ClassifyModelFamily(%q, %q)", tt.provider, tt.model)
		})
	}
}

func TestIsOSeriesModel(t *testing.T) {
	assert.True(t, isOSeriesModel("o1"))
	assert.True(t, isOSeriesModel("o1-mini"))
	assert.True(t, isOSeriesModel("o3"))
	assert.True(t, isOSeriesModel("o3-mini"))
	assert.True(t, isOSeriesModel("o4-mini"))
	assert.False(t, isOSeriesModel("openai-gpt"))
	assert.False(t, isOSeriesModel(""))
	assert.False(t, isOSeriesModel("o"))
	assert.False(t, isOSeriesModel("gpt-4o"))
}
