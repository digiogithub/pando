package models

const (
	ProviderVertexAI ModelProvider = "vertexai"

	// Models
	VertexAIGemini25Flash ModelID = "vertexai.gemini-2.5-flash"
	VertexAIGemini25      ModelID = "vertexai.gemini-2.5"
)

var VertexAIGeminiModels = map[ModelID]Model{
	VertexAIGemini25Flash: {
		ID:                  VertexAIGemini25Flash,
		Name:                "VertexAI: Gemini 2.5 Flash",
		Provider:            ProviderVertexAI,
		APIModel:            "gemini-2.5-flash-preview-04-17",
		CostPer1MIn:         0.15,
		CostPer1MInCached:   0,
		CostPer1MOut:        0.60,
		CostPer1MOutCached:  0,
		ContextWindow:       1000000,
		DefaultMaxTokens:    50000,
		SupportsAttachments: true,
	},
	VertexAIGemini25: {
		ID:                  VertexAIGemini25,
		Name:                "VertexAI: Gemini 2.5 Pro",
		Provider:            ProviderVertexAI,
		APIModel:            "gemini-2.5-pro-preview-03-25",
		CostPer1MIn:         1.25,
		CostPer1MInCached:   0,
		CostPer1MOut:        10,
		CostPer1MOutCached:  0,
		ContextWindow:       1000000,
		DefaultMaxTokens:    50000,
		SupportsAttachments: true,
	},
}
