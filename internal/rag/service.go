// Package rag provides the RemembrancesService that bundles KB, Events, and Code indexing.
package rag

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/rag/events"
	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/internal/rag/sessions"
)

// RemembrancesService groups KB, Events, and Code indexing stores.
// All components share the same SQLite database connection and use
// provider-configured embedders.
type RemembrancesService struct {
	KB           *kb.KBStore
	Events       *events.EventStore
	Code         *code.CodeIndexer
	Sessions     *sessions.SessionRAGStore
	SessionIdx   *sessions.SessionIndexer // may be nil until SetSessionIndexer is called
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

	sessionStore := sessions.NewSessionRAGStore(db, docEmbedder, cfg.ChunkSize, cfg.ChunkOverlap)
	if err := sessionStore.InitTables(context.Background()); err != nil {
		return nil, fmt.Errorf("remembrances: init session RAG tables: %w", err)
	}

	return &RemembrancesService{
		KB:           kbStore,
		Events:       eventStore,
		Code:         codeIndexer,
		Sessions:     sessionStore,
		docEmbedder:  docEmbedder,
		codeEmbedder: codeEmbedder,
	}, nil
}

// NewUnifiedSearcher creates a UnifiedSearcher backed by this service's KB and Sessions.
func (r *RemembrancesService) NewUnifiedSearcher() *UnifiedSearcher {
	if r.Sessions == nil {
		return NewUnifiedSearcher(r.KB, nil)
	}
	return NewUnifiedSearcher(r.KB, r.Sessions)
}

// SetSessionIndexer attaches an optional SessionIndexer.
// It is configured externally because it requires message and session services
// that are not available during NewRemembrancesService construction.
func (r *RemembrancesService) SetSessionIndexer(idx *sessions.SessionIndexer) {
	r.SessionIdx = idx
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
