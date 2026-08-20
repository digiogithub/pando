package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// freePort returns a port nothing is listening on.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// runningServer starts a minimal Server (no app, no DB) on a free loopback port
// and returns it once it answers requests.
func runningServer(t *testing.T) *Server {
	t.Helper()

	port := freePort(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	s := &Server{
		config:      ServerConfig{Host: "127.0.0.1", Port: port, StartupMode: "app"},
		token:       "test-token",
		bindHost:    "127.0.0.1",
		initialHost: "127.0.0.1",
		rebindCh:    make(chan net.Listener, 1),
		httpServer:  &http.Server{Handler: mux},
	}

	done := make(chan error, 1)
	go func() { done <- s.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(ctx)
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("server exited with %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("server did not exit after shutdown")
		}
	})

	waitHealthy(t, port)
	return s
}

func waitHealthy(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server on port %d never became reachable", port)
}

func TestRebindMovesListenerAndKeepsServing(t *testing.T) {
	s := runningServer(t)

	if err := s.Rebind("0.0.0.0"); err != nil {
		t.Fatalf("rebind to 0.0.0.0 failed: %v", err)
	}
	if got := s.BindHost(); got != "0.0.0.0" {
		t.Fatalf("expected bind host 0.0.0.0, got %q", got)
	}
	waitHealthy(t, s.config.Port)

	// Back to loopback: the port must be released and re-bound, not leaked.
	if err := s.Rebind(s.InitialHost()); err != nil {
		t.Fatalf("rebind back to loopback failed: %v", err)
	}
	if got := s.BindHost(); got != "127.0.0.1" {
		t.Fatalf("expected bind host 127.0.0.1, got %q", got)
	}
	waitHealthy(t, s.config.Port)
}

func TestRebindRestoresPreviousHostOnFailure(t *testing.T) {
	s := runningServer(t)

	// A host no interface owns cannot be bound, so the rebind must fail and the
	// server must keep answering on the address it had.
	err := s.Rebind("203.0.113.7")
	if err == nil {
		t.Fatal("expected rebind to an unassigned address to fail")
	}
	if got := s.BindHost(); got != "127.0.0.1" {
		t.Fatalf("expected bind host to stay 127.0.0.1, got %q", got)
	}
	waitHealthy(t, s.config.Port)
}

// externalAccessServer wires the global config for the toggle endpoint tests.
func externalAccessServer(t *testing.T, startupMode string, basicAuth config.BasicAuthConfig) *Server {
	t.Helper()

	prev := config.Get()
	config.SetForTests(&config.Config{
		Server: config.APIServerConfig{Enabled: true, Host: "localhost", Port: 9999, BasicAuth: basicAuth},
	})
	t.Cleanup(func() { config.SetForTests(prev) })

	return &Server{
		config:      ServerConfig{Host: "localhost", Port: 9999, StartupMode: startupMode},
		token:       "test-token",
		bindHost:    "localhost",
		initialHost: "localhost",
		rebindCh:    make(chan net.Listener, 1),
	}
}

func externalAccessPut(t *testing.T, s *Server, enabled bool) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"enabled":%t}`, enabled))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/api-server/external-access", body)
	rec := httptest.NewRecorder()
	s.handleExternalAccess(rec, req)
	return rec
}

func TestExternalAccessRefusedWithoutCredentials(t *testing.T) {
	s := externalAccessServer(t, "app", config.BasicAuthConfig{})

	rec := externalAccessPut(t, s, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without basic-auth users, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "basic_auth_required_for_external_access") {
		t.Fatalf("expected a basic-auth error code, got %s", rec.Body.String())
	}
	if s.BindHost() != "localhost" {
		t.Fatalf("bind host must not change on a refused toggle, got %q", s.BindHost())
	}
}

func TestExternalAccessStatusReportsToggleability(t *testing.T) {
	s := externalAccessServer(t, "app", config.BasicAuthConfig{
		Enabled: true,
		Users:   []config.BasicAuthUser{{Username: "admin", Password: "secret"}},
	})

	rec := httptest.NewRecorder()
	s.handleExternalAccess(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config/api-server/external-access", nil))

	var status ExternalAccessStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}
	if status.Enabled {
		t.Fatal("expected external access to be off while bound to localhost")
	}
	if !status.CanToggle {
		t.Fatal("expected the toggle to be available in app mode on a loopback bind")
	}
	if !status.BasicAuthReady {
		t.Fatal("expected basic auth to be reported as ready")
	}
}

func TestExternalAccessFixedByStartupFlag(t *testing.T) {
	s := externalAccessServer(t, "serve", config.BasicAuthConfig{
		Enabled: true,
		Users:   []config.BasicAuthUser{{Username: "admin", Password: "secret"}},
	})
	s.bindHost = "0.0.0.0"
	s.initialHost = "0.0.0.0"

	rec := externalAccessPut(t, s, false)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when the bind comes from --host, got %d", rec.Code)
	}
}
