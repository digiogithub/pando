package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// resetMCPAuthTestConfig loads a fresh, isolated config for MCP auth handler
// tests. It also points PANDO_MCP_AUTH_FILE at a per-test temp file: because
// mcpauth.Default() is a process-wide sync.Once singleton, only the first
// test in this binary to reach it actually picks up its own path, but every
// test uses a distinct server name so they never collide within the shared
// store.
func resetMCPAuthTestConfig(t *testing.T, servers map[string]config.MCPServer) {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pando.json")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	t.Setenv("PANDO_MCP_AUTH_FILE", filepath.Join(t.TempDir(), "mcp-auth.json"))

	if servers == nil {
		servers = map[string]config.MCPServer{}
	}
	config.SetForTests(&config.Config{
		WorkingDir: tmpDir,
		MCPServers: servers,
		LSP:        map[string]config.LSPConfig{},
	})
}

func TestBuildMCPServerAuthItemNeverExposesSecrets(t *testing.T) {
	server := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example.com/mcp",
		Auth: &config.MCPAuth{
			Type:     config.MCPAuthBearer,
			Token:    "super-secret-token",
			Username: "alice",
			Password: "super-secret-password",
			OAuth: &config.MCPOAuthConfig{
				ClientID:     "client-123",
				ClientSecret: "oauth-secret",
			},
		},
	}

	item := buildMCPServerAuthItem(server)
	if item == nil {
		t.Fatal("expected non-nil auth item")
	}
	if item.Token != "" {
		t.Fatalf("token leaked in DTO: %q", item.Token)
	}
	if item.Password != "" {
		t.Fatalf("password leaked in DTO: %q", item.Password)
	}
	if !item.HasToken {
		t.Error("expected HasToken=true")
	}
	if !item.HasPassword {
		t.Error("expected HasPassword=true")
	}
	if item.Username != "alice" {
		t.Errorf("username = %q, want alice", item.Username)
	}
	if item.OAuth == nil {
		t.Fatal("expected oauth sub-item")
	}
	if item.OAuth.ClientSecret != "" {
		t.Fatalf("oauth client secret leaked in DTO: %q", item.OAuth.ClientSecret)
	}
	if !item.OAuth.HasClientSecret {
		t.Error("expected HasClientSecret=true")
	}
	if item.OAuth.ClientID != "client-123" {
		t.Errorf("client id = %q, want client-123", item.OAuth.ClientID)
	}
}

func TestBuildMCPServerAuthItemNilForNoAuth(t *testing.T) {
	if item := buildMCPServerAuthItem(config.MCPServer{}); item != nil {
		t.Fatalf("expected nil auth item for server with no Auth block, got %+v", item)
	}
}

func TestMergeMCPAuthRequestKeepsSecretsWhenRequestOmitsThem(t *testing.T) {
	existing := &config.MCPAuth{
		Type:     config.MCPAuthBearer,
		Token:    "age1:encrypted-token",
		Username: "alice",
		Password: "age1:encrypted-password",
		OAuth: &config.MCPOAuthConfig{
			ClientID:     "client-123",
			ClientSecret: "age1:encrypted-secret",
		},
	}

	// Request updates the username but leaves secret fields empty, meaning
	// "keep what's stored" per the write-path contract.
	req := &MCPServerAuthItem{
		Type:     "bearer",
		Username: "bob",
	}

	merged := mergeMCPAuthRequest(existing, req)
	if merged == nil {
		t.Fatal("expected non-nil merged auth")
	}
	if merged.Token != existing.Token {
		t.Errorf("token = %q, want preserved %q", merged.Token, existing.Token)
	}
	if merged.Username != "bob" {
		t.Errorf("username = %q, want bob", merged.Username)
	}
}

