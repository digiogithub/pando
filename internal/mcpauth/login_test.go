package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// stubAuthServer builds an httptest.Server acting as both the OAuth
// authorization server (metadata + token endpoint) and, implicitly, the
// resource — Login is pointed at it directly via AuthServerMetadataURL, so
// no separate protected-resource-metadata round trip is needed.
//
// withRegistration controls whether the served metadata advertises a
// registration_endpoint, so tests can exercise both the "static client_id"
// and the "no dynamic registration configured" paths.
func stubAuthServer(t *testing.T, withRegistration bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	var srv *httptest.Server
	mux.HandleFunc("/as-metadata", func(w http.ResponseWriter, r *http.Request) {
		meta := map[string]any{
			"issuer":                   srv.URL,
			"authorization_endpoint":   srv.URL + "/authorize",
			"token_endpoint":           srv.URL + "/token",
			"response_types_supported": []string{"code"},
		}
		if withRegistration {
			meta["registration_endpoint"] = srv.URL + "/register"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(meta)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			http.Error(w, "unexpected grant_type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "stub-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestManager_Login_HappyPath(t *testing.T) {
	m := newTestManager(t)
	stub := stubAuthServer(t, false /* client_id supplied statically, no DCR needed */)

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

	var capturedAuthURL string
	prompt := LoginPrompt{
		OnAuthorizationURL: func(u string) { capturedAuthURL = u },
		OpenBrowser:        false,
		ManualCode: func(ctx context.Context) (string, string, string, error) {
			u, err := url.Parse(capturedAuthURL)
			if err != nil {
				return "", "", "", err
			}
			return "stub-authorization-code", u.Query().Get("state"), "", nil
		},
		Timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := m.Login(ctx, "test-server", srv, prompt); err != nil {
		t.Fatalf("Login returned error: %v", err)
	}

	if capturedAuthURL == "" {
		t.Fatal("OnAuthorizationURL was never called")
	}
	if !strings.Contains(capturedAuthURL, stub.URL+"/authorize") {
		t.Fatalf("authorization URL = %q, want it to point at the stub authorize endpoint", capturedAuthURL)
	}

	if !m.HasTokens("test-server", srv.URL) {
		t.Fatal("expected tokens to be persisted after a successful login")
	}

	entry, ok, err := m.store.GetForURL("test-server", srv.URL)
	if err != nil || !ok {
		t.Fatalf("GetForURL: ok=%v err=%v", ok, err)
	}
	if entry.Tokens == nil || entry.Tokens.AccessToken != "stub-access-token" {
		t.Fatalf("persisted tokens = %+v, want access token stub-access-token", entry.Tokens)
	}
}

func TestManager_Login_NoClientIDNoDynamicRegistration(t *testing.T) {
	m := newTestManager(t)
	// withRegistration=false: the stub AS metadata omits registration_endpoint,
	// so RegisterClient must fail because dynamic client registration isn't
	// supported by this (stub) authorization server.
	stub := stubAuthServer(t, false)

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example-resource.test/mcp",
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuth,
			OAuth: &config.MCPOAuthConfig{
				// No ClientID: Login must attempt dynamic registration and
				// fail with an actionable error.
				AuthServerMetadataURL: stub.URL + "/as-metadata",
				RedirectURI:           "http://127.0.0.1:0/mcp/oauth/callback",
			},
		},
	}

	err := m.Login(context.Background(), "test-server", srv, LoginPrompt{})
	if err == nil {
		t.Fatal("expected an error when no client_id is configured and DCR is unsupported")
	}
	if !strings.Contains(err.Error(), "ClientID") {
		t.Fatalf("error = %v, want it to mention Auth.OAuth.ClientID", err)
	}
	if m.HasTokens("test-server", srv.URL) {
		t.Fatal("no tokens should have been persisted on failure")
	}
}

func TestManager_Login_RejectsStdioServer(t *testing.T) {
	m := newTestManager(t)
	srv := config.MCPServer{Type: config.MCPStdio, Command: "some-binary"}
	err := m.Login(context.Background(), "test-server", srv, LoginPrompt{})
	if err == nil {
		t.Fatal("expected an error for a stdio server")
	}
	if !strings.Contains(err.Error(), "stdio") {
		t.Fatalf("error = %v, want it to mention stdio", err)
	}
}

func TestManager_Login_RejectsNonOAuthServer(t *testing.T) {
	m := newTestManager(t)
	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example.test/mcp",
		Auth: &config.MCPAuth{Type: config.MCPAuthBearer, Token: "tok"},
	}
	err := m.Login(context.Background(), "test-server", srv, LoginPrompt{})
	if err == nil {
		t.Fatal("expected an error for a non-oauth server")
	}
	if !strings.Contains(err.Error(), "oauth") {
		t.Fatalf("error = %v, want it to mention oauth", err)
	}
}

func TestManager_Status(t *testing.T) {
	m := newTestManager(t)
	srv := oauthServer("https://example.test/mcp", &config.MCPOAuthConfig{ClientID: "cfg-client"})

	// No tokens yet.
	info := m.Status("srv", srv)
	if info.HasTokens {
		t.Fatal("expected HasTokens=false before any login")
	}
	if info.AuthType != string(config.MCPAuthOAuth) {
		t.Fatalf("AuthType = %q, want oauth", info.AuthType)
	}

	// Persist a token directly and a dynamically-registered client id
	// (distinct from the configured one) to exercise both flags.
	if err := m.store.UpdateTokens("srv", &Tokens{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour),
	}, srv.URL); err != nil {
		t.Fatalf("UpdateTokens: %v", err)
	}

	info = m.Status("srv", srv)
	if !info.HasTokens {
		t.Fatal("expected HasTokens=true after UpdateTokens")
	}
	if info.Expired {
		t.Fatal("token expiring in an hour should not be reported as expired")
	}

	// Now simulate an expired token.
	if err := m.store.UpdateTokens("srv", &Tokens{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(-time.Hour),
	}, srv.URL); err != nil {
		t.Fatalf("UpdateTokens: %v", err)
	}
	info = m.Status("srv", srv)
	if !info.Expired {
		t.Fatal("expected Expired=true for a past ExpiresAt")
	}

	// DynamicallyRegistered should be false when the persisted ClientID
	// matches what static config would have supplied anyway... but since
	// static config already has a ClientID ("cfg-client"), OAuthConfig would
	// never have needed to persist a different one via DCR. Simulate the
	// server having a different persisted ClientID than static config to
	// mark it as dynamically registered.
	if err := m.store.UpdateClientInfo("srv", &ClientInfo{ClientID: "dcr-client"}, srv.URL); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}
	srvNoStaticClientID := oauthServer("https://example.test/mcp", &config.MCPOAuthConfig{})
	info = m.Status("srv", srvNoStaticClientID)
	if !info.DynamicallyRegistered {
		t.Fatal("expected DynamicallyRegistered=true when static config has no client_id but one is persisted")
	}
	if info.ClientID != "dcr-client" {
		t.Fatalf("ClientID = %q, want dcr-client", info.ClientID)
	}
}

func TestNonInteractiveLoginError(t *testing.T) {
	err := NonInteractiveLoginError("my-server")
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	if !strings.Contains(err.Error(), "pando mcp login my-server") {
		t.Fatalf("error = %v, want it to name the exact command", err)
	}
}

func TestIsInteractive_DoesNotPanic(t *testing.T) {
	// In CI/test environments stdin/stdout are typically not a TTY, so this
	// just needs to return a bool without panicking; the actual TTY-bound
	// behavior is exercised manually.
	_ = IsInteractive()
}
