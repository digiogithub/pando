package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// maskAPIKey returns a masked version of the API key showing only the last 4 characters.
// Returns an empty string if the key is empty or very short.
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "••••"
	}
	return "••••" + key[len(key)-4:]
}

// --- Providers ---

// ProviderConfigItem is the JSON representation of a provider configuration.
type ProviderConfigItem struct {
	Name     string `json:"name"`
	APIKey   string `json:"apiKey"`   // masked in GET responses
	BaseURL  string `json:"baseUrl"`
	Disabled bool   `json:"disabled"`
	UseOAuth bool   `json:"useOAuth"`
}

// ProviderConfigUpdateRequest is the body for PUT /api/v1/config/providers.
// APIKey is only applied if non-empty.
type ProviderConfigUpdateRequest struct {
	Providers []ProviderConfigItem `json:"providers"`
}

func (s *Server) handleConfigProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfigProviders(w, r)
	case http.MethodPut:
		s.handlePutConfigProviders(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetConfigProviders(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	items := make([]ProviderConfigItem, 0, len(cfg.Providers))
	for name, p := range cfg.Providers {
		items = append(items, ProviderConfigItem{
			Name:     string(name),
			APIKey:   maskAPIKey(p.APIKey),
			BaseURL:  p.BaseURL,
			Disabled: p.Disabled,
			UseOAuth: p.UseOAuth,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": items})
}

func (s *Server) handlePutConfigProviders(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	var req ProviderConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, item := range req.Providers {
		name := models.ModelProvider(item.Name)
		existing := cfg.Providers[name]

		// Keep existing key when the incoming value is empty or still masked.
		apiKey := item.APIKey
		if apiKey == "" || strings.HasPrefix(apiKey, "••••") {
			apiKey = existing.APIKey
		}

		if err := config.UpdateProvider(name, apiKey, item.BaseURL, item.Disabled); err != nil {
			writeError(w, http.StatusBadRequest, "failed to update provider "+item.Name+": "+err.Error())
			return
		}
		if item.UseOAuth != existing.UseOAuth {
			if err := config.UpdateProviderOAuth(name, item.UseOAuth); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update provider OAuth "+item.Name+": "+err.Error())
				return
			}
		}
	}

	s.handleGetConfigProviders(w, r)
}

// --- Agents ---

// AgentConfigItem is the JSON representation of a single agent configuration.
type AgentConfigItem struct {
	Name                 string              `json:"name"`
	Model                models.ModelID      `json:"model"`
	MaxTokens            int64               `json:"maxTokens"`
	ReasoningEffort      string              `json:"reasoningEffort"`
	ThinkingMode         config.ThinkingMode `json:"thinkingMode,omitempty"`
	AutoCompact          bool                `json:"autoCompact"`
	AutoCompactThreshold float64             `json:"autoCompactThreshold"`
}

func (s *Server) handleConfigAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfigAgents(w, r)
	case http.MethodPut:
		s.handlePutConfigAgents(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetConfigAgents(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	items := make([]AgentConfigItem, 0, len(cfg.Agents))
	for name, a := range cfg.Agents {
		items = append(items, AgentConfigItem{
			Name:                 string(name),
			Model:                a.Model,
			MaxTokens:            a.MaxTokens,
			ReasoningEffort:      a.ReasoningEffort,
			ThinkingMode:         a.ThinkingMode,
			AutoCompact:          a.AutoCompact,
			AutoCompactThreshold: a.AutoCompactThreshold,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"agents": items})
}

func (s *Server) handlePutConfigAgents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agents []AgentConfigItem `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, item := range req.Agents {
		agent := config.Agent{
			Model:                item.Model,
			MaxTokens:            item.MaxTokens,
			ReasoningEffort:      item.ReasoningEffort,
			ThinkingMode:         item.ThinkingMode,
			AutoCompact:          item.AutoCompact,
			AutoCompactThreshold: item.AutoCompactThreshold,
		}
		if err := config.UpdateAgent(config.AgentName(item.Name), agent); err != nil {
			writeError(w, http.StatusBadRequest, "failed to update agent "+item.Name+": "+err.Error())
			return
		}
	}

	s.handleGetConfigAgents(w, r)
}

// --- MCP Servers ---

// MCPToolInfo is a lightweight tool descriptor returned alongside server config.
type MCPToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MCPServerConfigItem is the JSON representation of a single MCP server entry.
type MCPServerConfigItem struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     []string          `json:"env"`
	Type    config.MCPType    `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Tools   []MCPToolInfo     `json:"tools"`
}

func (s *Server) handleConfigMCPServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfigMCPServers(w, r)
	case http.MethodPut:
		s.handlePutConfigMCPServer(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetConfigMCPServers(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	// Load tool registry from DB grouped by server_name.
	toolsByServer := map[string][]MCPToolInfo{}
	if s.config.DB != nil {
		rows, err := s.config.DB.QueryContext(r.Context(), `
			SELECT server_name, tool_name, COALESCE(description, '')
			FROM mcp_tool_registry
			ORDER BY server_name, tool_name
		`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var srvName, toolName, desc string
				if rows.Scan(&srvName, &toolName, &desc) == nil {
					toolsByServer[srvName] = append(toolsByServer[srvName], MCPToolInfo{Name: toolName, Description: desc})
				}
			}
		}
	}

	items := make([]MCPServerConfigItem, 0, len(cfg.MCPServers))
	for name, srv := range cfg.MCPServers {
		tools := toolsByServer[name]
		if tools == nil {
			tools = []MCPToolInfo{}
		}
		items = append(items, MCPServerConfigItem{
			Name:    name,
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
			Type:    srv.Type,
			URL:     srv.URL,
			Headers: srv.Headers,
			Tools:   tools,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"mcpServers": items})
}

func (s *Server) handlePutConfigMCPServer(w http.ResponseWriter, r *http.Request) {
	var req MCPServerConfigItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	server := config.MCPServer{
		Command: req.Command,
		Args:    req.Args,
		Env:     req.Env,
		Type:    req.Type,
		URL:     req.URL,
		Headers: req.Headers,
	}
	if err := config.UpdateMCPServer(req.Name, server); err != nil {
		writeError(w, http.StatusBadRequest, "failed to update MCP server: "+err.Error())
		return
	}

	s.handleGetConfigMCPServers(w, r)
}

// handleDeleteConfigMCPServer handles DELETE /api/v1/config/mcp-servers/{name}.
func (s *Server) handleDeleteConfigMCPServer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := config.DeleteMCPServer(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleReloadMCPServer handles POST /api/v1/config/mcp-servers/{name}/reload.
// Since MCP servers are managed as external processes, this endpoint signals intent to
// reconnect. The actual reconnect happens when the MCP client next establishes a session.
func (s *Server) handleReloadMCPServer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	if _, ok := cfg.MCPServers[name]; !ok {
		writeError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reload scheduled", "name": name})
}

// --- MCP Gateway ---

func (s *Server) handleConfigMCPGateway(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, cfg.MCPGateway)
	case http.MethodPut:
		var req config.MCPGatewayConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := config.UpdateMCPGateway(req); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update MCP gateway config: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config.Get().MCPGateway)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- LSP ---

// LSPConfigItem is the JSON representation of a single LSP configuration entry.
type LSPConfigItem struct {
	Language  string   `json:"language"`
	Disabled  bool     `json:"disabled"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Languages []string `json:"languages"`
}

func (s *Server) handleConfigLSP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfigLSP(w, r)
	case http.MethodPut:
		s.handlePutConfigLSP(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetConfigLSP(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	items := make([]LSPConfigItem, 0, len(cfg.LSP))
	for lang, lsp := range cfg.LSP {
		items = append(items, LSPConfigItem{
			Language:  lang,
			Disabled:  lsp.Disabled,
			Command:   lsp.Command,
			Args:      lsp.Args,
			Languages: lsp.Languages,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"lsp": items})
}

func (s *Server) handlePutConfigLSP(w http.ResponseWriter, r *http.Request) {
	var req LSPConfigItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Language) == "" {
		writeError(w, http.StatusBadRequest, "language is required")
		return
	}

	lsp := config.LSPConfig{
		Disabled:  req.Disabled,
		Command:   req.Command,
		Args:      req.Args,
		Languages: req.Languages,
	}
	if err := config.UpdateLSP(req.Language, lsp); err != nil {
		writeError(w, http.StatusBadRequest, "failed to update LSP config: "+err.Error())
		return
	}

	s.handleGetConfigLSP(w, r)
}

// handleDeleteConfigLSP handles DELETE /api/v1/config/lsp/{language}.
func (s *Server) handleDeleteConfigLSP(w http.ResponseWriter, r *http.Request) {
	language := r.PathValue("language")
	if strings.TrimSpace(language) == "" {
		writeError(w, http.StatusBadRequest, "language is required")
		return
	}

	if err := config.DeleteLSP(language); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Tools ---

// ToolsConfigResponse is the GET response for /api/v1/config/tools.
// API keys are masked.
type ToolsConfigResponse struct {
	FetchEnabled   bool `json:"fetchEnabled"`
	FetchMaxSizeMB int  `json:"fetchMaxSizeMB"`

	GoogleSearchEnabled  bool   `json:"googleSearchEnabled"`
	GoogleAPIKey         string `json:"googleApiKey"` // masked
	GoogleSearchEngineID string `json:"googleSearchEngineId"`

	BraveSearchEnabled bool   `json:"braveSearchEnabled"`
	BraveAPIKey        string `json:"braveApiKey"` // masked

	PerplexitySearchEnabled bool   `json:"perplexitySearchEnabled"`
	PerplexityAPIKey        string `json:"perplexityApiKey"` // masked

	ExaSearchEnabled bool   `json:"exaSearchEnabled"`
	ExaAPIKey        string `json:"exaApiKey"` // masked

	Context7Enabled bool `json:"context7Enabled"`

	BrowserEnabled     bool   `json:"browserEnabled"`
	BrowserHeadless    bool   `json:"browserHeadless"`
	BrowserTimeout     int    `json:"browserTimeout"`
	BrowserUserDataDir string `json:"browserUserDataDir"`
	BrowserMaxSessions int    `json:"browserMaxSessions"`
}

func (s *Server) handleConfigTools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfigTools(w, r)
	case http.MethodPut:
		s.handlePutConfigTools(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetConfigTools(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	t := cfg.InternalTools
	resp := ToolsConfigResponse{
		FetchEnabled:            t.FetchEnabled,
		FetchMaxSizeMB:          t.FetchMaxSizeMB,
		GoogleSearchEnabled:     t.GoogleSearchEnabled,
		GoogleAPIKey:            maskAPIKey(t.GoogleAPIKey),
		GoogleSearchEngineID:    t.GoogleSearchEngineID,
		BraveSearchEnabled:      t.BraveSearchEnabled,
		BraveAPIKey:             maskAPIKey(t.BraveAPIKey),
		PerplexitySearchEnabled: t.PerplexitySearchEnabled,
		PerplexityAPIKey:        maskAPIKey(t.PerplexityAPIKey),
		ExaSearchEnabled:        t.ExaSearchEnabled,
		ExaAPIKey:               maskAPIKey(t.ExaAPIKey),
		Context7Enabled:         t.Context7Enabled,
		BrowserEnabled:          t.BrowserEnabled,
		BrowserHeadless:         t.BrowserHeadless,
		BrowserTimeout:          t.BrowserTimeout,
		BrowserUserDataDir:      t.BrowserUserDataDir,
		BrowserMaxSessions:      t.BrowserMaxSessions,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePutConfigTools(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	// Use a map to detect which fields were actually sent.
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Re-encode and decode into the struct so we have typed values.
	var req config.InternalToolsConfig
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Keep existing masked keys when incoming is empty or masked.
	existing := cfg.InternalTools
	if req.GoogleAPIKey == "" || strings.HasPrefix(req.GoogleAPIKey, "••••") {
		req.GoogleAPIKey = existing.GoogleAPIKey
	}
	if req.BraveAPIKey == "" || strings.HasPrefix(req.BraveAPIKey, "••••") {
		req.BraveAPIKey = existing.BraveAPIKey
	}
	if req.PerplexityAPIKey == "" || strings.HasPrefix(req.PerplexityAPIKey, "••••") {
		req.PerplexityAPIKey = existing.PerplexityAPIKey
	}
	if req.ExaAPIKey == "" || strings.HasPrefix(req.ExaAPIKey, "••••") {
		req.ExaAPIKey = existing.ExaAPIKey
	}

	if err := config.UpdateInternalTools(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update tools config: "+err.Error())
		return
	}

	s.handleGetConfigTools(w, r)
}

// --- Bash ---

func (s *Server) handleConfigBash(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, cfg.Bash)
	case http.MethodPut:
		var req config.BashConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := config.UpdateBash(req); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update bash config: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config.Get().Bash)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- Extensions ---

// ExtensionsConfigResponse groups Skills, SkillsCatalog, and Lua engine configuration.
type ExtensionsConfigResponse struct {
	Skills        config.SkillsConfig        `json:"skills"`
	SkillsCatalog config.SkillsCatalogConfig  `json:"skillsCatalog"`
	Lua           config.LuaConfig            `json:"lua"`
}

func (s *Server) handleConfigExtensions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		resp := ExtensionsConfigResponse{
			Skills:        cfg.Skills,
			SkillsCatalog: cfg.SkillsCatalog,
			Lua:           cfg.Lua,
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPut:
		var req ExtensionsConfigResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := config.UpdateSkillsEnabled(req.Skills.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update skills config: "+err.Error())
			return
		}
		if err := config.UpdateSkillsCatalog(req.SkillsCatalog); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update skills catalog config: "+err.Error())
			return
		}
		if err := config.UpdateLua(req.Lua); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update Lua config: "+err.Error())
			return
		}

		cfg := config.Get()
		writeJSON(w, http.StatusOK, ExtensionsConfigResponse{
			Skills:        cfg.Skills,
			SkillsCatalog: cfg.SkillsCatalog,
			Lua:           cfg.Lua,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- Services ---

// ServicesConfigResponse groups Mesnada, Remembrances, Snapshots, and API Server configuration.
type ServicesConfigResponse struct {
	Mesnada      config.MesnadaConfig      `json:"mesnada"`
	Remembrances config.RemembrancesConfig `json:"remembrances"`
	Snapshots    config.SnapshotsConfig    `json:"snapshots"`
	Server       config.APIServerConfig    `json:"server"`
}

func (s *Server) handleConfigServices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		resp := ServicesConfigResponse{
			Mesnada:      cfg.Mesnada,
			Remembrances: cfg.Remembrances,
			Snapshots:    cfg.Snapshots,
			Server:       cfg.Server,
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPut:
		var req ServicesConfigResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := config.UpdateMesnada(req.Mesnada); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update Mesnada config: "+err.Error())
			return
		}

		// Mask API key fields in Remembrances before update.
		cfg := config.Get()
		existing := cfg.Remembrances
		if req.Remembrances.DocumentEmbeddingAPIKey == "" || strings.HasPrefix(req.Remembrances.DocumentEmbeddingAPIKey, "••••") {
			req.Remembrances.DocumentEmbeddingAPIKey = existing.DocumentEmbeddingAPIKey
		}
		if req.Remembrances.CodeEmbeddingAPIKey == "" || strings.HasPrefix(req.Remembrances.CodeEmbeddingAPIKey, "••••") {
			req.Remembrances.CodeEmbeddingAPIKey = existing.CodeEmbeddingAPIKey
		}
		if err := config.UpdateRemembrances(req.Remembrances); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update Remembrances config: "+err.Error())
			return
		}

		if err := config.UpdateSnapshots(req.Snapshots); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update Snapshots config: "+err.Error())
			return
		}
		if err := config.UpdateServer(req.Server); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update Server config: "+err.Error())
			return
		}

		updated := config.Get()
		writeJSON(w, http.StatusOK, ServicesConfigResponse{
			Mesnada:      updated.Mesnada,
			Remembrances: updated.Remembrances,
			Snapshots:    updated.Snapshots,
			Server:       updated.Server,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- Evaluator ---

func (s *Server) handleConfigEvaluator(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, cfg.Evaluator)
	case http.MethodPut:
		var req config.EvaluatorConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := config.UpdateEvaluator(req); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update evaluator config: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config.Get().Evaluator)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
