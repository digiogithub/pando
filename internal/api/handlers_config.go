package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mcpauth"
	"github.com/digiogithub/pando/internal/mcpgateway"
	"github.com/digiogithub/pando/internal/savings"
)

// mcpRefreshTimeout bounds an interactive MCP reconnect + ListTools round trip
// so a hanging server cannot block the HTTP handler indefinitely.
const mcpRefreshTimeout = 45 * time.Second

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
	APIKey   string `json:"apiKey"` // masked in GET responses
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
	ResolvedMaxTokens    int64               `json:"resolvedMaxTokens,omitempty"` // effective value after auto-budget resolution
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

	// Only expose the canonical built-in agents (config.KnownAgentNames), in a
	// stable order. Any stray/unknown agent key is intentionally not surfaced so it
	// cannot appear as a phantom duplicate in the web-UI.
	items := make([]AgentConfigItem, 0, len(config.KnownAgentNames))
	for _, name := range config.KnownAgentNames {
		a := cfg.Agents[name]
		model := models.SupportedModels()[a.Model]
		items = append(items, AgentConfigItem{
			Name:                 string(name),
			Model:                a.Model,
			MaxTokens:            a.MaxTokens,
			ResolvedMaxTokens:    config.ResolveAgentMaxTokens(name, a, model),
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
		// Resolve the incoming model ID to its registered canonical form so the value
		// persisted matches what the TUI writes and what validateAgent accepts on the
		// next reload. Without this a non-canonical ID would be reverted to a default.
		modelID := item.Model
		if resolved, ok := models.ResolveModelID(modelID); ok {
			modelID = resolved
		}
		agent := config.Agent{
			Model:                modelID,
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
	Running bool              `json:"running"`
	Tools   []MCPToolInfo     `json:"tools"`
	// Auth describes the authentication mechanism configured for this server.
	// Secret fields (Token, Password, OAuth.ClientSecret) are write-only: GET
	// responses never populate them, only the corresponding HasXxx booleans.
	Auth *MCPServerAuthItem `json:"auth,omitempty"`
	// AuthStatus is a computed, read-only snapshot of the current OAuth state
	// (populated only for Auth.Type == "oauth"); nil for other auth types.
	AuthStatus *MCPServerAuthStatusItem `json:"authStatus,omitempty"`
}

// MCPServerOAuthItem is the JSON representation of config.MCPOAuthConfig.
// ClientSecret is write-only (accepted on PUT, never returned on GET); the
// GET path instead reports HasClientSecret.
type MCPServerOAuthItem struct {
	ClientID              string   `json:"clientID,omitempty"`
	ClientSecret          string   `json:"clientSecret,omitempty"` // write-only; empty means "keep existing"
	HasClientSecret       bool     `json:"hasClientSecret"`
	Scopes                []string `json:"scopes,omitempty"`
	RedirectURI           string   `json:"redirectURI,omitempty"`
	CallbackPort          int      `json:"callbackPort,omitempty"`
	AuthServerMetadataURL string   `json:"authServerMetadataURL,omitempty"`
}

// MCPServerAuthItem is the JSON representation of config.MCPAuth. Token and
// Password are write-only (accepted on PUT, never returned on GET); the GET
// path instead reports HasToken/HasPassword. An empty Token/Password/
// OAuth.ClientSecret on a PUT request means "leave the stored secret
// unchanged", so the WebUI never needs to round-trip a decrypted value.
type MCPServerAuthItem struct {
	Type        string              `json:"type,omitempty"`
	Token       string              `json:"token,omitempty"` // write-only
	HasToken    bool                `json:"hasToken"`
	Username    string              `json:"username,omitempty"`
	Password    string              `json:"password,omitempty"` // write-only
	HasPassword bool                `json:"hasPassword"`
	HeaderName  string              `json:"headerName,omitempty"`
	OAuth       *MCPServerOAuthItem `json:"oauth,omitempty"`
}

// MCPServerAuthStatusItem is the JSON representation of mcpauth.StatusInfo,
// computed live from the on-disk credential store — never from the config
// file — so it always reflects the current login state.
type MCPServerAuthStatusItem struct {
	Type                  string     `json:"type"`
	HasTokens             bool       `json:"hasTokens"`
	Expired               bool       `json:"expired"`
	ExpiresAt             *time.Time `json:"expiresAt,omitempty"`
	ClientID              string     `json:"clientID,omitempty"`
	DynamicallyRegistered bool       `json:"dynamicallyRegistered"`
}

// buildMCPServerAuthItem converts server's (in-memory, plaintext) Auth block
// into its API-safe DTO: secret values are reduced to booleans, everything
// else passes through unchanged.
func buildMCPServerAuthItem(server config.MCPServer) *MCPServerAuthItem {
	if server.Auth == nil {
		return nil
	}
	auth := server.Auth
	item := &MCPServerAuthItem{
		Type:        string(auth.Type),
		HasToken:    strings.TrimSpace(auth.Token) != "",
		Username:    auth.Username,
		HasPassword: strings.TrimSpace(auth.Password) != "",
		HeaderName:  auth.HeaderName,
	}
	if auth.OAuth != nil {
		item.OAuth = &MCPServerOAuthItem{
			ClientID:              auth.OAuth.ClientID,
			HasClientSecret:       strings.TrimSpace(auth.OAuth.ClientSecret) != "",
			Scopes:                auth.OAuth.Scopes,
			RedirectURI:           auth.OAuth.RedirectURI,
			CallbackPort:          auth.OAuth.CallbackPort,
			AuthServerMetadataURL: auth.OAuth.AuthServerMetadataURL,
		}
	}
	return item
}

// buildMCPServerAuthStatusItem converts mcpauth's StatusInfo into its API
// DTO. Returns nil for non-OAuth servers, since there is nothing dynamic to
// report (the static Auth block above is the complete picture).
func buildMCPServerAuthStatusItem(info mcpauth.StatusInfo) *MCPServerAuthStatusItem {
	if info.AuthType != string(config.MCPAuthOAuth) {
		return nil
	}
	item := &MCPServerAuthStatusItem{
		Type:                  info.AuthType,
		HasTokens:             info.HasTokens,
		Expired:               info.Expired,
		ClientID:              info.ClientID,
		DynamicallyRegistered: info.DynamicallyRegistered,
	}
	if !info.ExpiresAt.IsZero() {
		expiresAt := info.ExpiresAt
		item.ExpiresAt = &expiresAt
	}
	return item
}

// mergeMCPAuthRequest builds the config.MCPAuth to persist from an incoming
// request DTO, folding in the previously-stored auth block (existing, which
// may be nil) so that an empty Token/Password/OAuth.ClientSecret in the
// request keeps the previously stored (possibly AGE-encrypted) value instead
// of wiping it. A nil req clears the Auth block entirely (explicit removal).
func mergeMCPAuthRequest(existing *config.MCPAuth, req *MCPServerAuthItem) *config.MCPAuth {
	if req == nil {
		return nil
	}
	auth := &config.MCPAuth{
		Type:       config.MCPAuthType(req.Type),
		Username:   req.Username,
		HeaderName: req.HeaderName,
	}
	auth.Token = req.Token
	if strings.TrimSpace(auth.Token) == "" && existing != nil {
		auth.Token = existing.Token
	}
	auth.Password = req.Password
	if strings.TrimSpace(auth.Password) == "" && existing != nil {
		auth.Password = existing.Password
	}
	if req.OAuth != nil {
		oauth := &config.MCPOAuthConfig{
			ClientID:              req.OAuth.ClientID,
			Scopes:                req.OAuth.Scopes,
			RedirectURI:           req.OAuth.RedirectURI,
			CallbackPort:          req.OAuth.CallbackPort,
			AuthServerMetadataURL: req.OAuth.AuthServerMetadataURL,
		}
		oauth.ClientSecret = req.OAuth.ClientSecret
		if strings.TrimSpace(oauth.ClientSecret) == "" && existing != nil && existing.OAuth != nil {
			oauth.ClientSecret = existing.OAuth.ClientSecret
		}
		auth.OAuth = oauth
	} else if existing != nil && existing.OAuth != nil && auth.Type == config.MCPAuthOAuth {
		// Preserve the previously stored OAuth block when the request omits
		// it but keeps the type as oauth (e.g. a partial update that only
		// touches Token/Username).
		oauth := *existing.OAuth
		auth.OAuth = &oauth
	}
	return auth
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
		running := false
		if s.app != nil && s.app.MCPGateway != nil {
			running = s.app.MCPGateway.HasClient(name)
		}
		var authStatus *MCPServerAuthStatusItem
		if srv.Auth.IsOAuth() {
			authStatus = buildMCPServerAuthStatusItem(mcpauth.Default().Status(name, srv))
		}

		items = append(items, MCPServerConfigItem{
			Name:       name,
			Command:    srv.Command,
			Args:       srv.Args,
			Env:        srv.Env,
			Type:       srv.Type,
			URL:        srv.URL,
			Headers:    srv.Headers,
			Running:    running,
			Tools:      tools,
			Auth:       buildMCPServerAuthItem(srv),
			AuthStatus: authStatus,
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

	var existingAuth *config.MCPAuth
	if cfg := config.Get(); cfg != nil {
		if existing, ok := cfg.MCPServers[req.Name]; ok {
			existingAuth = existing.Auth
		}
	}

	server := config.MCPServer{
		Command: req.Command,
		Args:    req.Args,
		Env:     req.Env,
		Type:    req.Type,
		URL:     req.URL,
		Headers: req.Headers,
		Auth:    mergeMCPAuthRequest(existingAuth, req.Auth),
	}
	if err := server.Auth.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid MCP auth configuration: "+err.Error())
		return
	}
	if err := config.UpdateMCPServer(req.Name, server); err != nil {
		writeError(w, http.StatusBadRequest, "failed to update MCP server: "+err.Error())
		return
	}

	// Reconnect and rediscover right away so the saved server reports its real
	// tool count without requiring a restart. Discovery failures are reported
	// through the GET payload (tools stay empty) and the log; the save itself
	// already succeeded, so this never fails the request.
	if _, err := s.refreshMCPServerTools(r.Context(), req.Name); err != nil {
		logging.Warn("MCP server saved but tool discovery failed", "server", req.Name, "error", err)
	}

	s.handleGetConfigMCPServers(w, r)
}

// refreshMCPServerTools reconnects to one configured MCP server, refreshes its
// entry in the tool catalog and invalidates the agent's MCP tool cache so the
// next turn sees the new tool set. It works whether or not the MCP gateway is
// enabled: without a gateway it talks to the catalog registry directly.
func (s *Server) refreshMCPServerTools(ctx context.Context, name string) (int, error) {
	cfg := config.Get()
	if cfg == nil {
		return 0, fmt.Errorf("configuration not loaded")
	}
	srv, ok := cfg.MCPServers[name]
	if !ok {
		return 0, fmt.Errorf("MCP server not found: %s", name)
	}

	ctx, cancel := context.WithTimeout(ctx, mcpRefreshTimeout)
	defer cancel()

	// The agent builds its MCP tool list lazily and caches it process-wide; drop
	// that cache regardless of how discovery below goes, then hand the running
	// agent its rebuilt tool set so the change applies without a restart.
	defer func() {
		agent.ResetMcpToolsCache()
		if s.app != nil {
			s.app.RefreshAgentTools()
		}
	}()

	if s.app != nil && s.app.MCPGateway != nil {
		return s.app.MCPGateway.RefreshServer(ctx, name, srv)
	}
	if s.config.DB == nil {
		return 0, fmt.Errorf("no database available for the MCP tool catalog")
	}
	resolved, err := config.ResolveMCPServerSecrets(srv)
	if err != nil {
		return 0, fmt.Errorf("resolve MCP server %s secrets: %w", name, err)
	}
	return mcpgateway.NewRegistry(s.config.DB).DiscoverServer(ctx, name, resolved)
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

	// Forget the deleted server's tools so they stop showing up in the catalog
	// and are not offered to the agent any more.
	if s.app != nil && s.app.MCPGateway != nil {
		if err := s.app.MCPGateway.DeleteServerData(r.Context(), name); err != nil {
			logging.Warn("failed to delete MCP server tool catalog entries", "server", name, "error", err)
		}
	} else if s.config.DB != nil {
		if err := mcpgateway.NewRegistry(s.config.DB).DeleteServer(r.Context(), name); err != nil {
			logging.Warn("failed to delete MCP server tool catalog entries", "server", name, "error", err)
		}
	}
	agent.ResetMcpToolsCache()
	if s.app != nil {
		s.app.RefreshAgentTools()
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleReloadMCPServer handles POST /api/v1/config/mcp-servers/{name}/reload.
// It drops any pooled connection to the server, reconnects, re-lists its tools
// and refreshes the catalog, so the caller learns the current tool count — or
// the exact connection error when discovery fails.
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

	count, err := s.refreshMCPServerTools(r.Context(), name)
	if err != nil {
		logging.Error("MCP server reload failed", "server", name, "error", err)
		writeError(w, http.StatusBadGateway, "failed to reload MCP server "+name+": "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "reloaded", "name": name, "tools": count})
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

// handleConfigMCPServer handles GET/PUT for the MCPServer tool-group configuration.
func (s *Server) handleConfigMCPServer(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, cfg.MCPServer)
	case http.MethodPut:
		var req config.MCPServerConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := config.UpdateMCPServerConfig(req); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update MCP server config: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config.Get().MCPServer)
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
	// Filenames are base names handled regardless of extension (Dockerfile).
	Filenames []string `json:"filenames,omitempty"`
	Autostart bool     `json:"autostart"`
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
			Filenames: lsp.Filenames,
			Autostart: lsp.Autostart,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Language < items[j].Language })

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"lsp":        items,
		"activation": cfg.LSPActivationSettings(),
	})
}

