// Package embeddings provides a unified interface for generating embeddings
// from multiple providers (OpenAI, Google, Ollama, Anthropic/Voyage).
package embeddings

import (
	"context"
	"fmt"
)

// Embedder is the main interface for generating embeddings from text.
// All provider implementations must be thread-safe.
type Embedder interface {
	// EmbedDocuments generates embeddings for multiple documents.
	// Implementations should batch requests according to provider limits.
	// Partial failures are allowed: continue processing remaining documents.
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)

	// EmbedQuery generates an embedding for a single query text.
	// This is typically the same as EmbedDocuments but may use different
	// parameters in some providers (e.g. search_query vs search_document).
	EmbedQuery(ctx context.Context, text string) ([]float32, error)

	// Dimension returns the dimensionality of the embeddings.
	// This is auto-detected on the first successful embedding call.
	Dimension() int
}

// ErrUnsupportedProvider is returned when the provider name is not recognized.
var ErrUnsupportedProvider = fmt.Errorf("unsupported embedding provider")

// ErrEmptyAPIKey is returned when an API key is required but not provided.
var ErrEmptyAPIKey = fmt.Errorf("API key is required but not provided")

// ErrNoTexts is returned when an empty text slice is passed to EmbedDocuments.
var ErrNoTexts = fmt.Errorf("no texts provided for embedding")

// ErrDimensionMismatch is returned when embeddings have unexpected dimensions.
var ErrDimensionMismatch = fmt.Errorf("embedding dimension mismatch")
