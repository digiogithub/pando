package mcpauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// newTestCallbackServer starts a callback server on an ephemeral port
// (redirect URI port 0) so tests never collide with each other or with
// DefaultCallbackPort.
func newTestCallbackServer(t *testing.T) *CallbackServer {
	t.Helper()
	cs, err := StartCallbackServer("http://127.0.0.1:0" + DefaultCallbackPath)
	if err != nil {
		t.Fatalf("StartCallbackServer: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestCallbackServer_HappyPath(t *testing.T) {
	cs := newTestCallbackServer(t)

	errCh := make(chan error, 1)
	codeCh := make(chan string, 1)
	go func() {
		code, _, err := cs.Wait(context.Background(), "expected-state", 5*time.Second)
		codeCh <- code
		errCh <- err
	}()

	// Give Wait a moment to register itself before firing the request.
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("%s?code=abc123&state=expected-state", cs.RedirectURI())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if code := <-codeCh; code != "abc123" {
		t.Fatalf("code = %q, want %q", code, "abc123")
	}
}

func TestCallbackServer_StateMismatchRejected(t *testing.T) {
	cs := newTestCallbackServer(t)

	errCh := make(chan error, 1)
	go func() {
		_, _, err := cs.Wait(context.Background(), "expected-state", 300*time.Millisecond)
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("%s?code=abc123&state=wrong-state", cs.RedirectURI())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "CSRF") {
		t.Fatalf("body = %q, want mention of CSRF", body)
	}

	// The Wait call must NOT have been resolved by the mismatched request:
	// it should still be pending until its own timeout fires.
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Wait resolved successfully on a state-mismatched request")
		}
		// Timed out — expected, since the mismatched request must not
		// deliver a result.
	case <-time.After(2 * time.Second):
		t.Fatal("Wait never returned")
	}
}

