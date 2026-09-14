package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// newCORSTestServer builds a Server with the HTTP transport wired and the
// given origin allow-list; token is left empty since these tests exercise
// corsMiddleware only, which runs outside bearerMiddleware.
func newCORSTestServer(t *testing.T, allowedOrigins []string) *Server {
	t.Helper()
	srv := New(Config{
		UseStdio:       false,
		Addr:           "127.0.0.1:0",
		AllowedOrigins: allowedOrigins,
	})
	require.NotNil(t, srv.httpServer)
	return srv
}

func TestCORS_EmptyAllowListEmitsNoHeadersOnNormalRequest(t *testing.T) {
	srv := newCORSTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Methods"))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Headers"))
	require.Empty(t, rec.Header().Get("Vary"))
	require.Equal(t, http.StatusOK, rec.Code, "the request itself still succeeds; CORS is a browser-side gate, not a server-side block on GET/POST")
}

func TestCORS_EmptyAllowListRefusesPreflight(t *testing.T) {
	srv := newCORSTestServer(t, nil)

	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_EmptyAllowListRefusesPreflightEvenWithNoOrigin(t *testing.T) {
	srv := newCORSTestServer(t, nil)

	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCORS_MatchingOriginIsEchoedWithVary(t *testing.T) {
	srv := newCORSTestServer(t, []string{"https://app.example.com", "http://localhost:5173"})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	require.Equal(t, "http://localhost:5173", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "Origin", rec.Header().Get("Vary"))
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"), "credentials must never accompany an echoed origin")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestCORS_MatchingOriginPreflightSucceeds(t *testing.T) {
	srv := newCORSTestServer(t, []string{"https://app.example.com"})

	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "https://app.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "Origin", rec.Header().Get("Vary"))
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Authorization")
}

func TestCORS_NonMatchingOriginGetsNoHeaders(t *testing.T) {
	srv := newCORSTestServer(t, []string{"https://app.example.com"})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://not-allowed.example.com")
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, rec.Header().Get("Vary"))
}

func TestCORS_NonMatchingOriginPreflightIsRefused(t *testing.T) {
	srv := newCORSTestServer(t, []string{"https://app.example.com"})

	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Header.Set("Origin", "https://not-allowed.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_NoWildcardAnywhereInResponses(t *testing.T) {
	srv := newCORSTestServer(t, []string{"https://app.example.com"})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	for _, h := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
		"Access-Control-Expose-Headers",
	} {
		require.NotEqual(t, "*", rec.Header().Get(h), "header %s must never be a bare wildcard", h)
	}
}
