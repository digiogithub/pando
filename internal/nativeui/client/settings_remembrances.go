package client

import (
	"context"
	"net/url"
)

// EmbeddingModelInfo is an embedding model offered by a provider.
type EmbeddingModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Size string `json:"size,omitempty"`
}

// EmbeddingModels is GET /api/v1/remembrances/embedding-models.
type EmbeddingModels struct {
	Models []EmbeddingModelInfo `json:"models"`
	Source string               `json:"source"`
	Error  string               `json:"error,omitempty"`
}

// ListEmbeddingModels lists the embedding models of a provider.
func (c *Client) ListEmbeddingModels(ctx context.Context, provider, baseURL, apiKey string) (EmbeddingModels, error) {
	q := url.Values{"provider": {provider}}
	if baseURL != "" {
		q.Set("base_url", baseURL)
	}
	if apiKey != "" {
		q.Set("api_key", apiKey)
	}
	var out EmbeddingModels
	err := c.Get(ctx, "/api/v1/remembrances/embedding-models?"+q.Encode(), &out)
	return out, err
}

// CodeProjectInfo is a registered code index project.
type CodeProjectInfo struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	Status    string `json:"status"`
}

// CodeProjects is GET /api/v1/remembrances/projects.
func (c *Client) CodeProjects(ctx context.Context) ([]CodeProjectInfo, error) {
	var out []CodeProjectInfo
	err := c.Get(ctx, "/api/v1/remembrances/projects", &out)
	return out, err
}

// ReindexResult is the answer of POST /api/v1/remembrances/reindex.
type ReindexResult struct {
	Started []struct {
		ProjectID string `json:"project_id"`
	} `json:"started"`
	Failed []struct {
		ProjectID string `json:"project_id"`
	} `json:"failed"`
}

// ReindexAll re-indexes every registered code project.
func (c *Client) ReindexAll(ctx context.Context) (ReindexResult, error) {
	var out ReindexResult
	err := c.Post(ctx, "/api/v1/remembrances/reindex", map[string]any{}, &out)
	return out, err
}

// IndexWorkdir indexes the working directory as a code project.
func (c *Client) IndexWorkdir(ctx context.Context) (projectID, jobID string, err error) {
	var out struct {
		ProjectID string `json:"project_id"`
		JobID     string `json:"job_id"`
	}
	err = c.Post(ctx, "/api/v1/remembrances/projects/index", map[string]any{}, &out)
	return out.ProjectID, out.JobID, err
}

// EmbeddingTestResult is one side of the test-connection answer.
type EmbeddingTestResult struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	LatencyMS int    `json:"latency_ms"`
	Dimension int    `json:"dimension,omitempty"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
}

// TestEmbedding tests the "document" or "code" embedding connection.
func (c *Client) TestEmbedding(ctx context.Context, kind string) (*EmbeddingTestResult, error) {
	var out struct {
		Document *EmbeddingTestResult `json:"document"`
		Code     *EmbeddingTestResult `json:"code"`
	}
	if err := c.Post(ctx, "/api/v1/remembrances/test-connection", map[string]string{"type": kind}, &out); err != nil {
		return nil, err
	}
	if kind == "code" {
		return out.Code, nil
	}
	return out.Document, nil
}
