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

// GoogleEmbedder generates embeddings using Google's Gemini embedding API.
// Supports text-embedding-004 and other Gemini embedding models.
type GoogleEmbedder struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client

	mu  sync.RWMutex
	dim int
}

// googleEmbeddingRequest is the request structure for Google embeddings API.
type googleEmbeddingRequest struct {
	Requests []googleEmbedRequest `json:"requests"`
}

type googleEmbedRequest struct {
	Model   string                 `json:"model"`
	Content googleEmbedContent     `json:"content"`
	TaskType string                `json:"taskType,omitempty"`
}

type googleEmbedContent struct {
	Parts []googleEmbedPart `json:"parts"`
}

type googleEmbedPart struct {
	Text string `json:"text"`
}

// googleEmbeddingResponse is the response structure from Google embeddings API.
type googleEmbeddingResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}

// googleErrorResponse represents an error from the Google API.
type googleErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// NewGoogleEmbedder creates a new Google/Gemini embedder.
// If baseURL is empty, it defaults to "https://generativelanguage.googleapis.com".
func NewGoogleEmbedder(apiKey, model, baseURL string) (*GoogleEmbedder, error) {
	if apiKey == "" {
		return nil, ErrEmptyAPIKey
	}
	if model == "" {
		model = "text-embedding-004"
	}
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}

	return &GoogleEmbedder{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{},
	}, nil
}

// EmbedDocuments generates embeddings for multiple documents.
// Google's batch endpoint supports multiple texts in one request.
func (e *GoogleEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrNoTexts
	}

	const maxBatchSize = 100
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := e.embedBatch(ctx, batch, "RETRIEVAL_DOCUMENT")
		if err != nil {
			return nil, fmt.Errorf("google: batch %d-%d: %w", i, end, err)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

// EmbedQuery generates an embedding for a single query.
func (e *GoogleEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := e.embedBatch(ctx, []string{text}, "RETRIEVAL_QUERY")
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("google: no embedding returned")
	}
	return embeddings[0], nil
}

// Dimension returns the dimensionality of embeddings.
func (e *GoogleEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dim
}

// embedBatch makes a single API call to embed a batch of texts.
func (e *GoogleEmbedder) embedBatch(ctx context.Context, texts []string, taskType string) ([][]float32, error) {
	requests := make([]googleEmbedRequest, len(texts))
	for i, text := range texts {
		requests[i] = googleEmbedRequest{
			Model: "models/" + e.model,
			Content: googleEmbedContent{
				Parts: []googleEmbedPart{{Text: text}},
			},
			TaskType: taskType,
		}
	}

	reqBody := googleEmbeddingRequest{
		Requests: requests,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("google: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:batchEmbedContents?key=%s", e.baseURL, e.model, e.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("google: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("google: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp googleErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("google: %s (code: %d)", errResp.Error.Message, errResp.Error.Code)
		}
		return nil, fmt.Errorf("google: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var embResp googleEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("google: unmarshal response: %w", err)
	}

	if len(embResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("google: expected %d embeddings, got %d", len(texts), len(embResp.Embeddings))
	}

	embeddings := make([][]float32, len(embResp.Embeddings))
	for i, emb := range embResp.Embeddings {
		embeddings[i] = emb.Values

		// Auto-detect dimension on first successful call
		if e.Dimension() == 0 && len(emb.Values) > 0 {
			e.mu.Lock()
			e.dim = len(emb.Values)
			e.mu.Unlock()
		}
	}

	return embeddings, nil
}
