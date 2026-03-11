package embeddings

import (
	"fmt"
	"strings"
)

// NewEmbedder creates an Embedder based on the provider name and configuration.
// Supported providers: "openai", "google", "gemini", "ollama", "anthropic", "voyage".
//
// Parameters:
//   - provider: The embedding provider name (case-insensitive)
//   - model: The model name/ID to use
//   - apiKey: API key (not needed for Ollama)
//   - baseURL: Base URL for the API (optional, uses defaults if empty)
//
// Returns an Embedder instance or an error if the provider is unsupported.
func NewEmbedder(provider, model, apiKey, baseURL string) (Embedder, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))

	switch provider {
	case "openai":
		return NewOpenAIEmbedder(apiKey, model, baseURL)

	case "google", "gemini":
		return NewGoogleEmbedder(apiKey, model, baseURL)

	case "ollama":
		return NewOllamaEmbedder(model, baseURL)

	case "anthropic", "voyage":
		return NewAnthropicEmbedder(apiKey, model, baseURL)

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

// ProviderDefaultModels maps provider names to their recommended default models.
var ProviderDefaultModels = map[string]string{
	"openai":    "text-embedding-3-small",
	"google":    "text-embedding-004",
	"gemini":    "text-embedding-004",
	"ollama":    "nomic-embed-text",
	"anthropic": "voyage-3",
	"voyage":    "voyage-3",
}

// GetDefaultModel returns the default model for a given provider.
// Returns empty string if the provider is unknown.
func GetDefaultModel(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return ProviderDefaultModels[provider]
}

// ProviderDimensions maps common models to their embedding dimensions.
// This is useful for pre-allocating storage or validation.
var ProviderDimensions = map[string]int{
	// OpenAI
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,

	// Google/Gemini
	"text-embedding-004": 768,

	// Ollama (common models)
	"nomic-embed-text":   768,
	"mxbai-embed-large":  1024,
	"all-minilm":         384,

	// Voyage (Anthropic)
	"voyage-3":        1024,
	"voyage-3-large":  1536,
	"voyage-code-3":   1024,
}

// GetModelDimension returns the expected dimension for a given model.
// Returns 0 if the model is not in the known list (dimension will be auto-detected).
func GetModelDimension(model string) int {
	model = strings.ToLower(strings.TrimSpace(model))
	return ProviderDimensions[model]
}
