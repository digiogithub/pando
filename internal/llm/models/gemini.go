package models

const (
	ProviderGemini ModelProvider = "gemini"

	// Model IDs match the dynamic format: "gemini.{api-id}"
	// Note: Gemini API returns preview-versioned IDs (e.g. "gemini-2.5-pro-preview-05-06")
	Gemini31ProPreview ModelID = "gemini.gemini-3.1-pro-preview-customtools"
	Gemini30Flash      ModelID = "gemini.gemini-3-flash"
	Gemini25Flash      ModelID = "gemini.gemini-2.5-flash-preview-04-17"
	Gemini25           ModelID = "gemini.gemini-2.5-pro-preview-05-06"
	Gemini20Flash      ModelID = "gemini.gemini-2.0-flash"
	Gemini20FlashLite  ModelID = "gemini.gemini-2.0-flash-lite"
)

var GeminiModels = map[ModelID]Model{
	Gemini31ProPreview: {
		ID:                  Gemini31ProPreview,
		Name:                "Gemini 3.1 Pro Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3.1-pro-preview-customtools",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini30Flash: {
		ID:                  Gemini30Flash,
		Name:                "Gemini 3 Flash",
		Provider:            ProviderGemini,
		APIModel:            "gemini-3-flash",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini25Flash: {
		ID:                  Gemini25Flash,
		Name:                "Gemini 2.5 Flash Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-flash-preview-04-17",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
	Gemini25: {
		ID:                  Gemini25,
		Name:                "Gemini 2.5 Pro Preview",
		Provider:            ProviderGemini,
		APIModel:            "gemini-2.5-pro-preview-05-06",
		ContextWindow:       1_000_000,
		DefaultMaxTokens:    8_192,
		CanReason:           true,
		SupportsAttachments: true,
	},
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
