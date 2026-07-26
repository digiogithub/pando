package mcpauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// stubIssAuthServer is like stubAuthServer but additionally advertises
// authorization_response_iss_parameter_supported and counts /token hits, so
// tests can assert the token endpoint is never reached when iss validation
// aborts the flow first.
func stubIssAuthServer(t *testing.T, issSupported bool) (srv *httptest.Server, tokenCalls *int32) {
	t.Helper()
	mux := http.NewServeMux()
	var calls int32
	var s *httptest.Server

	mux.HandleFunc("/as-metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                   s.URL,
			"authorization_endpoint":   s.URL + "/authorize",
			"token_endpoint":           s.URL + "/token",
			"response_types_supported": []string{"code"},
			"authorization_response_iss_parameter_supported": issSupported,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "stub-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})

	s = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s, &calls
}

// runLoginWithSimulatedBrowser drives m.Login using the real local callback
// server (not ManualCode), simulating the browser by GET-ing the redirect
// URI with the given iss value (or omitting it if issParam == "") once the
// authorization URL is available.
func runLoginWithSimulatedBrowser(t *testing.T, m *Manager, serverName string, srv config.MCPServer, issParam string, sendIss bool) error {
	t.Helper()

	authURLCh := make(chan string, 1)
	prompt := LoginPrompt{
		OnAuthorizationURL: func(u string) { authURLCh <- u },
		Timeout:            5 * time.Second,
	}

	errCh := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		errCh <- m.Login(ctx, serverName, srv, prompt)
	}()

	var authURL string
	select {
	case authURL = <-authURLCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for authorization URL")
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	redirectURI := u.Query().Get("redirect_uri")
	state := u.Query().Get("state")

	callbackURL := fmt.Sprintf("%s?code=stub-authorization-code&state=%s", redirectURI, url.QueryEscape(state))
	if sendIss {
		callbackURL += "&iss=" + url.QueryEscape(issParam)
	}

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("simulate browser GET callback: %v", err)
	}
	defer resp.Body.Close()

	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not return")
	}
	return nil
}

func TestManager_Login_IssMatchSucceeds(t *testing.T) {
	m := newTestManager(t)
	stub, tokenCalls := stubIssAuthServer(t, true)

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

	err := runLoginWithSimulatedBrowser(t, m, "iss-match-server", srv, stub.URL, true)
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if atomic.LoadInt32(tokenCalls) != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", atomic.LoadInt32(tokenCalls))
	}
	if !m.HasTokens("iss-match-server", srv.URL) {
		t.Fatal("expected tokens to be persisted")
	}

	entry, ok, err := m.store.GetForURL("iss-match-server", srv.URL)
	if err != nil || !ok {
		t.Fatalf("GetForURL: ok=%v err=%v", ok, err)
	}
	if entry.Metadata == nil || entry.Metadata.Issuer != stub.URL {
		t.Errorf("Metadata = %+v, want Issuer %q", entry.Metadata, stub.URL)
	}
	if !entry.Metadata.AuthorizationResponseIssParameterSupported {
		t.Errorf("AuthorizationResponseIssParameterSupported = false, want true")
	}
}

func TestManager_Login_IssMismatchAbortsBeforeTokenExchange(t *testing.T) {
	m := newTestManager(t)
	stub, tokenCalls := stubIssAuthServer(t, true)

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

	err := runLoginWithSimulatedBrowser(t, m, "iss-mismatch-server", srv, "https://attacker.example.com", true)
	if err == nil {
		t.Fatal("expected an error on iss mismatch")
	}
	if !strings.Contains(err.Error(), "mix-up") {
		t.Errorf("error = %v, want it to mention the possible mix-up attack", err)
	}
	if atomic.LoadInt32(tokenCalls) != 0 {
		t.Fatalf("token endpoint calls = %d, want 0 (must abort before exchange)", atomic.LoadInt32(tokenCalls))
	}
	if m.HasTokens("iss-mismatch-server", srv.URL) {
		t.Fatal("no tokens should have been persisted on iss mismatch")
	}
}

func TestManager_Login_IssRequiredButMissingAborts(t *testing.T) {
	m := newTestManager(t)
	stub, tokenCalls := stubIssAuthServer(t, true)

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

	err := runLoginWithSimulatedBrowser(t, m, "iss-missing-server", srv, "", false)
	if err == nil {
		t.Fatal("expected an error when iss is required but missing")
	}
	if atomic.LoadInt32(tokenCalls) != 0 {
		t.Fatalf("token endpoint calls = %d, want 0", atomic.LoadInt32(tokenCalls))
	}
}

func TestManager_Login_IssNotSupportedAndAbsent_Proceeds(t *testing.T) {
	m := newTestManager(t)
	stub, tokenCalls := stubIssAuthServer(t, false)

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

	err := runLoginWithSimulatedBrowser(t, m, "iss-not-supported-server", srv, "", false)
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if atomic.LoadInt32(tokenCalls) != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", atomic.LoadInt32(tokenCalls))
	}
}