func TestCallbackServer_MissingStateRejected(t *testing.T) {
	cs := newTestCallbackServer(t)

	go func() {
		_, _, _ = cs.Wait(context.Background(), "expected-state", 300*time.Millisecond)
	}()
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("%s?code=abc123", cs.RedirectURI())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCallbackServer_ErrorParamPropagated(t *testing.T) {
	cs := newTestCallbackServer(t)

	errCh := make(chan error, 1)
	go func() {
		_, _, err := cs.Wait(context.Background(), "expected-state", 5*time.Second)
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("%s?error=access_denied&error_description=user+said+no&state=expected-state", cs.RedirectURI())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The page itself renders 200 with an error message body; the
		// failure is signaled to Wait's return value, not the HTTP status.
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	err = <-errCh
	if err == nil {
		t.Fatal("expected an error from Wait")
	}
	if !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "user said no") {
		t.Fatalf("error = %v, want mention of access_denied and description", err)
	}
}

func TestCallbackServer_MissingCodeIsError(t *testing.T) {
	cs := newTestCallbackServer(t)

	errCh := make(chan error, 1)
	go func() {
		_, _, err := cs.Wait(context.Background(), "expected-state", 5*time.Second)
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("%s?state=expected-state", cs.RedirectURI())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()

	err = <-errCh
	if err == nil {
		t.Fatal("expected an error from Wait when code is missing")
	}
}

func TestCallbackServer_WrongPath404s(t *testing.T) {
	cs := newTestCallbackServer(t)

	base := cs.RedirectURI()
	// Swap the known-good path suffix for an unrelated one.
	wrongURL := base[:len(base)-len(DefaultCallbackPath)] + "/not-the-callback-path"

	resp, err := http.Get(wrongURL)
	if err != nil {
		t.Fatalf("GET wrong path: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCallbackServer_TimeoutHonoured(t *testing.T) {
	cs := newTestCallbackServer(t)

	start := time.Now()
	_, _, err := cs.Wait(context.Background(), "expected-state", 150*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("Wait returned too early: %s", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Wait took too long: %s", elapsed)
	}
}

func TestCallbackServer_ContextCancellation(t *testing.T) {
	cs := newTestCallbackServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, _, err := cs.Wait(ctx, "expected-state", 5*time.Second)
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected an error after context cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after context cancellation")
	}
}

func TestCallbackServer_CloseIsIdempotent(t *testing.T) {
	cs := newTestCallbackServer(t)
	if err := cs.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := cs.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCallbackServer_OnlyBindsLoopback(t *testing.T) {
	cs := newTestCallbackServer(t)
	uri := cs.RedirectURI()
	if !strings.Contains(uri, "127.0.0.1") {
		t.Fatalf("redirect URI = %q, want a 127.0.0.1 host", uri)
	}
}

func TestCallbackServer_IssCaptured(t *testing.T) {
	cs := newTestCallbackServer(t)

	type result struct {
		code, iss string
		err       error
	}
	resCh := make(chan result, 1)
	go func() {
		code, iss, err := cs.Wait(context.Background(), "expected-state", 5*time.Second)
		resCh <- result{code, iss, err}
	}()
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("%s?code=abc123&state=expected-state&iss=https%%3A%%2F%%2Fas.example.com", cs.RedirectURI())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Wait returned error: %v", res.err)
	}
	if res.code != "abc123" {
		t.Fatalf("code = %q, want abc123", res.code)
	}
	if res.iss != "https://as.example.com" {
		t.Fatalf("iss = %q, want https://as.example.com", res.iss)
	}
}

func TestCallbackServer_IssAbsentIsEmptyString(t *testing.T) {
	cs := newTestCallbackServer(t)

	type result struct {
		code, iss string
		err       error
	}
	resCh := make(chan result, 1)
	go func() {
		code, iss, err := cs.Wait(context.Background(), "expected-state", 5*time.Second)
		resCh <- result{code, iss, err}
	}()
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("%s?code=abc123&state=expected-state", cs.RedirectURI())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Wait returned error: %v", res.err)
	}
	if res.iss != "" {
		t.Fatalf("iss = %q, want empty when the server didn't send one", res.iss)
	}
}

func TestCallbackServer_ErrorParamIgnoredOnStateMismatch(t *testing.T) {
	// Phase 4 fix: an attacker hitting the callback with a spoofed
	// error/error_description but the wrong (or no) state must not be able
	// to resolve — successfully or with an error — a legitimate pending
	// Wait call at all. State must be validated before the error param is
	// ever looked at.
	cs := newTestCallbackServer(t)

	errCh := make(chan error, 1)
	go func() {
		_, _, err := cs.Wait(context.Background(), "expected-state", 300*time.Millisecond)
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)

	url := fmt.Sprintf("%s?error=access_denied&error_description=spoofed&state=wrong-state", cs.RedirectURI())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (state must be checked before the error param)", resp.StatusCode)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Wait resolved successfully on a state-mismatched error callback")
		}
		if strings.Contains(err.Error(), "spoofed") {
			t.Fatalf("Wait resolved with the spoofed error message: %v", err)
		}
		// Timed out with the plain timeout error — expected.
	case <-time.After(2 * time.Second):
		t.Fatal("Wait never returned")
	}
}

func TestParseRedirectURI(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantPort int
		wantPath string
		wantErr  bool
	}{
		{name: "valid loopback", in: "http://127.0.0.1:19876/mcp/oauth/callback", wantPort: 19876, wantPath: "/mcp/oauth/callback"},
		{name: "localhost host", in: "http://localhost:4000/cb", wantPort: 4000, wantPath: "/cb"},
		{name: "ephemeral port", in: "http://127.0.0.1:0/cb", wantPort: 0, wantPath: "/cb"},
		{name: "non-loopback rejected", in: "http://example.com:8080/cb", wantErr: true},
		{name: "https non-loopback rejected", in: "https://example.com/cb", wantErr: true},
		{name: "unparseable rejected", in: "://bad", wantErr: true},
		{name: "missing port rejected", in: "http://127.0.0.1/cb", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, path, err := parseRedirectURI(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if port != tt.wantPort || path != tt.wantPath {
				t.Fatalf("parseRedirectURI(%q) = (%d, %q), want (%d, %q)", tt.in, port, path, tt.wantPort, tt.wantPath)
			}
		})
	}
}
