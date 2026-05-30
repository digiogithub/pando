package prompt

import "strings"

// ModelFamily represents a classification of models within a provider
// that share similar capabilities and warrant different prompting strategies.
type ModelFamily string

const (
	// Anthropic families
	FamilyClaude4 ModelFamily = "claude-4" // Claude 4.x — reasoning capable, extended thinking
	FamilyClaude3 ModelFamily = "claude-3" // Claude 3.x — older, standard chat

	// OpenAI families
	FamilyGPT5    ModelFamily = "gpt-5"    // GPT-5.x — latest flagship
	FamilyGPT     ModelFamily = "gpt"      // GPT-4o, GPT-4.1, etc. — standard chat
	FamilyOSeries ModelFamily = "o-series" // o1, o3, o4 — reasoning-first models

	// Gemini families
	FamilyGemini3 ModelFamily = "gemini-3" // Gemini 3.x — latest
	FamilyGemini  ModelFamily = "gemini"   // Gemini 2.x and older

	// Fallback
	FamilyDefault ModelFamily = "" // Use provider-level template
)

// ClassifyModelFamily determines the model family from the provider and model
// API name. This drives template selection: the builder will look for
// providers/{provider}/{family}.md.tpl before falling back to providers/{provider}.md.tpl.
func ClassifyModelFamily(provider, model string) ModelFamily {
	p := strings.ToLower(strings.TrimSpace(provider))
	m := strings.ToLower(strings.TrimSpace(model))

	switch {
	case p == "anthropic" || p == "bedrock" || p == "vertexai":
		return classifyAnthropicFamily(m)
	case p == "openai" || p == "azure":
		return classifyOpenAIFamily(m)
	case p == "copilot":
		return classifyCopilotFamily(m)
	case p == "gemini" || p == "antigravity":
		return classifyGeminiFamily(m)
	default:
		return FamilyDefault
	}
}

func classifyAnthropicFamily(model string) ModelFamily {
	// Claude 4.x models: claude-sonnet-4*, claude-opus-4*, claude-haiku-4*
	if strings.Contains(model, "claude-sonnet-4") ||
		strings.Contains(model, "claude-opus-4") ||
		strings.Contains(model, "claude-haiku-4") {
		return FamilyClaude4
	}
	// Claude 3.7 with reasoning
	if strings.Contains(model, "claude-3-7") || strings.Contains(model, "claude-3.7") {
		return FamilyClaude4 // 3.7 is reasoning-capable like 4.x
	}
	// Claude 3.x
	if strings.Contains(model, "claude-3") || strings.Contains(model, "claude") {
		return FamilyClaude3
	}
	return FamilyDefault
}

func classifyOpenAIFamily(model string) ModelFamily {
	// o-series reasoning models
	if isOSeriesModel(model) {
		return FamilyOSeries
	}
	// GPT-5.x
	if strings.HasPrefix(model, "gpt-5") {
		return FamilyGPT5
	}
	// GPT-4.x, GPT-4o
	if strings.HasPrefix(model, "gpt-4") || strings.HasPrefix(model, "gpt-") {
		return FamilyGPT
	}
	return FamilyDefault
}

func classifyCopilotFamily(model string) ModelFamily {
	// Copilot wraps multiple providers — classify by underlying model
	if strings.Contains(model, "claude") {
		return classifyAnthropicFamily(model)
	}
	if isOSeriesModel(model) {
		return FamilyOSeries
	}
	if strings.HasPrefix(model, "gpt-5") {
		return FamilyGPT5
	}
	if strings.HasPrefix(model, "gpt-") {
		return FamilyGPT
	}
	if strings.Contains(model, "gemini") {
		return classifyGeminiFamily(model)
	}
	return FamilyDefault
}

func classifyGeminiFamily(model string) ModelFamily {
	if strings.Contains(model, "gemini-3") {
		return FamilyGemini3
	}
	return FamilyGemini
}

// isOSeriesModel detects OpenAI o-series reasoning models (o1, o3, o4, etc.)
func isOSeriesModel(model string) bool {
	// Match: "o1", "o1-mini", "o1-pro", "o3", "o3-mini", "o4-mini"
	if len(model) < 2 {
		return false
	}
	if model[0] != 'o' {
		return false
	}
	if model[1] >= '0' && model[1] <= '9' {
		// Ensure it's not something like "openai..." by checking the next char
		if len(model) == 2 {
			return true
		}
		return model[2] == '-' || model[2] == '.'
	}
	return false
}
