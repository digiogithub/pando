package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OllamaEmbedder generates embeddings using a local Ollama instance.
// Supports models like nomic-embed-text, mxbai-embed-large, etc.
type OllamaEmbedder struct {
	model   string
	baseURL string
	client  *http.Client

	mu  sync.RWMutex
	dim int
}

// ollamaEmbeddingRequest is the request structure for Ollama embeddings API.
type ollamaEmbeddingRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

// ollamaEmbeddingResponse is the response structure from Ollama embeddings API.
type ollamaEmbeddingResponse struct {
	Embedding  []float64   `json:"embedding"`
	Embeddings [][]float64 `json:"embeddings"`
}

// NewOllamaEmbedder creates a new Ollama embedder.
// If baseURL is empty, it defaults to "http://localhost:11434".
func NewOllamaEmbedder(model, baseURL string) (*OllamaEmbedder, error) {
	if model == "" {
		model = "nomic-embed-text"
	}
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	baseURL = normalizeOllamaNativeBaseURL(baseURL)

	return &OllamaEmbedder{
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 90 * time.Second},
	}, nil
}

func normalizeOllamaNativeBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "http://localhost:11434"
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}

	switch strings.TrimRight(parsed.Path, "/") {
	case "", "/", "/v1", "/api":
		parsed.Path = ""
	}

	return strings.TrimRight(parsed.String(), "/")
}

// ollamaHTTPError is a non-200 answer from Ollama. Keeping the status lets the
// embedder tell "endpoint not supported" (fall back to the legacy API) from
// "server overloaded" (stop).
type ollamaHTTPError struct {
	StatusCode int
	Body       string
}

func (e *ollamaHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// Unwrap marks 503 answers (Ollama's "server busy" when its queue is full)
// as ErrBackendUnavailable.
func (e *ollamaHTTPError) Unwrap() error {
	if e.StatusCode == http.StatusServiceUnavailable {
		return ErrBackendUnavailable
	}
	return nil
}

// isEndpointUnsupported reports whether err means the endpoint does not exist
// on this Ollama version, the only case where the legacy endpoint can help.
func isEndpointUnsupported(err error) bool {
	var httpErr *ollamaHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	switch httpErr.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	return false
}

// shouldStop reports whether a failed request must not be followed by more
// requests: the caller's context is done or the backend is unavailable. Every
// retry would queue more work on an Ollama that is already saturated, and
// Ollama keeps computing requests the client has abandoned.
func shouldStop(ctx context.Context, err error) bool {
	return ctx.Err() != nil || IsBackendUnavailable(err)
}

// EmbedDocuments generates embeddings for multiple documents with one batch
// call to /api/embed. It falls back to one call per text only when the batch
// fails for a reason other than an unavailable backend (for example an old
// Ollama without batch input support).
func (e *OllamaEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrNoTexts
	}

	batch, batchErr := e.requestEmbedBatch(ctx, texts)
	if batchErr == nil {
		return batch, nil
	}
	if shouldStop(ctx, batchErr) {
		return nil, batchErr
	}

	embeddings := make([][]float32, len(texts))
	var firstErr error

	for i, text := range texts {
		emb, err := e.embedSingle(ctx, text)
		if err != nil {
			if shouldStop(ctx, err) {
				return nil, fmt.Errorf("ollama: text %d: %w", i, err)
			}
			// Continue on individual failures but record the first error
			if firstErr == nil {
				firstErr = fmt.Errorf("ollama: text %d: %w", i, err)
			}
			// Use zero vector for failed embeddings if we know the dimension
			if e.Dimension() > 0 {
				embeddings[i] = make([]float32, e.Dimension())
			}
			continue
		}
		embeddings[i] = emb
	}

	// If all embeddings failed, return the first error
	if firstErr != nil {
		allFailed := true
		for _, emb := range embeddings {
			if len(emb) > 0 {
				allFailed = false
				break
			}
		}
		if allFailed {
			return nil, firstErr
		}
	}

	return embeddings, nil
}

// EmbedQuery generates an embedding for a single query.
func (e *OllamaEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return e.embedSingle(ctx, text)
}

// Dimension returns the dimensionality of embeddings.
func (e *OllamaEmbedder) Dimension() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dim
}

// embedSingle makes a single API call to embed one text.
func (e *OllamaEmbedder) embedSingle(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbeddingRequest{
		Model: e.model,
		Input: text,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	embedding, err := e.requestEmbed(ctx, e.baseURL+"/api/embed", bodyBytes)
	if err != nil {
		// Fall back to the legacy endpoint only when /api/embed does not exist
		// on this Ollama; a timeout or overload would just fail again there.
		if !isEndpointUnsupported(err) {
			return nil, err
		}
		type legacyReq struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
		}
		legacyBody, mErr := json.Marshal(legacyReq{Model: e.model, Prompt: text})
		if mErr != nil {
			return nil, fmt.Errorf("ollama: marshal legacy request: %w", mErr)
		}
		embedding, err = e.requestEmbed(ctx, e.baseURL+"/api/embeddings", legacyBody)
		if err != nil {
			return nil, err
		}
	}

	// Auto-detect dimension on first successful call
	if e.Dimension() == 0 && len(embedding) > 0 {
		e.mu.Lock()
		e.dim = len(embedding)
		e.mu.Unlock()
	}

	return embedding, nil
}

func (e *OllamaEmbedder) requestEmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := ollamaEmbeddingRequest{
		Model: e.model,
		Input: texts,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal batch request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/api/embed", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("ollama: create batch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: batch request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: read batch response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: batch %w", &ollamaHTTPError{StatusCode: resp.StatusCode, Body: string(body)})
	}

	var embResp ollamaEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("ollama: unmarshal batch response: %w", err)
	}

	if len(embResp.Embeddings) == 0 {
		if len(embResp.Embedding) == 0 {
			return nil, fmt.Errorf("ollama: empty batch embeddings")
		}
		embResp.Embeddings = [][]float64{embResp.Embedding}
	}

	if len(embResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama: batch embedding count mismatch: got %d, expected %d", len(embResp.Embeddings), len(texts))
	}

	out := make([][]float32, len(embResp.Embeddings))
	for i, row := range embResp.Embeddings {
		if len(row) == 0 {
			return nil, fmt.Errorf("ollama: empty embedding vector at index %d", i)
		}
		vec := make([]float32, len(row))
		for j, v := range row {
			vec[j] = float32(v)
		}
		out[i] = vec
	}

	if e.Dimension() == 0 && len(out) > 0 && len(out[0]) > 0 {
		e.mu.Lock()
		e.dim = len(out[0])
		e.mu.Unlock()
	}

	return out, nil
}

func (e *OllamaEmbedder) requestEmbed(ctx context.Context, url string, bodyBytes []byte) ([]float32, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("ollama: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: %w", &ollamaHTTPError{StatusCode: resp.StatusCode, Body: string(body)})
	}

	var embResp ollamaEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("ollama: unmarshal response: %w", err)
	}

	raw := embResp.Embedding
	if len(raw) == 0 && len(embResp.Embeddings) > 0 {
		raw = embResp.Embeddings[0]
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("ollama: empty embedding vector from %s", url)
	}

	embedding := make([]float32, len(raw))
	for i, v := range raw {
		embedding[i] = float32(v)
	}
	return embedding, nil
}
