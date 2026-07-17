package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// withBasicAuth points the global config at an in-memory config carrying the
// given credentials and returns a Server bound to bindHost.
func withBasicAuth(t *testing.T, bindHost string, enabled bool, users ...config.BasicAuthUser) *Server {
	t.Helper()

	prev := config.Get()
	config.SetForTests(&config.Config{
		Server: config.APIServerConfig{
			Enabled: true,
			Host:    bindHost,
			Port:    9999,
			BasicAuth: config.BasicAuthConfig{
				Enabled: enabled,
				Users:   users,
			},
		},
	})
	t.Cleanup(func() { config.SetForTests(prev) })

	return &Server{config: ServerConfig{Host: bindHost}, token: "test-token"}
}

// guarded returns a handler with the basic-auth middleware in front of a 200 OK.
func guarded(s *Server) http.Handler {
	return s.basicAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func request(path, remoteAddr string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.RemoteAddr = remoteAddr
	return req
}

func TestBasicAuthChallengesRemoteClients(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", true, config.BasicAuthUser{Username: "admin", Password: "secret"})

	rec := httptest.NewRecorder()
	guarded(s).ServeHTTP(rec, request("/api/v1/token", "192.168.1.20:5000"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without credentials, got %d", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("expected a WWW-Authenticate challenge for a non-web client")
	}
}

func TestBasicAuthAcceptsValidCredentials(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", true, config.BasicAuthUser{Username: "admin", Password: "secret"})

	req := request("/api/v1/token", "192.168.1.20:5000")
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	guarded(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid credentials, got %d", rec.Code)
	}
}

func TestBasicAuthRejectsWrongPassword(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", true, config.BasicAuthUser{Username: "admin", Password: "secret"})

	req := request("/api/v1/token", "192.168.1.20:5000")
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()
	guarded(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a wrong password, got %d", rec.Code)
	}
}

func TestBasicAuthSuppressesChallengeForWebClient(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", true, config.BasicAuthUser{Username: "admin", Password: "secret"})

	req := request("/api/v1/token", "192.168.1.20:5000")
	req.Header.Set(webClientHeader, "web")
	rec := httptest.NewRecorder()
	guarded(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("expected no native challenge for the SPA, got %q", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "basic_auth_required") {
		t.Fatalf("expected a basic_auth_required marker for the SPA, got %q", body)
	}
}

// A server on 0.0.0.0 is reachable from the network, so being local buys the
// caller nothing: the local browser must authenticate like everyone else.
func TestBasicAuthChallengesLocalRequestsOnExposedServer(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", true, config.BasicAuthUser{Username: "admin", Password: "secret"})

	for _, addr := range []string{"127.0.0.1:5000", "[::1]:5000"} {
		rec := httptest.NewRecorder()
		guarded(s).ServeHTTP(rec, request("/api/v1/token", addr))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected local request from %s to be challenged, got %d", addr, rec.Code)
		}
	}
}

// Bound to localhost the server is unreachable from the network, so basic auth
// stays inert and local development is unaffected.
func TestBasicAuthSkipsLoopbackBind(t *testing.T) {
	for _, bind := range []string{"localhost", "127.0.0.1", "::1"} {
		s := withBasicAuth(t, bind, true, config.BasicAuthUser{Username: "admin", Password: "secret"})

		rec := httptest.NewRecorder()
		guarded(s).ServeHTTP(rec, request("/api/v1/token", "127.0.0.1:5000"))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected no challenge while bound to %s, got %d", bind, rec.Code)
		}
	}
}

func TestBasicAuthSkipsHealthAndStaticPaths(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", true, config.BasicAuthUser{Username: "admin", Password: "secret"})

	for _, path := range []string{"/health", "/", "/assets/index.js"} {
		rec := httptest.NewRecorder()
		guarded(s).ServeHTTP(rec, request(path, "192.168.1.20:5000"))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %s to stay public, got %d", path, rec.Code)
		}
	}
}