// handleConfigLSPCatalog handles GET /api/v1/config/lsp/catalog: the full
// registry (built-in presets plus user servers) with, for each server, whether
// its binary is installed, installable by Pando, or needs a manual install.
func (s *Server) handleConfigLSPCatalog(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeError(w, http.StatusServiceUnavailable, "application not available")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"servers":    s.app.LSPServerStatuses(),
		"activation": config.Get().LSPActivationSettings(),
	})
}

// handleConfigLSPActivation handles GET/PUT /api/v1/config/lsp/activation, the
// global on-demand knobs (LSPAutoActivate, LSPActivateOn, LSPAutoInstall and
// the two timeouts).
func (s *Server) handleConfigLSPActivation(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, config.Get().LSPActivationSettings())
	case http.MethodPut:
		var req config.LSPActivationSettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := config.UpdateLSPActivation(req); err != nil {
			writeError(w, http.StatusBadRequest, "failed to update LSP activation: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config.Get().LSPActivationSettings())
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
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
		Filenames: req.Filenames,
		Autostart: req.Autostart,
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

	SourcegraphEnabled bool   `json:"sourcegraphEnabled"`
	SourcegraphToken   string `json:"sourcegraphToken"` // masked

	Context7Enabled bool `json:"context7Enabled"`

	BrowserType        string `json:"browserType"`
	BrowserExecutable  string `json:"browserExecutable"`
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
		SourcegraphEnabled:      t.SourcegraphEnabled,
		SourcegraphToken:        maskAPIKey(t.SourcegraphToken),
		Context7Enabled:         t.Context7Enabled,
		BrowserType:             t.BrowserType,
		BrowserExecutable:       t.BrowserExecutable,
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
	if req.SourcegraphToken == "" || strings.HasPrefix(req.SourcegraphToken, "••••") {
		req.SourcegraphToken = existing.SourcegraphToken
	}
	if strings.TrimSpace(req.BrowserType) == "" {
		req.BrowserType = existing.BrowserType
	}
	if strings.TrimSpace(req.BrowserExecutable) == "" {
		req.BrowserExecutable = existing.BrowserExecutable
	}
	if strings.TrimSpace(req.BrowserType) == "" {
		req.BrowserType = "chrome"
	}

	if err := config.UpdateInternalTools(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update tools config: "+err.Error())
		return
	}

	s.handleGetConfigTools(w, r)
}

// --- OpenLit ---

func (s *Server) handleConfigOpenLit(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, cfg.OpenLit)
	case http.MethodPut:
		var req config.OpenLitConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := config.UpdateOpenLit(req); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update OpenLit config: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config.Get().OpenLit)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
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

