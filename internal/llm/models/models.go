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
	APIModel           string        `json:"apiModel"`
	CostPer1MIn        float64       `json:"costPer1mIn"`
	CostPer1MOut       float64       `json:"costPer1mOut"`
	CostPer1MInCached  float64       `json:"constPer1mInCached"`
	CostPer1MOutCached float64       `json:"costPer1mOutCached"`
	ContextWindow      int64         `json:"contextWindow"`
	DefaultMaxTokens   int64         `json:"defaultMaxTokens"`
	CanReason          bool          `json:"canReason"`
	ImageInput         bool          `json:"imageInput"`
	Ref                string        `json:"ref"` // used when referencing a default model config
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
