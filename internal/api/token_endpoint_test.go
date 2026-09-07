package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// The token endpoint is what turns a login into the credential every other
// /api/ path accepts, so it must never be the one path that needs no
// credential at all. Basic auth covers it once credentials are configured; the
// case below is the one that was open: an exposed server with none.

// tokenStack builds the real middleware stack over the token endpoint.
func tokenStack(s *Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(TokenPath, s.handleToken)
	return s.corsMiddleware(s.basicAuthMiddleware(s.authMiddleware(mux)))
}

// TestExposedServerWithNoCredentialRefusesToMintATokenIs the defect: with no
// [Server.BasicAuth] user configured, basic auth is not enforced, and the token
// endpoint was exempt from the token check, so anything that could reach the
// server could ask for the key to it.
func TestExposedServerWithNoCredentialRefusesToMintAToken(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", false)

	req := httptest.NewRequest(http.MethodPost, TokenPath, nil)
	req.RemoteAddr = "192.168.1.20:5000"
	rec := httptest.NewRecorder()
	tokenStack(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an exposed server handed out its API token: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), s.token) {
		t.Fatal("the refusal carried the token")
	}
	if !strings.Contains(rec.Body.String(), "BasicAuth") {
		t.Errorf("the refusal does not say what is missing: %s", rec.Body.String())
	}
}

// TestTokenHolderCanStillRefreshOnAnExposedServer: closing the hole must not
// strand a client that already authenticated.
func TestTokenHolderCanStillRefreshOnAnExposedServer(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", false)

	req := httptest.NewRequest(http.MethodPost, TokenPath, nil)
	req.RemoteAddr = "192.168.1.20:5000"
	req.Header.Set("X-Pando-Token", s.token)
	rec := httptest.NewRecorder()
	tokenStack(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("a token holder was refused its own token: %d %s", rec.Code, rec.Body.String())
	}
}

// TestLoopbackServerStillHandsOutItsToken: on a loopback bind the token is
// already readable by the same user from the state directory, and the local Web
// UI fetches it on every load. Gating it there would protect nothing and break
// `pando app`.
func TestLoopbackServerStillHandsOutItsToken(t *testing.T) {
	s := withBasicAuth(t, "127.0.0.1", false)

	rec := httptest.NewRecorder()
	tokenStack(s).ServeHTTP(rec, request(TokenPath, "127.0.0.1:5000"))

	if rec.Code != http.StatusOK {
		t.Fatalf("a loopback server refused its own token: %d %s", rec.Code, rec.Body.String())
	}
}

// TestExposedServerWithCredentialsMintsAfterBasicAuth: the configured path is
// unchanged, which is what keeps the fix narrow.
func TestExposedServerWithCredentialsMintsAfterBasicAuth(t *testing.T) {
	s := withBasicAuth(t, "0.0.0.0", true, config.BasicAuthUser{Username: "admin", Password: "secret"})

	req := httptest.NewRequest(http.MethodPost, TokenPath, nil)
	req.RemoteAddr = "192.168.1.20:5000"
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	tokenStack(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("a credentialed caller was refused: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), s.token) {
		t.Fatal("the token endpoint returned no token to a credentialed caller")
	}
}
