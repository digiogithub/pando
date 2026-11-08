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

// OpenAIEmbedder generates embeddings using OpenAI's embedding API.
// Supports text-embedding-3-small, text-embedding-3-large, and ada-002.
type OpenAIEmbedder struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client

	mu  sync.RWMutex
	dim int
}

// openaiEmbeddingRequest is the request structure for OpenAI embeddings API.
type openaiEmbeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// openaiEmbeddingResponse is the response structure from OpenAI embeddings API.
type openaiEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// openaiErrorResponse represents an error from the OpenAI API.
type openaiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// NewOpenAIEmbedder creates a new OpenAI embedder.
// If baseURL is empty, it defaults to "https://api.openai.com".
func NewOpenAIEmbedder(apiKey, model, baseURL string) (*OpenAIEmbedder, error) {
	if apiKey == "" {
		return nil, ErrEmptyAPIKey
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	return &OpenAIEmbedder{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{},
	}, nil
}

// EmbedDocuments generates embeddings for multiple documents.
// OpenAI supports up to 2048 inputs per request, so we batch if needed.
func (e *OpenAIEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrNoTexts
	}

	const maxBatchSize = 2048
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("openai: batch %d-%d: %w", i, end, err)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

// EmbedQuery generates an embedding for a single query.
func (e *OpenAIEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := e.embedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("openai: no embedding returned")
	}
	return embeddings[0], nil
}

// Dimension returns the dimensionality of embeddings.
func (e *OpenAIEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dim
}

// embedBatch makes a single API call to embed a batch of texts.
func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := openaiEmbeddingRequest{
		Input: texts,
		Model: e.model,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	url := e.baseURL + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp openaiErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("openai: %s (type: %s)", errResp.Error.Message, errResp.Error.Type)
		}
		return nil, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var embResp openaiEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("openai: unmarshal response: %w", err)
	}

	if len(embResp.Data) != len(texts) {
		return nil, fmt.Errorf("openai: expected %d embeddings, got %d", len(texts), len(embResp.Data))
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