func TestBasicAuthInertWhenDisabledOrEmpty(t *testing.T) {
	disabled := withBasicAuth(t, "0.0.0.0", false, config.BasicAuthUser{Username: "admin", Password: "secret"})
	rec := httptest.NewRecorder()
	guarded(disabled).ServeHTTP(rec, request("/api/v1/token", "192.168.1.20:5000"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected no challenge while disabled, got %d", rec.Code)
	}

	noUsers := withBasicAuth(t, "0.0.0.0", true)
	rec = httptest.NewRecorder()
	guarded(noUsers).ServeHTTP(rec, request("/api/v1/token", "192.168.1.20:5000"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected no challenge without users, got %d", rec.Code)
	}
}

// TestTokenEndpointIsGatedEndToEnd drives the real middleware stack over a real
// HTTP listener: the token is what unlocks the whole API, so handing it out must
// itself require credentials once the server is exposed.
func TestTokenEndpointIsGatedEndToEnd(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", true, config.BasicAuthUser{Username: "admin", Password: "secret"})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", s.handleToken)
	mux.HandleFunc("/api/v1/project", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	stack := s.corsMiddleware(s.basicAuthMiddleware(s.authMiddleware(mux)))

	srv := httptest.NewServer(stack)
	t.Cleanup(srv.Close)

	// httptest dials over 127.0.0.1, i.e. the local browser `pando app` opens.
	// The server is bound to 0.0.0.0, so it is challenged like any other client.
	resp, err := srv.Client().Post(srv.URL+"/api/v1/token", "application/json", nil)
	if err != nil {
		t.Fatalf("token request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected a local token fetch on an exposed server to be challenged, got %d", resp.StatusCode)
	}

	// A remote caller with no credentials gets nothing — this is the hole the
	// feature closes: /api/v1/token used to hand out full access to anyone.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/token", nil)
	req.RemoteAddr = "192.168.1.20:5000"
	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected remote token fetch to be rejected, got %d", rec.Code)
	}

	// With credentials the caller gets the token.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/token", nil)
	req.RemoteAddr = "192.168.1.20:5000"
	req.SetBasicAuth("admin", "secret")
	rec = httptest.NewRecorder()
	stack.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected an authenticated token fetch to succeed, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), s.token) {
		t.Fatalf("expected the token in the response, got %q", rec.Body.String())
	}

	// The token is the session credential: once obtained it stands on its own, so
	// the SPA (and SSE streams, which can only pass ?token=) keep working without
	// replaying the password on every call.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/project", nil)
	req.RemoteAddr = "192.168.1.20:5000"
	req.Header.Set("X-Pando-Token", s.token)
	rec = httptest.NewRecorder()
	stack.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected the token to unlock the API, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/project?token="+s.token, nil)
	req.RemoteAddr = "192.168.1.20:5000"
	rec = httptest.NewRecorder()
	stack.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected a query-param token (SSE) to work, got %d", rec.Code)
	}

	// A wrong token is still rejected by the basic-auth gate.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/project", nil)
	req.RemoteAddr = "192.168.1.20:5000"
	req.Header.Set("X-Pando-Token", "not-the-token")
	rec = httptest.NewRecorder()
	stack.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected a bogus token to be rejected, got %d", rec.Code)
	}
}

func TestGenerateTokenIsRandom(t *testing.T) {
	first, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	second, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if first == second {
		t.Fatal("generateToken returned the same token twice")
	}
	if len(first) != 64 {
		t.Fatalf("expected a 32-byte hex token, got %d chars", len(first))
	}
}

func TestPreserveBasicAuthKeepsCredentials(t *testing.T) {
	existing := config.APIServerConfig{
		BasicAuth: config.BasicAuthConfig{
			Enabled: true,
			Users:   []config.BasicAuthUser{{Username: "admin", Password: "secret"}},
		},
	}
	// The services form round-trips masked passwords and older clients omit the
	// block entirely; neither may wipe the stored credentials.
	incoming := config.APIServerConfig{Host: "0.0.0.0", Port: 8080}

	merged := preserveBasicAuth(incoming, existing)

	if merged.Host != "0.0.0.0" || merged.Port != 8080 {
		t.Fatal("expected non-credential fields to come from the incoming payload")
	}
	if !merged.BasicAuth.Enabled || len(merged.BasicAuth.Users) != 1 {
		t.Fatal("expected stored credentials to survive a services save")
	}
	if merged.BasicAuth.Users[0].Password != "secret" {
		t.Fatalf("expected the stored password, got %q", merged.BasicAuth.Users[0].Password)
	}
}

func TestMaskBasicAuthPasswords(t *testing.T) {
	server := config.APIServerConfig{
		BasicAuth: config.BasicAuthConfig{
			Users: []config.BasicAuthUser{{Username: "admin", Password: "secret"}},
		},
	}

	masked := maskBasicAuthPasswords(server)

	if masked.BasicAuth.Users[0].Password != maskedPassword {
		t.Fatal("expected the password to be masked in API responses")
	}
	if server.BasicAuth.Users[0].Password != "secret" {
		t.Fatal("masking must not mutate the source config")
	}
}
