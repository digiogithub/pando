package mcpauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

func TestCanonicalResourceURI(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"trailing slash removed", "https://example.com/mcp/", "https://example.com/mcp", false},
		{"root path preserved", "https://example.com/", "https://example.com/", false},
		{"no path", "https://example.com", "https://example.com", false},
		{"fragment dropped", "https://example.com/mcp#section", "https://example.com/mcp", false},
		{"uppercase host lowered", "https://EXAMPLE.com/mcp", "https://example.com/mcp", false},
		{"default https port stripped", "https://example.com:443/mcp", "https://example.com/mcp", false},
		{"default http port stripped", "http://example.com:80/mcp", "http://example.com/mcp", false},
		{"non-default port preserved", "https://example.com:8443/mcp", "https://example.com:8443/mcp", false},
		{"scheme lowered", "HTTPS://example.com/mcp", "https://example.com/mcp", false},
		{"query dropped", "https://example.com/mcp?x=1", "https://example.com/mcp", false},
		{"path preserved with multiple segments", "https://example.com/a/b/c/", "https://example.com/a/b/c", false},
		{"no scheme errors", "example.com/mcp", "", true},
		{"no host errors", "https:///mcp", "", true},
		{"empty errors", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalResourceURI(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("CanonicalResourceURI(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalResourceURI(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("CanonicalResourceURI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// clientCredentialsTestServer wires up a minimal RFC 8414 metadata endpoint
// plus a token endpoint that records the last request body and answers
// according to tokenHandler.
type clientCredentialsTestServer struct {
	*httptest.Server
	lastRequestForm atomic.Pointer[url.Values]
	requestCount    atomic.Int64
}

func newClientCredentialsTestServer(t *testing.T, tokenHandler func(w http.ResponseWriter, form url.Values)) *clientCredentialsTestServer {
	t.Helper()
	cs := &clientCredentialsTestServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issuer":"%s","token_endpoint":"%s/token","authorization_endpoint":"%s/authorize"}`,
			cs.URLPlaceholder(), cs.URLPlaceholder(), cs.URLPlaceholder())
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		cs.requestCount.Add(1)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		form := r.PostForm
		cs.lastRequestForm.Store(&form)
		tokenHandler(w, form)
	})

	ts := httptest.NewServer(mux)
	cs.Server = ts
	return cs
}

// URLPlaceholder is replaced after the server starts (issuer/token_endpoint
// need the server's own URL, which isn't known until httptest.NewServer
// returns); this returns a value that self-corrects via the closure below.
func (cs *clientCredentialsTestServer) URLPlaceholder() string {
	if cs.Server == nil {
		return ""
	}
	return cs.Server.URL
}

func TestMintClientCredentialsToken_SendsExpectedFields(t *testing.T) {
	var gotForm url.Values
	ts := newClientCredentialsTestServer(t, func(w http.ResponseWriter, form url.Values) {
		gotForm = form
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok-123","token_type":"Bearer","expires_in":3600}`)
	})
	defer ts.Close()

	tok, err := mintClientCredentialsToken(context.Background(), ClientCredentialsParams{
		TokenEndpoint: ts.URL + "/token",
		ClientID:      "my-client",
		ClientSecret:  "my-secret",
		Scopes:        []string{"read", "write"},
		Resource:      "https://example.com/mcp",
	})
	if err != nil {
		t.Fatalf("mintClientCredentialsToken: %v", err)
	}
	if tok.AccessToken != "tok-123" {
		t.Fatalf("AccessToken = %q, want tok-123", tok.AccessToken)
	}
	if tok.ExpiresAt.IsZero() {
		t.Fatal("expected ExpiresAt to be set from expires_in")
	}

	if gotForm.Get("grant_type") != "client_credentials" {
		t.Fatalf("grant_type = %q, want client_credentials", gotForm.Get("grant_type"))
	}
	if gotForm.Get("client_id") != "my-client" {
		t.Fatalf("client_id = %q, want my-client", gotForm.Get("client_id"))
	}
	if gotForm.Get("client_secret") != "my-secret" {
		t.Fatalf("client_secret = %q, want my-secret", gotForm.Get("client_secret"))
	}
	if gotForm.Get("scope") != "read write" {
		t.Fatalf("scope = %q, want %q", gotForm.Get("scope"), "read write")
	}
	if gotForm.Get("resource") != "https://example.com/mcp" {
		t.Fatalf("resource = %q, want https://example.com/mcp", gotForm.Get("resource"))
	}
}

