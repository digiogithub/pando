// Package rag provides the RemembrancesService that bundles KB, Events, and Code indexing.
package rag

import (
	"database/sql"
	"fmt"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/rag/events"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// RemembrancesService groups KB, Events, and Code indexing stores.
// All components share the same SQLite database connection and use
// provider-configured embedders.
type RemembrancesService struct {
	KB           *kb.KBStore
	Events       *events.EventStore
	Code         *code.CodeIndexer
	docEmbedder  embeddings.Embedder
	codeEmbedder embeddings.Embedder
}

// NewRemembrancesService creates a RemembrancesService from the app configuration and an
// existing SQLite connection. Returns nil (no error) when remembrances is disabled.
func NewRemembrancesService(db *sql.DB, cfg *config.RemembrancesConfig) (*RemembrancesService, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	// Resolve API key and base URL for the document embedding provider.
	docAPIKey, docBaseURL := resolveEmbedderCredentials(cfg.DocumentEmbeddingProvider, cfg.DocumentEmbeddingAPIKey, cfg.DocumentEmbeddingBaseURL)
	docEmbedder, err := embeddings.NewEmbedder(
		cfg.DocumentEmbeddingProvider,
		cfg.DocumentEmbeddingModel,
		docAPIKey,
		docBaseURL,
	)
	if err != nil {
		return nil, fmt.Errorf("remembrances: create document embedder (%s/%s): %w",
			cfg.DocumentEmbeddingProvider, cfg.DocumentEmbeddingModel, err)
	}

	// Resolve API key and base URL for the code embedding provider.
	var codeEmbedder embeddings.Embedder
	if cfg.CodeEmbeddingProvider == cfg.DocumentEmbeddingProvider &&
		cfg.CodeEmbeddingModel == cfg.DocumentEmbeddingModel &&
		cfg.CodeEmbeddingBaseURL == cfg.DocumentEmbeddingBaseURL {
		codeEmbedder = docEmbedder
	} else {
		codeAPIKey, codeBaseURL := resolveEmbedderCredentials(cfg.CodeEmbeddingProvider, cfg.CodeEmbeddingAPIKey, cfg.CodeEmbeddingBaseURL)
		codeEmbedder, err = embeddings.NewEmbedder(
			cfg.CodeEmbeddingProvider,
			cfg.CodeEmbeddingModel,
			codeAPIKey,
			codeBaseURL,
		)
		if err != nil {
			return nil, fmt.Errorf("remembrances: create code embedder (%s/%s): %w",
				cfg.CodeEmbeddingProvider, cfg.CodeEmbeddingModel, err)
		}
	}

	workers := cfg.IndexWorkers
	kbStore := kb.NewKBStore(db, docEmbedder, cfg.ChunkSize, cfg.ChunkOverlap)
	kbStore.SetSyncWorkers(workers)
	eventStore := events.NewEventStore(db, docEmbedder)
	codeIndexer := code.NewCodeIndexer(db, codeEmbedder, workers)

	return &RemembrancesService{
		KB:           kbStore,
		Events:       eventStore,
		Code:         codeIndexer,
		docEmbedder:  docEmbedder,
		codeEmbedder: codeEmbedder,
	}, nil
}

// resolveEmbedderCredentials returns the API key and base URL for an embedding provider.
// For "openai-compatible", it uses the explicitly supplied customAPIKey and customBaseURL.
// For all other providers it falls back to the global Providers config.
func resolveEmbedderCredentials(provider, customAPIKey, customBaseURL string) (apiKey, baseURL string) {
	if provider == "openai-compatible" {
		return customAPIKey, customBaseURL
	}
	return resolveProviderCredentials(provider)
}

// resolveProviderCredentials looks up the API key and base URL for a provider from the current app config.
func resolveProviderCredentials(provider string) (apiKey, baseURL string) {
	cfg := config.Get()
	if cfg == nil {
		return "", ""
	}
	// Map embedding provider names to LLM provider names used in pando config.
	switch provider {
	case "openai":
		if p, ok := cfg.Providers[models.ProviderOpenAI]; ok {
			return p.APIKey, p.BaseURL
		}
	case "google", "gemini":
		if p, ok := cfg.Providers[models.ProviderGemini]; ok {
			return p.APIKey, p.BaseURL
		}
	case "anthropic", "voyage":
		if p, ok := cfg.Providers[models.ProviderAnthropic]; ok {
			return p.APIKey, p.BaseURL
		}
	case "ollama":
		if p, ok := cfg.Providers[models.ProviderOllama]; ok {
			return "", models.ResolveOllamaRawBaseURL(p.BaseURL)
		}
		return "", models.ResolveOllamaRawBaseURL("")
	}
	return "", ""
}
