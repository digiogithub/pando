package mcpauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

func TestClientInfoExpired(t *testing.T) {
	tests := []struct {
		name string
		ci   *ClientInfo
		want bool
	}{
		{name: "nil", ci: nil, want: false},
		{name: "zero means never expires", ci: &ClientInfo{SecretExpiresAt: 0}, want: false},
		{name: "future expiry", ci: &ClientInfo{SecretExpiresAt: time.Now().Add(time.Hour).Unix()}, want: false},
		{name: "past expiry", ci: &ClientInfo{SecretExpiresAt: time.Now().Add(-time.Hour).Unix()}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientInfoExpired(tt.ci); got != tt.want {
				t.Errorf("clientInfoExpired(%+v) = %v, want %v", tt.ci, got, tt.want)
			}
		})
	}
}

func TestManager_OAuthConfig_ExpiredClientSecretTreatedAsAbsent(t *testing.T) {
	m := newTestManager(t)
	if err := m.store.UpdateClientInfo("srv", &ClientInfo{
		ClientID:        "expired-id",
		ClientSecret:    "expired-secret",
		SecretExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}, "https://example.com/mcp"); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	srv := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{})
	cfg, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig: %v", err)
	}
	if cfg.ClientID != "" || cfg.ClientSecret != "" {
		t.Errorf("ClientID/Secret = %q/%q, want both empty (expired registration treated as absent)", cfg.ClientID, cfg.ClientSecret)
	}
}

func TestManager_OAuthConfig_NeverExpiringClientSecretStillUsed(t *testing.T) {
	m := newTestManager(t)
	if err := m.store.UpdateClientInfo("srv", &ClientInfo{
		ClientID:        "persisted-id",
		ClientSecret:    "persisted-secret",
		SecretExpiresAt: 0, // never
	}, "https://example.com/mcp"); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	srv := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{})
	cfg, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig: %v", err)
	}
	if cfg.ClientID != "persisted-id" {
		t.Errorf("ClientID = %q, want persisted-id", cfg.ClientID)
	}
}

func TestManager_InvalidateClientRegistration(t *testing.T) {
	m := newTestManager(t)
	if err := m.store.UpdateTokens("srv", &Tokens{AccessToken: "at"}, "https://example.com/mcp"); err != nil {
		t.Fatalf("UpdateTokens: %v", err)
	}
	if err := m.store.UpdateClientInfo("srv", &ClientInfo{ClientID: "dcr-id", ClientSecret: "dcr-secret"}, "https://example.com/mcp"); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	if err := m.InvalidateClientRegistration("srv"); err != nil {
		t.Fatalf("InvalidateClientRegistration: %v", err)
	}

	entry, ok, err := m.store.GetForURL("srv", "https://example.com/mcp")
	if err != nil || !ok {
		t.Fatalf("GetForURL: ok=%v err=%v", ok, err)
	}
	if entry.ClientInfo != nil {
		t.Errorf("ClientInfo = %+v, want nil after invalidation", entry.ClientInfo)
	}
	// Tokens must survive: invalidating the client registration is not a
	// full logout.
	if entry.Tokens == nil || entry.Tokens.AccessToken != "at" {
		t.Errorf("Tokens = %+v, want AccessToken at (untouched)", entry.Tokens)
	}

	// Cached serverState must also be dropped so a subsequent OAuthConfig
	// call rebuilds it (and no longer sees the dropped ClientInfo).
	m.mu.Lock()
	_, cached := m.states["srv"]
	m.mu.Unlock()
	if cached {
		t.Errorf("expected cached state to be dropped after InvalidateClientRegistration")
	}
}

// stubInvalidClientAuthServer serves AS metadata plus a /token endpoint
// that always rejects with the OAuth error invalid_client, so Login's
// token-exchange step can be tested without a real authorization server.
func stubInvalidClientAuthServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var s *httptest.Server
	mux.HandleFunc("/as-metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                   s.URL,
			"authorization_endpoint":   s.URL + "/authorize",
			"token_endpoint":           s.URL + "/token",
			"response_types_supported": []string{"code"},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_client",
			"error_description": "client not found",
		})
	})
	s = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestManager_Login_InvalidClientInvalidatesDynamicRegistration(t *testing.T) {
	m := newTestManager(t)
	stub := stubInvalidClientAuthServer(t)

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example-resource.test/mcp",
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuth,
			OAuth: &config.MCPOAuthConfig{
				// No static ClientID: seed a persisted (dynamically
				// registered) one directly so OAuthConfig picks it up
				// without needing a real registration endpoint.
				AuthServerMetadataURL: stub.URL + "/as-metadata",
				RedirectURI:           "http://127.0.0.1:0/mcp/oauth/callback",
			},
		},
	}
	if err := m.store.UpdateClientInfo("invalid-client-server", &ClientInfo{ClientID: "stale-dcr-id"}, srv.URL); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	err := runLoginWithSimulatedBrowser(t, m, "invalid-client-server", srv, "", false)
	if err == nil {
		t.Fatal("expected an error from the invalid_client token response")
	}
	if !strings.Contains(err.Error(), "stale") && !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("error = %v, want it to mention the stale registration or invalid_client", err)
	}

	entry, ok, err := m.store.GetForURL("invalid-client-server", srv.URL)
	if err != nil {
		t.Fatalf("GetForURL: %v", err)
	}
	if ok && entry.ClientInfo != nil {
		t.Errorf("ClientInfo = %+v, want nil (invalidated after invalid_client)", entry.ClientInfo)
	}
}

func TestManager_Login_InvalidClientNotInvalidatedWhenClientIDIsStatic(t *testing.T) {
	m := newTestManager(t)
	stub := stubInvalidClientAuthServer(t)

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example-resource.test/mcp",
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuth,
			OAuth: &config.MCPOAuthConfig{
				ClientID:              "static-client-id",
				AuthServerMetadataURL: stub.URL + "/as-metadata",
				RedirectURI:           "http://127.0.0.1:0/mcp/oauth/callback",
			},
		},
	}
	if err := m.store.UpdateClientInfo("static-client-server", &ClientInfo{ClientID: "some-persisted-id"}, srv.URL); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	err := runLoginWithSimulatedBrowser(t, m, "static-client-server", srv, "", false)
	if err == nil {
		t.Fatal("expected an error from the invalid_client token response")
	}

	entry, ok, err := m.store.GetForURL("static-client-server", srv.URL)
	if err != nil || !ok {
		t.Fatalf("GetForURL: ok=%v err=%v", ok, err)
	}
	if entry.ClientInfo == nil || entry.ClientInfo.ClientID != "some-persisted-id" {
		t.Errorf("ClientInfo = %+v, want unchanged (static client_id must not be invalidated)", entry.ClientInfo)
	}
}