func TestMintClientCredentialsToken_InvalidClientError(t *testing.T) {
	ts := newClientCredentialsTestServer(t, func(w http.ResponseWriter, form url.Values) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid_client","error_description":"client authentication failed"}`)
	})
	defer ts.Close()

	_, err := mintClientCredentialsToken(context.Background(), ClientCredentialsParams{
		TokenEndpoint: ts.URL + "/token",
		ClientID:      "bad-client",
		ClientSecret:  "bad-secret",
	})
	if err == nil {
		t.Fatal("expected an error for a 401 invalid_client response")
	}
	if !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("expected error to mention invalid_client, got: %v", err)
	}
}

func TestMintClientCredentialsToken_MissingTokenEndpoint(t *testing.T) {
	_, err := mintClientCredentialsToken(context.Background(), ClientCredentialsParams{
		ClientID:     "id",
		ClientSecret: "secret",
	})
	if err == nil {
		t.Fatal("expected an error when TokenEndpoint is empty")
	}
}

func TestLoginClientCredentials_PersistsToken(t *testing.T) {
	ts := newClientCredentialsTestServer(t, func(w http.ResponseWriter, form url.Values) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok-abc","token_type":"Bearer","expires_in":3600}`)
	})
	defer ts.Close()

	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	mgr := NewManager(store)

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  ts.URL,
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuthClientCredentials,
			OAuth: &config.MCPOAuthConfig{
				ClientID:     "my-client",
				ClientSecret: "my-secret",
			},
		},
	}

	if err := mgr.LoginClientCredentials(context.Background(), "srv", srv); err != nil {
		t.Fatalf("LoginClientCredentials: %v", err)
	}

	entry, ok, err := store.GetForURL("srv", ts.URL)
	if err != nil {
		t.Fatalf("GetForURL: %v", err)
	}
	if !ok || entry.Tokens == nil || entry.Tokens.AccessToken != "tok-abc" {
		t.Fatalf("expected persisted token tok-abc, got %#v", entry)
	}

	if !mgr.HasTokens("srv", ts.URL) {
		t.Fatal("expected HasTokens() to be true after LoginClientCredentials")
	}
}

func TestLoginClientCredentials_RejectsWrongAuthType(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	mgr := NewManager(store)
	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example.com",
		Auth: &config.MCPAuth{Type: config.MCPAuthBearer, Token: "x"},
	}
	if err := mgr.LoginClientCredentials(context.Background(), "srv", srv); err == nil {
		t.Fatal("expected an error for a non-client_credentials auth type")
	}
}

