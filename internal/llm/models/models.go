package models

import (
	"maps"
	"regexp"
)

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
	// SupportedEndpoints lists the API routes the provider accepts for this
	// model, as reported by its model-listing API (e.g. "/responses",
	// "/chat/completions"). Empty for statically-defined models and for
	// providers that do not report it.
	SupportedEndpoints []string `json:"supported_endpoints,omitempty"`
	// AccountID is the ProviderAccount.ID that this model belongs to.
	// Empty means the model comes from the legacy single-account system.
	AccountID string `json:"account_id,omitempty"`
	// Description, Knowledge (training cutoff, e.g. "2026-01") and ReleaseDate
	// are descriptive fields shown in the model selectors. They are usually
	// absent from provider listing APIs and come from the models.dev catalog.
	Description string `json:"description,omitempty"`
	Knowledge   string `json:"knowledge,omitempty"`
	ReleaseDate string `json:"release_date,omitempty"`
	// ImageMaxLongSidePx is the model's vision long-side pixel limit. Images
	// larger than this are rescaled by the provider before encoding, so Pando
	// resizes to this value locally to save bandwidth. 0 means "unknown": use
	// ImageLongSideLimit() which falls back to a per-family default.
	ImageMaxLongSidePx int `json:"image_max_long_side_px,omitempty"`
}

// highTierVisionRE matches the newest, highest-resolution vision models
// (Opus 4.8, Sonnet 5, Fable 5, Mythos 5), which accept a larger long side.
var highTierVisionRE = regexp.MustCompile(`(?i)(opus-4\.8|opus-4-8|sonnet-5|fable-5|mythos-5)`)

// ImageLongSideLimit returns the long-side pixel cap to resize images to before
// sending them to this model. Prefers the explicit ImageMaxLongSidePx (e.g. from
// the models.dev catalog), otherwise infers a per-family default.
func (m Model) ImageLongSideLimit() int {
	if m.ImageMaxLongSidePx > 0 {
		return m.ImageMaxLongSidePx
	}
	if highTierVisionRE.MatchString(string(m.ID)) || highTierVisionRE.MatchString(m.APIModel) {
		return 2576
	}
	return 1568
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
	ProviderCopilot:          1,
	ProviderAnthropic:        2,
	ProviderOpenAI:           3,
	ProviderAntigravity:      4,
	ProviderOllama:           5,
	ProviderLlamaCpp:         6,
	ProviderGemini:           7,
	ProviderGROQ:             8,
	ProviderOpenRouter:       9,
	ProviderBedrock:          10,
	ProviderAzure:            11,
	ProviderVertexAI:         12,
	ProviderOpenAICompatible: 13,
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
	// Antigravity exposes a small curated logical catalog so users choose stable provider models
	// without binding selections to a specific OAuth account.
	// All other providers populate models dynamically via RefreshProviderModels.
	maps.Copy(SupportedModels, AzureModels)
	maps.Copy(SupportedModels, VertexAIGeminiModels)
	maps.Copy(SupportedModels, GeminiModels)
	maps.Copy(SupportedModels, AntigravityModels)
	maps.Copy(SupportedModels, AnthropicModels)
}
