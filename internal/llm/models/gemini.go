package models

const (
	ProviderGemini ModelProvider = "gemini"

	// Gemini 3 preview models (require preview access)
	Gemini31ProPreview           ModelID = "gemini.gemini-3.1-pro-preview-customtools"
	Gemini31ProPreviewBase       ModelID = "gemini.gemini-3.1-pro-preview"
	Gemini31FlashLitePreview     ModelID = "gemini.gemini-3.1-flash-lite-preview"
	Gemini30ProPreview           ModelID = "gemini.gemini-3-pro-preview"
	Gemini30Flash                ModelID = "gemini.gemini-3-flash-preview"
	// Gemini30Flash legacy alias – config files using "gemini.gemini-3-flash" still resolve.
	Gemini30FlashLegacy ModelID = "gemini.gemini-3-flash"

	// Gemini 2.5 stable models
	Gemini25          ModelID = "gemini.gemini-2.5-pro"
	Gemini25Flash     ModelID = "gemini.gemini-2.5-flash"
	Gemini25FlashLite ModelID = "gemini.gemini-2.5-flash-lite"

	// Gemini 2.5 preview aliases – config files using old preview IDs still resolve.
	Gemini25LegacyPreview      ModelID = "gemini.gemini-2.5-pro-preview-05-06"
	Gemini25FlashLegacyPreview ModelID = "gemini.gemini-2.5-flash-preview-04-17"

	// Gemini 2.0 stable models
	Gemini20Flash     ModelID = "gemini.gemini-2.0-flash"
	Gemini20FlashLite ModelID = "gemini.gemini-2.0-flash-lite"
)

var GeminiModels = map[ModelID]Model{
	// --- Gemini 3 preview models ---
	Gemini31ProPreview: {
		ID:                  Gemini31ProPreview,
		Name:                "Gemini 3.1 Pro Preview (Custom Tools)",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3.1-pro-preview-customtools",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini31ProPreviewBase: {
		ID:                  Gemini31ProPreviewBase,
		Name:                "Gemini 3.1 Pro Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3.1-pro-preview",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini31FlashLitePreview: {
		ID:                  Gemini31FlashLitePreview,
		Name:                "Gemini 3.1 Flash Lite Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3.1-flash-lite-preview",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini30ProPreview: {
		ID:                  Gemini30ProPreview,
		Name:                "Gemini 3 Pro Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3-pro-preview",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini30Flash: {
		ID:                  Gemini30Flash,
		Name:                "Gemini 3 Flash Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3-flash-preview",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	// Legacy alias: maps old model ID to current API name.
	Gemini30FlashLegacy: {
		ID:                  Gemini30FlashLegacy,
		Name:                "Gemini 3 Flash Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3-flash-preview",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},

	// --- Gemini 2.5 stable models ---
	Gemini25: {
		ID:                  Gemini25,
		Name:                "Gemini 2.5 Pro",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-pro",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini25Flash: {
		ID:                  Gemini25Flash,
		Name:                "Gemini 2.5 Flash",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-flash",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini25FlashLite: {
		ID:                  Gemini25FlashLite,
		Name:                "Gemini 2.5 Flash Lite",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-flash-lite",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		SupportsAttachments: true,
	},
	// Legacy preview aliases: map old IDs to current stable API names.
	Gemini25LegacyPreview: {
		ID:                  Gemini25LegacyPreview,
		Name:                "Gemini 2.5 Pro",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-pro",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini25FlashLegacyPreview: {
		ID:                  Gemini25FlashLegacyPreview,
		Name:                "Gemini 2.5 Flash",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-flash",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},

	// --- Gemini 2.0 stable models ---
	Gemini20Flash: {
		ID:                  Gemini20Flash,
		Name:                "Gemini 2.0 Flash",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.0-flash",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		SupportsAttachments: true,
	},
	Gemini20FlashLite: {
		ID:                  Gemini20FlashLite,
		Name:                "Gemini 2.0 Flash Lite",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.0-flash-lite",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		SupportsAttachments: true,
	},
}
