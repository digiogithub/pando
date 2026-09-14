package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// newAuthTestServer builds a Server with the HTTP transport wired (UseStdio:
// false), which is what constructs s.httpServer and its cors->bearer->mux
// handler chain.
func newAuthTestServer(t *testing.T, token string) *Server {
	t.Helper()
	srv := New(Config{
		UseStdio: false,
		Addr:     "127.0.0.1:0",
		Token:    token,
	})
	require.NotNil(t, srv.httpServer, "HTTP transport must be built when UseStdio is false")
	return srv
}

func TestBearerMiddleware_CorrectTokenPassesThrough(t *testing.T) {
	srv := newAuthTestServer(t, "s3cr3t")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := srv.bearerMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, called, "next handler must be invoked with a matching token")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestBearerMiddleware_WrongTokenIsRejected(t *testing.T) {
	srv := newAuthTestServer(t, "s3cr3t")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := srv.bearerMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.False(t, called, "next handler must not run for a wrong token")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestBearerMiddleware_MissingHeaderIsRejected(t *testing.T) {
	srv := newAuthTestServer(t, "s3cr3t")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := srv.bearerMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.False(t, called, "next handler must not run with no Authorization header at all")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestBearerMiddleware_MalformedHeaderIsRejected(t *testing.T) {
	srv := newAuthTestServer(t, "s3cr3t")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler must not run for a malformed Authorization header")
	})
	handler := srv.bearerMiddleware(next)

	cases := map[string]string{
		"no scheme at all":      "s3cr3t",
		"scheme with no token":  "Bearer",
		"wrong scheme":          "Basic s3cr3t",
		"wrong case for Bearer": "bearer s3cr3t",
	}
	for name, authHeader := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", authHeader)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "case %q must be rejected", name)
		})
	}
}

// TestBearerMiddleware_QueryParamTokenIsNotAccepted pins the spec's explicit
// "header only" requirement: unlike internal/api's token check, a token
// placed in the URL must never authenticate a request (query strings end up
// in server logs, browser history and proxies).
func TestBearerMiddleware_QueryParamTokenIsNotAccepted(t *testing.T) {
	srv := newAuthTestServer(t, "s3cr3t")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := srv.bearerMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/mcp?token=s3cr3t", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.False(t, called, "a query-string token must not authenticate the request")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestBearerMiddleware_HealthIsExempt(t *testing.T) {
	srv := newAuthTestServer(t, "s3cr3t")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := srv.bearerMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, called, "/health must not require a token")
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestBearerMiddleware_NoTokenConfiguredIsUnauthenticated pins the backward-
// compatibility behavior for the embedded Mesnada orchestrator server
// (internal/app/app.go), which this story does not touch and which never
// sets Config.Token: an empty token must keep the transport unauthenticated
// exactly as before this change.
func TestBearerMiddleware_NoTokenConfiguredIsUnauthenticated(t *testing.T) {
	srv := newAuthTestServer(t, "")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := srv.bearerMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, called, "an empty Config.Token must leave the transport unauthenticated")
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestHTTPTransport_RequiresBearerOnMCPEndpoints exercises the real handler
// chain (corsMiddleware -> bearerMiddleware -> mux) the way a client actually
// reaches /mcp and /mcp/sse, not just the middleware in isolation.
func TestHTTPTransport_RequiresBearerOnMCPEndpoints(t *testing.T) {
	srv := newAuthTestServer(t, "s3cr3t")

	for _, path := range []string{"/mcp", "/mcp/sse"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			rec := httptest.NewRecorder()
			srv.httpServer.Handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "path %s must require a token", path)
		})
	}
}

func TestHTTPTransport_HealthNeedsNoTokenThroughFullChain(t *testing.T) {
	srv := newAuthTestServer(t, "s3cr3t")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}
