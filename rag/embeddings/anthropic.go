package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// AnthropicEmbedder generates embeddings using Voyage AI's API.
// Anthropic partners with Voyage for embeddings (voyage-3, voyage-3-large, voyage-code-3).
type AnthropicEmbedder struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client

	mu  sync.RWMutex
	dim int
}

// voyageEmbeddingRequest is the request structure for Voyage AI embeddings API.
type voyageEmbeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// voyageEmbeddingResponse is the response structure from Voyage AI embeddings API.
type voyageEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// voyageErrorResponse represents an error from the Voyage AI API.
type voyageErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// NewAnthropicEmbedder creates a new Anthropic/Voyage embedder.
// If baseURL is empty, it defaults to "https://api.voyageai.com".
func NewAnthropicEmbedder(apiKey, model, baseURL string) (*AnthropicEmbedder, error) {
	if apiKey == "" {
		return nil, ErrEmptyAPIKey
	}
	if model == "" {
		model = "voyage-3"
	}
	if baseURL == "" {
		baseURL = "https://api.voyageai.com"
	}

	return &AnthropicEmbedder{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{},
	}, nil
}

// EmbedDocuments generates embeddings for multiple documents.
// Voyage supports up to 128 inputs per request.
func (e *AnthropicEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrNoTexts
	}

	const maxBatchSize = 128
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("voyage: batch %d-%d: %w", i, end, err)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

// EmbedQuery generates an embedding for a single query.
func (e *AnthropicEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := e.embedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("voyage: no embedding returned")
	}
	return embeddings[0], nil
}

// Dimension returns the dimensionality of embeddings.
func (e *AnthropicEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dim
}

// embedBatch makes a single API call to embed a batch of texts.
func (e *AnthropicEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := voyageEmbeddingRequest{
		Input: texts,
		Model: e.model,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("voyage: marshal request: %w", err)
	}

	url := e.baseURL + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("voyage: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voyage: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("voyage: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp voyageErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("voyage: %s (type: %s)", errResp.Error.Message, errResp.Error.Type)
		}
		return nil, fmt.Errorf("voyage: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var embResp voyageEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("voyage: unmarshal response: %w", err)
	}

	if len(embResp.Data) != len(texts) {
		return nil, fmt.Errorf("voyage: expected %d embeddings, got %d", len(texts), len(embResp.Data))
	}

	// Convert float64 to float32 and detect dimension
	embeddings := make([][]float32, len(embResp.Data))
	for _, item := range embResp.Data {
		emb := make([]float32, len(item.Embedding))
		for j, v := range item.Embedding {
			emb[j] = float32(v)
		}
		embeddings[item.Index] = emb

		// Auto-detect dimension on first successful call
		if e.Dimension() == 0 && len(emb) > 0 {
			e.mu.Lock()
			e.dim = len(emb)
			e.mu.Unlock()
		}
	}

	return embeddings, nil
}
