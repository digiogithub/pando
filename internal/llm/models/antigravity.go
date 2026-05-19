package models

const (
	AntigravityGemini30Pro   ModelID = "antigravity/gemini-3-pro"
	AntigravityGemini31Pro   ModelID = "antigravity/gemini-3.1-pro"
	AntigravityGemini30Flash ModelID = "antigravity/gemini-3-flash"
)

var antigravityModelAliases = map[string]ModelID{
	"antigravity.gemini-3-pro":   AntigravityGemini30Pro,
	"antigravity.gemini-3.1-pro": AntigravityGemini31Pro,
	"antigravity.gemini-3-flash": AntigravityGemini30Flash,
}

var AntigravityModels = map[ModelID]Model{
	AntigravityGemini30Pro: {
		ID:                  AntigravityGemini30Pro,
		Name:                "Gemini 3 Pro",
		Provider:            ProviderAntigravity,
		APIModel:            "gemini-3-pro-preview",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	AntigravityGemini31Pro: {
		ID:                  AntigravityGemini31Pro,
		Name:                "Gemini 3.1 Pro",
		Provider:            ProviderAntigravity,
		APIModel:            "gemini-3.1-pro-preview-customtools",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	AntigravityGemini30Flash: {
		ID:                  AntigravityGemini30Flash,
		Name:                "Gemini 3 Flash",
		Provider:            ProviderAntigravity,
		APIModel:            "gemini-3-flash-preview",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		SupportsAttachments: true,
	},
}
