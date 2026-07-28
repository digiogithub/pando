package agui

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestStartListenerServesOnlyAGUI is the isolation guarantee of deployment
// shape 2: the dedicated port carries the adapter's routes and nothing else of
// the API server.
func TestStartListenerServesOnlyAGUI(t *testing.T) {
	cfg := testConfig()
	cfg.Port = 0 // resolved below; 0 means "no dedicated listener"
	r := newTestRuntime(cfg, "secret")

	if _, err := r.StartListener(ListenerOptions{}); err == nil {
		t.Fatal("a listener without a configured port must be refused")
	}

	// Port 0 in Config means "mounted", so an ephemeral port cannot be requested
	// through it: a concrete free one is picked instead.
	r.cfg.Port = freePort(t)
	listener, err := r.StartListener(ListenerOptions{Host: "127.0.0.1"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = listener.Shutdown(ctx)
	})

	base := listener.URL()
	if !strings.HasPrefix(base, "http://") {
		t.Fatalf("a listener without certificates must serve plain HTTP, got %q", base)
	}

	resp, err := http.Get(base + defaultPath + "/info?token=secret")
	if err != nil {
		t.Fatalf("get info: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var info InfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Protocol != "ag-ui" {
		t.Fatalf("unexpected discovery payload: %+v", info)
	}

	// Nothing else of the API server exists on this listener.
	other, err := http.Get(base + "/api/v1/sessions")
	if err != nil {
		t.Fatalf("get sessions: %v", err)
	}
	defer other.Body.Close()
	if other.StatusCode != http.StatusNotFound {
		t.Fatalf("the dedicated listener exposed a non-AG-UI route: %d", other.StatusCode)
	}
}

// TestListenerRejectsTokenlessRequest: the dedicated port keeps the adapter's
// own auth policy, since no outer middleware protects it.
func TestListenerRejectsTokenlessRequest(t *testing.T) {
	cfg := testConfig()
	cfg.Port = freePort(t)
	r := newTestRuntime(cfg, "secret")

	listener, err := r.StartListener(ListenerOptions{Host: "127.0.0.1"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = listener.Shutdown(ctx)
	})

	resp, err := http.Get(listener.URL() + defaultPath + "/info")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestListenerShutdownIsClean(t *testing.T) {
	cfg := testConfig()
	cfg.Port = freePort(t)
	r := newTestRuntime(cfg, "secret")

	listener, err := r.StartListener(ListenerOptions{Host: "127.0.0.1"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := listener.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := listener.Wait(); err != nil {
		t.Fatalf("a clean shutdown must not report an error: %v", err)
	}
}

// freePort returns a port nothing is listening on. Config.Port == 0 means
// "mounted on the API server", so the tests cannot let the OS assign one.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
