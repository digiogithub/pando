package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/embeddings"
)

// codeProjectResponse is a slimmed-down representation of a CodeProject for the API.
type codeProjectResponse struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	Status    string `json:"status"`
}

// handleListCodeProjects returns all indexed code projects.
// GET /api/v1/remembrances/projects
func (s *Server) handleListCodeProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.Remembrances == nil || s.app.Remembrances.Code == nil {
		writeJSON(w, http.StatusOK, []codeProjectResponse{})
		return
	}

	projects, err := s.app.Remembrances.Code.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects: "+err.Error())
		return
	}

	resp := make([]codeProjectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, codeProjectResponse{
			ProjectID: p.ProjectID,
			Name:      p.Name,
			RootPath:  p.RootPath,
			Status:    string(p.IndexingStatus),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// indexProjectRequest is the request body for indexing a new project.
type indexProjectRequest struct {
	// ProjectID to use (optional — defaults to sanitized path basename).
	ProjectID string `json:"project_id,omitempty"`
	// Path to index. Defaults to the working directory if omitted.
	Path string `json:"path,omitempty"`
}

// handleIndexCodeProject starts indexing a code project.
// POST /api/v1/remembrances/projects/index
func (s *Server) handleIndexCodeProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.Remembrances == nil || s.app.Remembrances.Code == nil {
		writeError(w, http.StatusServiceUnavailable, "remembrances code indexer not initialized")
		return
	}

	var req indexProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	targetPath := req.Path
	if targetPath == "" {
		targetPath = config.WorkingDirectory()
	}

	projectID := req.ProjectID
	if projectID == "" {
		projectID = sanitizeProjectID(targetPath)
	}

	jobID, err := s.app.Remembrances.Code.IndexProject(r.Context(), projectID, targetPath, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start indexing: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"project_id": projectID,
		"job_id":     jobID,
		"path":       targetPath,
		"status":     string(code.IndexingStatusPending),
	})
}

// sanitizeProjectID converts an absolute path to a safe project identifier.
func sanitizeProjectID(path string) string {
	// Use the last path component
	clean := path
	for len(clean) > 0 && (clean[len(clean)-1] == '/' || clean[len(clean)-1] == '\\') {
		clean = clean[:len(clean)-1]
	}
	for i := len(clean) - 1; i >= 0; i-- {
		if clean[i] == '/' || clean[i] == '\\' {
			clean = clean[i+1:]
			break
		}
	}
	if clean == "" {
		clean = "project"
	}
	// Replace characters that are awkward in IDs
	out := make([]byte, 0, len(clean))
	for _, b := range []byte(clean) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
			out = append(out, b)
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

// enrichmentStatusResponse is the JSON body for enrichment status and toggle endpoints.
type enrichmentStatusResponse struct {
	Enabled     bool   `json:"enabled"`
	PlannerMode string `json:"planner_mode"` // "llm" or "heuristic"
}

// handleGetEnrichmentStatus returns the current context enrichment state.
// GET /api/v1/remembrances/enrichment
func (s *Server) handleGetEnrichmentStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	e := s.app.ContextEnricher
	resp := enrichmentStatusResponse{
		Enabled:     e.IsEnabled(),
		PlannerMode: e.PlannerMode(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// embeddingTestResult holds the result of a single embedder connectivity test.
type embeddingTestResult struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
	Dimension int    `json:"dimension,omitempty"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url,omitempty"`
}

// handleTestEmbeddingConnection tests whether the configured embedding providers are reachable.
// POST /api/v1/remembrances/test-connection
// Optional body: { "type": "document" | "code" | "all" }  (default: "all")
func (s *Server) handleTestEmbeddingConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Type == "" {
		req.Type = "all"
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	rem := cfg.Remembrances

	type response struct {
		Document *embeddingTestResult `json:"document,omitempty"`
		Code     *embeddingTestResult `json:"code,omitempty"`
	}
	resp := response{}

	if req.Type == "document" || req.Type == "all" {
		resp.Document = testEmbedder(r.Context(), cfg, rem.DocumentEmbeddingProvider, rem.DocumentEmbeddingModel, rem.DocumentEmbeddingAPIKey, rem.DocumentEmbeddingBaseURL)
	}

	codeProvider := rem.CodeEmbeddingProvider
	codeModel := rem.CodeEmbeddingModel
	codeAPIKey := rem.CodeEmbeddingAPIKey
	codeBaseURL := rem.CodeEmbeddingBaseURL
	if rem.UseSameModel {
		codeProvider = rem.DocumentEmbeddingProvider
		codeModel = rem.DocumentEmbeddingModel
		codeAPIKey = rem.DocumentEmbeddingAPIKey
		codeBaseURL = rem.DocumentEmbeddingBaseURL
	}
	if req.Type == "code" || req.Type == "all" {
		resp.Code = testEmbedder(r.Context(), cfg, codeProvider, codeModel, codeAPIKey, codeBaseURL)
	}

	writeJSON(w, http.StatusOK, resp)
}

// testEmbedder creates an embedder and runs a single test query, returning diagnostics.
func testEmbedder(ctx context.Context, cfg *config.Config, provider, model, customAPIKey, customBaseURL string) *embeddingTestResult {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)

	result := &embeddingTestResult{Provider: provider, Model: model}

	if provider == "" {
		result.Error = "provider not configured"
		return result
	}
	if model == "" {
		result.Error = "model not configured"
		return result
	}

	// Resolve credentials (same logic as internal/rag/service.go).
	// For ollama, a non-empty customBaseURL overrides the provider-level default.
	var apiKey, baseURL string
	if provider == "openai-compatible" {
		apiKey = customAPIKey
		baseURL = customBaseURL
	} else {
		apiKey, baseURL = resolveProviderCredentials(cfg, provider)
		if provider == "ollama" && strings.TrimSpace(customBaseURL) != "" {
			baseURL = models.ResolveOllamaRawBaseURL(strings.TrimSpace(customBaseURL))
		}
	}
	result.BaseURL = baseURL

	emb, err := embeddings.NewEmbedder(provider, model, apiKey, baseURL)
	if err != nil {
		result.Error = "failed to create embedder: " + err.Error()
		return result
	}

	start := time.Now()
	vec, err := emb.EmbedQuery(ctx, "test connection")
	result.LatencyMS = time.Since(start).Milliseconds()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.OK = true
	result.Dimension = len(vec)
	return result
}

// resolveProviderCredentials returns apiKey and baseURL for a provider from the global config.
func resolveProviderCredentials(cfg *config.Config, provider string) (apiKey, baseURL string) {
	if cfg == nil {
		return "", ""
	}
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

// handleToggleEnrichment enables or disables context enrichment at runtime without restart.
// PUT /api/v1/remembrances/enrichment
// Body: { "enabled": true|false }
func (s *Server) handleToggleEnrichment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.app.ContextEnricher == nil {
		writeError(w, http.StatusServiceUnavailable, "context enricher not initialized")
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.app.ContextEnricher.SetEnabled(body.Enabled)

	resp := enrichmentStatusResponse{
		Enabled:     s.app.ContextEnricher.IsEnabled(),
		PlannerMode: s.app.ContextEnricher.PlannerMode(),
	}
	writeJSON(w, http.StatusOK, resp)
}
