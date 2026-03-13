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

	// Resolve API key for the document embedding provider.
	docAPIKey := resolveAPIKey(cfg.DocumentEmbeddingProvider)
	docEmbedder, err := embeddings.NewEmbedder(
		cfg.DocumentEmbeddingProvider,
		cfg.DocumentEmbeddingModel,
		docAPIKey,
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("remembrances: create document embedder (%s/%s): %w",
			cfg.DocumentEmbeddingProvider, cfg.DocumentEmbeddingModel, err)
	}

	// Resolve API key for the code embedding provider.
	var codeEmbedder embeddings.Embedder
	if cfg.CodeEmbeddingProvider == cfg.DocumentEmbeddingProvider &&
		cfg.CodeEmbeddingModel == cfg.DocumentEmbeddingModel {
		codeEmbedder = docEmbedder
	} else {
		codeAPIKey := resolveAPIKey(cfg.CodeEmbeddingProvider)
		codeEmbedder, err = embeddings.NewEmbedder(
			cfg.CodeEmbeddingProvider,
			cfg.CodeEmbeddingModel,
			codeAPIKey,
			"",
		)
		if err != nil {
			return nil, fmt.Errorf("remembrances: create code embedder (%s/%s): %w",
				cfg.CodeEmbeddingProvider, cfg.CodeEmbeddingModel, err)
		}
	}

	kbStore := kb.NewKBStore(db, docEmbedder, cfg.ChunkSize, cfg.ChunkOverlap)
	eventStore := events.NewEventStore(db, docEmbedder)
	codeIndexer := code.NewCodeIndexer(db, codeEmbedder)

	return &RemembrancesService{
		KB:           kbStore,
		Events:       eventStore,
		Code:         codeIndexer,
		docEmbedder:  docEmbedder,
		codeEmbedder: codeEmbedder,
	}, nil
}

// resolveAPIKey looks up the API key for a provider from the current app config.
func resolveAPIKey(provider string) string {
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	// Map embedding provider names to LLM provider names used in pando config.
	switch provider {
	case "openai":
		if p, ok := cfg.Providers[models.ProviderOpenAI]; ok {
			return p.APIKey
		}
	case "google", "gemini":
		if p, ok := cfg.Providers[models.ProviderGemini]; ok {
			return p.APIKey
		}
	case "anthropic", "voyage":
		if p, ok := cfg.Providers[models.ProviderAnthropic]; ok {
			return p.APIKey
		}
	case "ollama":
		// Ollama is local, no API key required.
	}
	return ""
}
