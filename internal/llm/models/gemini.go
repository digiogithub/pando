package models

const (
	ProviderGemini ModelProvider = "gemini"

	// Model IDs match the dynamic format: "gemini.{api-id}"
	// Note: Gemini API returns preview-versioned IDs (e.g. "gemini-2.5-pro-preview-05-06")
	Gemini25Flash     ModelID = "gemini.gemini-2.5-flash-preview-04-17"
	Gemini25          ModelID = "gemini.gemini-2.5-pro-preview-05-06"
	Gemini20Flash     ModelID = "gemini.gemini-2.0-flash"
	Gemini20FlashLite ModelID = "gemini.gemini-2.0-flash-lite"
)
