package models

const (
	ProviderXAI ModelProvider = "xai"

	// Model IDs match the dynamic format: "xai.{api-id}"
	XAIGrok3Beta         ModelID = "xai.grok-3-beta"
	XAIGrok3MiniBeta     ModelID = "xai.grok-3-mini-beta"
	XAIGrok3FastBeta     ModelID = "xai.grok-3-fast-beta"
	XAiGrok3MiniFastBeta ModelID = "xai.grok-3-mini-fast-beta"
)
