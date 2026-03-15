package models

const (
	ProviderAnthropic ModelProvider = "anthropic"

	// Claude 3 family
	Claude3Haiku   ModelID = "anthropic.claude-3-haiku-20240307"
	Claude3Opus    ModelID = "anthropic.claude-3-opus-20240229"
	Claude35Sonnet ModelID = "anthropic.claude-3-5-sonnet-20241022"
	Claude35Haiku  ModelID = "anthropic.claude-3-5-haiku-20241022"
	Claude37Sonnet ModelID = "anthropic.claude-3-7-sonnet-20250219"

	// Claude 4 family
	Claude4Sonnet  ModelID = "anthropic.claude-sonnet-4-20250514"
	Claude4Opus    ModelID = "anthropic.claude-opus-4-20250514"
	Claude4Opus1   ModelID = "anthropic.claude-opus-4-1-20250805"
	Claude45Sonnet ModelID = "anthropic.claude-sonnet-4-5-20250929"
	Claude45Haiku  ModelID = "anthropic.claude-haiku-4-5-20251001"
	Claude45Opus   ModelID = "anthropic.claude-opus-4-5-20251101"
	Claude46Sonnet ModelID = "anthropic.claude-sonnet-4-6"
	Claude46Opus   ModelID = "anthropic.claude-opus-4-6"
)

// AnthropicModels lists all supported Anthropic models with their full capabilities.
// Capabilities are sourced from the Claude Code CLI source (oa() and uM() functions).
// Context window: 200 000 tokens for all current models.
// Max output tokens by family (default / upper limit from oa()):
//   - Claude 3 Haiku:    4 096 / 4 096
//   - Claude 3 Opus:     4 096 / 4 096
//   - Claude 3.5 Sonnet / 3.5 Haiku: 8 192 / 8 192
//   - Claude 3.7 Sonnet: 32 000 / 64 000
//   - Claude 4 Sonnet / Haiku / Opus (all 4.x): 32 000 / 64 000
var AnthropicModels = map[ModelID]Model{
	// ── Claude 3 ──────────────────────────────────────────────────────────
	Claude3Haiku: {
		ID:                  Claude3Haiku,
		Name:                "Claude 3 Haiku",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-3-haiku-20240307",
		CostPer1MIn:         0.25,
		CostPer1MInCached:   0.03,
		CostPer1MOut:        1.25,
		ContextWindow:       200_000,
		DefaultMaxTokens:    4_096,
		SupportsAttachments: true,
	},
	Claude3Opus: {
		ID:                  Claude3Opus,
		Name:                "Claude 3 Opus",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-3-opus-20240229",
		CostPer1MIn:         15.0,
		CostPer1MInCached:   1.50,
		CostPer1MOut:        75.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    4_096,
		SupportsAttachments: true,
	},
	Claude35Sonnet: {
		ID:                  Claude35Sonnet,
		Name:                "Claude 3.5 Sonnet",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-3-5-sonnet-20241022",
		CostPer1MIn:         3.0,
		CostPer1MInCached:   0.30,
		CostPer1MOut:        15.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    8_192,
		SupportsAttachments: true,
	},
	Claude35Haiku: {
		ID:                  Claude35Haiku,
		Name:                "Claude 3.5 Haiku",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-3-5-haiku-20241022",
		CostPer1MIn:         0.80,
		CostPer1MInCached:   0.08,
		CostPer1MOut:        4.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    8_192,
		SupportsAttachments: true,
	},
	Claude37Sonnet: {
		ID:                  Claude37Sonnet,
		Name:                "Claude 3.7 Sonnet",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-3-7-sonnet-20250219",
		CostPer1MIn:         3.0,
		CostPer1MInCached:   0.30,
		CostPer1MOut:        15.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		CanReason:           true,
		SupportsAttachments: true,
	},

	// ── Claude 4 ──────────────────────────────────────────────────────────
	Claude4Sonnet: {
		ID:                  Claude4Sonnet,
		Name:                "Claude Sonnet 4",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-sonnet-4-20250514",
		CostPer1MIn:         3.0,
		CostPer1MInCached:   0.30,
		CostPer1MOut:        15.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Claude4Opus: {
		ID:                  Claude4Opus,
		Name:                "Claude Opus 4",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-opus-4-20250514",
		CostPer1MIn:         15.0,
		CostPer1MInCached:   1.50,
		CostPer1MOut:        75.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Claude4Opus1: {
		ID:                  Claude4Opus1,
		Name:                "Claude Opus 4.1",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-opus-4-1-20250805",
		CostPer1MIn:         15.0,
		CostPer1MInCached:   1.50,
		CostPer1MOut:        75.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Claude45Sonnet: {
		ID:                  Claude45Sonnet,
		Name:                "Claude Sonnet 4.5",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-sonnet-4-5-20250929",
		CostPer1MIn:         3.0,
		CostPer1MInCached:   0.30,
		CostPer1MOut:        15.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Claude45Haiku: {
		ID:                  Claude45Haiku,
		Name:                "Claude Haiku 4.5",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-haiku-4-5-20251001",
		CostPer1MIn:         0.80,
		CostPer1MInCached:   0.08,
		CostPer1MOut:        4.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		SupportsAttachments: true,
	},
	Claude45Opus: {
		ID:                  Claude45Opus,
		Name:                "Claude Opus 4.5",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-opus-4-5-20251101",
		CostPer1MIn:         15.0,
		CostPer1MInCached:   1.50,
		CostPer1MOut:        75.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Claude46Sonnet: {
		ID:                  Claude46Sonnet,
		Name:                "Claude Sonnet 4.6",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-sonnet-4-6",
		CostPer1MIn:         3.0,
		CostPer1MInCached:   0.30,
		CostPer1MOut:        15.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Claude46Opus: {
		ID:                  Claude46Opus,
		Name:                "Claude Opus 4.6",
		Provider:            ProviderAnthropic,
		APIModel:            "claude-opus-4-6",
		CostPer1MIn:         15.0,
		CostPer1MInCached:   1.50,
		CostPer1MOut:        75.0,
		ContextWindow:       200_000,
		DefaultMaxTokens:    32_000,
		CanReason:           true,
		SupportsAttachments: true,
	},
}
