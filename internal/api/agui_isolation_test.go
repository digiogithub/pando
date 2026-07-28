package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digiogithub/pando/internal/agui"
)

// These tests pin the isolation contract of the AG-UI adapter: when it is
// disabled it is invisible to the API server, and when it is enabled it is
// excluded from the server-wide CORS and token middleware so it can enforce its
// own stricter policy.

func TestAGUIPathDetectionDisabled(t *testing.T) {
	s := &Server{}
	if s.isAGUIPath("/api/v1/agui/coder") {
		t.Fatal("no AG-UI path may match while the adapter is disabled")
	}
}

func TestAGUIPathDetection(t *testing.T) {
	s := &Server{agui: &agui.Runtime{}, aguiPath: "/api/v1/agui"}

	for _, p := range []string{"/api/v1/agui", "/api/v1/agui/coder", "/api/v1/agui/info"} {
		if !s.isAGUIPath(p) {
			t.Fatalf("%q should be owned by the adapter", p)
		}
	}
	for _, p := range []string{"/api/v1/chat/stream", "/api/v1/aguix", "/api/v1/sessions"} {
		if s.isAGUIPath(p) {
			t.Fatalf("%q must not be treated as an AG-UI path", p)
		}
	}
}

// TestAGUIOnDedicatedListenerOwnsNoPath: with [AGUI] Port set the adapter
// serves itself, so it must own no path on this server — otherwise the
// middleware exclusions would carve a hole out of a surface nothing answers on.
func TestAGUIOnDedicatedListenerOwnsNoPath(t *testing.T) {
	s := &Server{agui: &agui.Runtime{}, aguiListener: &agui.Listener{}}

	if s.isAGUIPath("/api/v1/agui/coder") {
		t.Fatal("a dedicated listener must not claim a path on the API server")
	}
}

func TestCORSMiddlewareSkipsAGUI(t *testing.T) {
	s := &Server{agui: &agui.Runtime{}, aguiPath: "/api/v1/agui"}
	handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// The wildcard the Web-UI routes rely on must not reach the AG-UI surface.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/agui/coder", nil))
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("AG-UI route inherited a CORS header: %q", got)
	}

	// Every other route keeps the previous behaviour.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil))
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("existing routes must keep their CORS header, got %q", got)
	}
}

func TestAuthMiddlewareDelegatesAGUI(t *testing.T) {
	s := &Server{agui: &agui.Runtime{}, aguiPath: "/api/v1/agui", token: "secret"}
	var reached bool
	handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	// No X-Pando-Token: the adapter validates the bearer token itself.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/agui/coder", nil))
	if !reached {
		t.Fatal("AG-UI requests must reach the adapter for it to authenticate them")
	}

	// Other API routes still require the token.
	reached = false
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil))
	if reached || rec.Code != http.StatusUnauthorized {
		t.Fatalf("token enforcement regressed on existing routes: code=%d reached=%v", rec.Code, reached)
	}
}
