package models

import "maps"

type (
	ModelID       string
	ModelProvider string
)

type Model struct {
	ID                 ModelID       `json:"id"`
	Name               string        `json:"name"`
	Provider           ModelProvider `json:"provider"`
	APIModel           string        `json:"api_model"`
	CostPer1MIn        float64       `json:"cost_per_1m_in"`
	CostPer1MOut       float64       `json:"cost_per_1m_out"`
	CostPer1MInCached  float64       `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached float64       `json:"cost_per_1m_out_cached"`
	ContextWindow      int64         `json:"context_window"`
	DefaultMaxTokens   int64         `json:"default_max_tokens"`
	CanReason          bool          `json:"can_reason"`
	ImageInput         bool          `json:"image_input"`
}

const (
	// ForTests
	ProviderMock ModelProvider = "__mock"
)

var SupportedModels = map[ModelID]Model{}

func init() {
	maps.Copy(SupportedModels, AnthropicModels)
	maps.Copy(SupportedModels, OpenAIModels)
	maps.Copy(SupportedModels, GeminiModels)
	maps.Copy(SupportedModels, BedrockModels)
	maps.Copy(SupportedModels, GroqModels)
}