func TestClientCredentialsTokenSource_RemintsOnExpiry(t *testing.T) {
	var issueCount atomic.Int64
	ts := newClientCredentialsTestServer(t, func(w http.ResponseWriter, form url.Values) {
		n := issueCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		// The first token is already expired (expires_in=0 with an explicit
		// past ExpiresAt isn't expressible via this JSON shape, so instead
		// issue a token with a 1-second lifetime and let the leeway window
		// force an immediate re-mint on the second call).
		fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"Bearer","expires_in":1}`, n)
	})
	defer ts.Close()

	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	source := &clientCredentialsTokenSource{
		store:      store,
		serverName: "srv",
		serverURL:  ts.URL,
		params: ClientCredentialsParams{
			TokenEndpoint: ts.URL + "/token",
			ClientID:      "id",
			ClientSecret:  "secret",
		},
	}

	tok1, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken (1st): %v", err)
	}
	if tok1 != "tok-1" {
		t.Fatalf("tok1 = %q, want tok-1", tok1)
	}

	// The token's 1s lifetime is well within clientCredentialsExpiryLeeway
	// (30s), so the very next call must re-mint rather than reuse it.
	tok2, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken (2nd): %v", err)
	}
	if tok2 != "tok-2" {
		t.Fatalf("tok2 = %q, want tok-2 (expected a re-mint)", tok2)
	}
	if issueCount.Load() != 2 {
		t.Fatalf("expected 2 token requests, got %d", issueCount.Load())
	}
}

func TestClientCredentialsTokenSource_ReusesValidToken(t *testing.T) {
	var issueCount atomic.Int64
	ts := newClientCredentialsTestServer(t, func(w http.ResponseWriter, form url.Values) {
		issueCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok-long","token_type":"Bearer","expires_in":3600}`)
	})
	defer ts.Close()

	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	source := &clientCredentialsTokenSource{
		store:      store,
		serverName: "srv",
		serverURL:  ts.URL,
		params: ClientCredentialsParams{
			TokenEndpoint: ts.URL + "/token",
			ClientID:      "id",
			ClientSecret:  "secret",
		},
	}

	for i := 0; i < 3; i++ {
		tok, err := source.AccessToken(context.Background())
		if err != nil {
			t.Fatalf("AccessToken (%d): %v", i, err)
		}
		if tok != "tok-long" {
			t.Fatalf("call %d: tok = %q, want tok-long", i, tok)
		}
	}
	if issueCount.Load() != 1 {
		t.Fatalf("expected exactly 1 token request across 3 calls, got %d", issueCount.Load())
	}
}

func TestBearerRoundTripper_AttachesAuthorizationHeader(t *testing.T) {
	var gotAuth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	if err := store.UpdateTokens("srv", &Tokens{AccessToken: "static-tok", ExpiresAt: time.Now().Add(time.Hour)}, backend.URL); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	source := &clientCredentialsTokenSource{store: store, serverName: "srv", serverURL: backend.URL}

	rt := &bearerRoundTripper{source: source}
	client := &http.Client{Transport: rt}

	resp, err := client.Get(backend.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	if gotAuth != "Bearer static-tok" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer static-tok")
	}
}

func TestClientCredentialsTransport_RejectsWrongAuthType(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	mgr := NewManager(store)
	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example.com",
		Auth: &config.MCPAuth{Type: config.MCPAuthOAuth},
	}
	if _, err := mgr.ClientCredentialsTransport(context.Background(), "srv", srv, nil); err == nil {
		t.Fatal("expected an error for a non-client_credentials auth type")
	}
}

func TestClientCredentialsTransport_EndToEnd(t *testing.T) {
	var gotAuth string
	ts := newClientCredentialsTestServer(t, func(w http.ResponseWriter, form url.Values) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"tok-e2e","token_type":"Bearer","expires_in":3600}`)
	})
	defer ts.Close()

	// A separate "resource" backend the bearerRoundTripper actually calls,
	// so we can assert the Authorization header made it all the way
	// through the RoundTripper chain built by ClientCredentialsTransport.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	mgr := NewManager(store)

	// The discovery handler will look for metadata at
	// ts.URL/.well-known/oauth-authorization-server, so point the server's
	// own "base URL" at ts.URL for discovery purposes, then issue the
	// actual authenticated request against backend.
	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  ts.URL,
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuthClientCredentials,
			OAuth: &config.MCPOAuthConfig{
				ClientID:     "id",
				ClientSecret: "secret",
			},
		},
	}

	rt, err := mgr.ClientCredentialsTransport(context.Background(), "srv", srv, nil)
	if err != nil {
		t.Fatalf("ClientCredentialsTransport: %v", err)
	}
	client := &http.Client{Transport: rt}

	resp, err := client.Get(backend.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if gotAuth != "Bearer tok-e2e" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer tok-e2e")
	}

	// The token should now be persisted for "srv"/ts.URL.
	if !mgr.HasTokens("srv", ts.URL) {
		t.Fatal("expected the minted token to be persisted")
	}
}
