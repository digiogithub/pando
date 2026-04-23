package models

import "maps"

type (
	ModelID       string
	ModelProvider string
)

type Model struct {
	ID                      ModelID       `json:"id"`
	Name                    string        `json:"name"`
	Provider                ModelProvider `json:"provider"`
	APIModel                string        `json:"api_model"`
	CostPer1MIn             float64       `json:"cost_per_1m_in"`
	CostPer1MOut            float64       `json:"cost_per_1m_out"`
	CostPer1MInCached       float64       `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached      float64       `json:"cost_per_1m_out_cached"`
	ContextWindow           int64         `json:"context_window"`
	DefaultMaxTokens        int64         `json:"default_max_tokens"`
	CanReason               bool          `json:"can_reason"`
	SupportsReasoningEffort bool          `json:"supports_reasoning_effort"`
	SupportsAttachments     bool          `json:"supports_attachments"`
	// AccountID is the ProviderAccount.ID that this model belongs to.
	// Empty means the model comes from the legacy single-account system.
	AccountID string `json:"account_id,omitempty"`
}

// DisplayLabel returns the display label for a model.
// sameTypeAccountCount is how many non-disabled accounts share this model's provider type.
// If > 1, the account ID is prefixed to disambiguate.
func (m Model) DisplayLabel(sameTypeAccountCount int) string {
	if m.AccountID == "" || sameTypeAccountCount <= 1 {
		return m.Name
	}
	return m.AccountID + ": " + m.Name
}

// Model IDs
const ( // GEMINI
	// Bedrock
	BedrockClaude37Sonnet ModelID = "bedrock.claude-3.7-sonnet"
)

const (
	ProviderBedrock ModelProvider = "bedrock"
	// ForTests
	ProviderMock ModelProvider = "__mock"
)

// Providers in order of popularity
var ProviderPopularity = map[ModelProvider]int{
	ProviderCopilot:    1,
	ProviderAnthropic:  2,
	ProviderOpenAI:     3,
	ProviderOllama:     4,
	ProviderGemini:     5,
	ProviderGROQ:       6,
	ProviderOpenRouter: 7,
	ProviderBedrock:          8,
	ProviderAzure:            9,
	ProviderVertexAI:         10,
	ProviderOpenAICompatible: 11,
}

var SupportedModels = map[ModelID]Model{
	// Bedrock (static, no simple model listing endpoint)
	BedrockClaude37Sonnet: {
		ID:                 BedrockClaude37Sonnet,
		Name:               "Bedrock: Claude 3.7 Sonnet",
		Provider:           ProviderBedrock,
		APIModel:           "anthropic.claude-3-7-sonnet-20250219-v1:0",
		CostPer1MIn:        3.0,
		CostPer1MInCached:  3.75,
		CostPer1MOutCached: 0.30,
		CostPer1MOut:       15.0,
	},
}

func init() {
	// Azure and VertexAI are kept static because they don't expose a simple model listing endpoint.
	// Anthropic models are hardcoded (same approach as Claude Code CLI) since the model list is static.
	// Gemini includes a curated static baseline, and dynamic discovery augments provider coverage.
	// All other providers populate models dynamically via RefreshProviderModels.
	maps.Copy(SupportedModels, AzureModels)
	maps.Copy(SupportedModels, VertexAIGeminiModels)
	maps.Copy(SupportedModels, GeminiModels)
	maps.Copy(SupportedModels, AnthropicModels)
}
