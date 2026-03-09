package models

const (
	ProviderOpenRouter ModelProvider = "openrouter"

	// Model IDs match the dynamic format: "openrouter.{api-id}"
	// OpenRouter API returns IDs in "provider/model" format
	OpenRouterClaude37Sonnet ModelID = "openrouter.anthropic/claude-3.7-sonnet"
	OpenRouterClaude35Haiku  ModelID = "openrouter.anthropic/claude-3.5-haiku"
	OpenRouterGPT41          ModelID = "openrouter.openai/gpt-4.1"
	OpenRouterDeepSeekR1Free ModelID = "openrouter.deepseek/deepseek-r1-0528:free"
)
