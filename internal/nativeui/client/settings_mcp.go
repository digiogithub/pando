package client

import (
	"context"
	"time"
)

// MCPTool is a tool a server exposes.
type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MCPOAuth is the OAuth block of a server's auth config. ClientSecret is
// write-only (omitted = keep the stored one).
type MCPOAuth struct {
	ClientID              string   `json:"clientID,omitempty"`
	ClientSecret          string   `json:"clientSecret,omitempty"`
	HasClientSecret       bool     `json:"hasClientSecret"`
	Scopes                []string `json:"scopes,omitempty"`
	RedirectURI           string   `json:"redirectURI,omitempty"`
	CallbackPort          int      `json:"callbackPort,omitempty"`
	AuthServerMetadataURL string   `json:"authServerMetadataURL,omitempty"`
}

// MCPAuth is the auth config of a remote server. Token and Password are
// write-only.
type MCPAuth struct {
	Type        string    `json:"type"`
	Token       string    `json:"token,omitempty"`
	HasToken    bool      `json:"hasToken"`
	Username    string    `json:"username,omitempty"`
	Password    string    `json:"password,omitempty"`
	HasPassword bool      `json:"hasPassword"`
	HeaderName  string    `json:"headerName,omitempty"`
	OAuth       *MCPOAuth `json:"oauth,omitempty"`
}

// MCPAuthStatus is the OAuth login state of a server.
type MCPAuthStatus struct {
	Type                  string `json:"type"`
	HasTokens             bool   `json:"hasTokens"`
	Expired               bool   `json:"expired"`
	ExpiresAt             string `json:"expiresAt,omitempty"`
	ClientID              string `json:"clientID,omitempty"`
	DynamicallyRegistered bool   `json:"dynamicallyRegistered"`
}

// MCPServer is /api/v1/config/mcp-servers item.
type MCPServer struct {
	Name       string            `json:"name"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        []string          `json:"env"`
	Type       string            `json:"type"`
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers"`
	Running    bool              `json:"running,omitempty"`
	Tools      []MCPTool         `json:"tools,omitempty"`
	Auth       *MCPAuth          `json:"auth,omitempty"`
	AuthStatus *MCPAuthStatus    `json:"authStatus,omitempty"`
}

const pathMCPServers = "/api/v1/config/mcp-servers"

// MCPServers lists the configured servers.
func (c *Client) MCPServers(ctx context.Context) ([]MCPServer, error) {
	var out struct {
		Servers []MCPServer `json:"mcpServers"`
	}
	err := c.Get(ctx, pathMCPServers, &out)
	return out.Servers, err
}

// SaveMCPServer creates or updates a server (PUT, as the Web UI does).
func (c *Client) SaveMCPServer(ctx context.Context, s MCPServer) error {
	s.Running, s.Tools, s.AuthStatus = false, nil, nil
	return c.Put(ctx, pathMCPServers, s, nil)
}

// DeleteMCPServer removes a server.
func (c *Client) DeleteMCPServer(ctx context.Context, name string) error {
	return c.Delete(ctx, pathMCPServers+"/"+Esc(name), nil)
}

// ReloadMCPServer reconnects a server and returns the tools discovered.
func (c *Client) ReloadMCPServer(ctx context.Context, name string) (int, error) {
	var out struct {
		Tools int `json:"tools"`
	}
	// Reloading performs the handshake, which may take a while.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := c.Post(ctx, pathMCPServers+"/"+Esc(name)+"/reload", map[string]any{}, &out)
	return out.Tools, err
}

// MCPLogin starts the OAuth flow and returns the authorization URL.
func (c *Client) MCPLogin(ctx context.Context, name string) (url, message string, err error) {
	var out struct {
		AuthorizationURL string `json:"authorizationUrl"`
		Message          string `json:"message"`
	}
	err = c.Post(ctx, "/api/v1/mcp/"+Esc(name)+"/login", map[string]any{}, &out)
	return out.AuthorizationURL, out.Message, err
}

// MCPLogout drops the stored OAuth tokens.
func (c *Client) MCPLogout(ctx context.Context, name string) error {
	return c.Post(ctx, "/api/v1/mcp/"+Esc(name)+"/logout", map[string]any{}, nil)
}

// MCPStatus reads the OAuth state of a server.
func (c *Client) MCPStatus(ctx context.Context, name string) (MCPAuthStatus, error) {
	var out MCPAuthStatus
	err := c.Get(ctx, "/api/v1/mcp/"+Esc(name)+"/status", &out)
	return out, err
}