// --- Token Optimization ---

// TokenOptimizationConfigResponse is the DTO for the Token Optimization settings
// section. It carries the dedicated TokenOptimization config plus the existing
// RTK shell-output filter knobs from Bash, which the section *surfaces* (the
// fields are not moved out of BashConfig). OutputFilterEnabled is the inverse of
// Bash.OutputFilterDisabled, presented as a friendly "enable compression" toggle.
type TokenOptimizationConfigResponse struct {
	config.TokenOptimizationConfig
	OutputFilterEnabled bool     `json:"outputFilterEnabled"`
	OutputFilterPaths   []string `json:"outputFilterPaths,omitempty"`
}

func (s *Server) handleConfigTokenOptimization(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, TokenOptimizationConfigResponse{
			TokenOptimizationConfig: cfg.TokenOptimization,
			OutputFilterEnabled:     !cfg.Bash.OutputFilterDisabled,
			OutputFilterPaths:       cfg.Bash.OutputFilterPaths,
		})
	case http.MethodPut:
		var req TokenOptimizationConfigResponse
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := config.UpdateTokenOptimization(req.TokenOptimizationConfig); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update token optimization config: "+err.Error())
			return
		}
		// Surface the RTK toggle: write the inverted enable flag + paths back into Bash.
		bashCfg := config.Get().Bash
		bashCfg.OutputFilterDisabled = !req.OutputFilterEnabled
		bashCfg.OutputFilterPaths = req.OutputFilterPaths
		if err := config.UpdateBash(bashCfg); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update output filter config: "+err.Error())
			return
		}
		cfg := config.Get()
		writeJSON(w, http.StatusOK, TokenOptimizationConfigResponse{
			TokenOptimizationConfig: cfg.TokenOptimization,
			OutputFilterEnabled:     !cfg.Bash.OutputFilterDisabled,
			OutputFilterPaths:       cfg.Bash.OutputFilterPaths,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleSavings reports the aggregated token-savings ledger (Phase 5). Read-only.
// Optional ?days=N restricts the window to the last N days.
func (s *Server) handleSavings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := config.Get()
	if cfg == nil || cfg.Data.Directory == "" {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	opts := savings.SummaryOptions{}
	if d := strings.TrimSpace(r.URL.Query().Get("days")); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			opts.Since = time.Now().AddDate(0, 0, -n)
		}
	}
	rep, err := savings.Summarize(cfg.Data.Directory, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read savings ledger: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		savings.Report
		LedgerEnabled bool `json:"ledgerEnabled"`
	}{Report: rep, LedgerEnabled: cfg.SavingsLedgerEnabled()})
}

// --- Extensions ---

// ExtensionsConfigResponse groups Skills, SkillsCatalog, and Lua engine configuration.
type ExtensionsConfigResponse struct {
	Skills        config.SkillsConfig        `json:"skills"`
	SkillsCatalog config.SkillsCatalogConfig `json:"skillsCatalog"`
	Lua           config.LuaConfig           `json:"lua"`
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

		if err := config.UpdateSkills(req.Skills); err != nil {
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
			Server:       maskBasicAuthPasswords(cfg.Server),
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
		if err := config.UpdateServer(preserveBasicAuth(req.Server, cfg.Server)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update Server config: "+err.Error())
			return
		}

		updated := config.Get()
		writeJSON(w, http.StatusOK, ServicesConfigResponse{
			Mesnada:      updated.Mesnada,
			Remembrances: updated.Remembrances,
			Snapshots:    updated.Snapshots,
			Server:       maskBasicAuthPasswords(updated.Server),
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
		writeJSON(w, http.StatusOK, config.EvaluatorWithDefaults(cfg.Evaluator))
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