func TestMergeMCPAuthRequestAppliesNewSecretWhenProvided(t *testing.T) {
	existing := &config.MCPAuth{
		Type:  config.MCPAuthBearer,
		Token: "age1:old-token",
	}
	req := &MCPServerAuthItem{
		Type:  "bearer",
		Token: "brand-new-token",
	}

	merged := mergeMCPAuthRequest(existing, req)
	if merged.Token != "brand-new-token" {
		t.Errorf("token = %q, want brand-new-token", merged.Token)
	}
}

func TestMergeMCPAuthRequestNilRequestClearsAuth(t *testing.T) {
	existing := &config.MCPAuth{Type: config.MCPAuthBearer, Token: "age1:old-token"}
	if merged := mergeMCPAuthRequest(existing, nil); merged != nil {
		t.Fatalf("expected nil merged auth when request is nil, got %+v", merged)
	}
}

func TestHandleGetConfigMCPServersIncludesMaskedAuth(t *testing.T) {
	resetMCPAuthTestConfig(t, map[string]config.MCPServer{
		"secure-server": {
			Type: config.MCPStreamableHTTP,
			URL:  "https://example.com/mcp",
			Auth: &config.MCPAuth{
				Type:  config.MCPAuthBearer,
				Token: "super-secret-token",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/mcp-servers", nil)
	rec := httptest.NewRecorder()

	var server Server
	server.handleGetConfigMCPServers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "super-secret-token") {
		t.Fatalf("response leaked the plaintext token: %s", rec.Body.String())
	}

	var got struct {
		MCPServers []MCPServerConfigItem `json:"mcpServers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.MCPServers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(got.MCPServers))
	}
	item := got.MCPServers[0]
	if item.Auth == nil {
		t.Fatal("expected auth block in response")
	}
	if !item.Auth.HasToken {
		t.Error("expected HasToken=true")
	}
	if item.Auth.Token != "" {
		t.Errorf("token = %q, want empty", item.Auth.Token)
	}
}

func TestHandleMCPAuthStatusUnknownServer(t *testing.T) {
	resetMCPAuthTestConfig(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/missing/status", nil)
	req.SetPathValue("name", "missing")
	rec := httptest.NewRecorder()

	var server Server
	server.handleMCPAuthStatus(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleMCPAuthStatusNonOAuthServer(t *testing.T) {
	resetMCPAuthTestConfig(t, map[string]config.MCPServer{
		"basic-server": {
			Type: config.MCPStreamableHTTP,
			URL:  "https://example.com/mcp",
			Auth: &config.MCPAuth{Type: config.MCPAuthNone},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/basic-server/status", nil)
	req.SetPathValue("name", "basic-server")
	rec := httptest.NewRecorder()

	var server Server
	server.handleMCPAuthStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got MCPServerAuthStatusItem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Type != string(config.MCPAuthNone) {
		t.Errorf("type = %q, want %q", got.Type, config.MCPAuthNone)
	}
	if got.HasTokens {
		t.Error("expected HasTokens=false for a non-oauth server")
	}
}

func TestHandleMCPLoginRejectsNonOAuthServer(t *testing.T) {
	resetMCPAuthTestConfig(t, map[string]config.MCPServer{
		"basic-server": {
			Type: config.MCPStreamableHTTP,
			URL:  "https://example.com/mcp",
			Auth: &config.MCPAuth{Type: config.MCPAuthBearer, Token: "tok"},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/basic-server/login", nil)
	req.SetPathValue("name", "basic-server")
	rec := httptest.NewRecorder()

	var server Server
	server.handleMCPLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleMCPLoginUnknownServer(t *testing.T) {
	resetMCPAuthTestConfig(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/missing/login", nil)
	req.SetPathValue("name", "missing")
	rec := httptest.NewRecorder()

	var server Server
	server.handleMCPLogin(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleMCPLogoutSucceedsEvenWithoutStoredCredentials(t *testing.T) {
	resetMCPAuthTestConfig(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/never-logged-in/logout", nil)
	req.SetPathValue("name", "never-logged-in")
	rec := httptest.NewRecorder()

	var server Server
	server.handleMCPLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
