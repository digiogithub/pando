package mcpauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultCallbackTimeout is how long CallbackServer.Wait blocks for the
// browser redirect to arrive before giving up, unless the caller passes a
// different timeout.
const DefaultCallbackTimeout = 5 * time.Minute

// CallbackServer is a short-lived local HTTP server that receives the OAuth
// authorization redirect (the "authorization code" leg of the flow) on
// 127.0.0.1. It is deliberately narrow: it only ever answers the configured
// callback path, only ever binds to loopback, and resolves at most one
// authorization attempt before it must be closed and recreated.
type CallbackServer struct {
	listener net.Listener
	server   *http.Server
	path     string

	mu       sync.Mutex
	state    string // expected state for the in-flight Wait call, set by Wait
	waiting  bool
	resultCh chan callbackResult
	closed   bool
}

// callbackResult is delivered on resultCh exactly once per Wait call.
type callbackResult struct {
	code string
	// iss is the RFC 9207 §3 "iss" query parameter from the authorization
	// response, if the authorization server sent one. Wait hands it back to
	// the caller (Manager.Login) so it can be validated against the issuer
	// recorded from AS metadata before the code is exchanged.
	iss string
	err error
}

// StartCallbackServer parses redirectURI to determine which loopback port
// and path to listen on (falling back to DefaultCallbackPort /
// DefaultCallbackPath when redirectURI is empty or fails to parse as an
// absolute http(s) URL), starts listening, and returns the running server.
//
// Only 127.0.0.1 (or localhost, normalized to 127.0.0.1) is ever bound; a
// redirectURI pointing anywhere else is rejected rather than silently
// listening on an unexpected interface.
func StartCallbackServer(redirectURI string) (*CallbackServer, error) {
	port, path := DefaultCallbackPort, DefaultCallbackPath

	if strings.TrimSpace(redirectURI) != "" {
		if p, path2, err := parseRedirectURI(redirectURI); err == nil {
			port, path = p, path2
		}
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: listen on %s: %w", addr, err)
	}

	cs := &CallbackServer{
		listener: listener,
		path:     path,
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, cs.handleCallback)
	cs.server = &http.Server{Handler: mux}

	go func() {
		_ = cs.server.Serve(listener)
	}()

	return cs, nil
}

// parseRedirectURI extracts the loopback port and path component from a
// configured redirect URI. It returns an error if redirectURI does not parse
// as an absolute URL, is not http(s), or does not point at a loopback host
// (127.0.0.1 / localhost) — configuration mistakes here must not silently
// bind a listener on an unexpected interface.
func parseRedirectURI(redirectURI string) (port int, path string, err error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return 0, "", fmt.Errorf("invalid redirect URI: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return 0, "", fmt.Errorf("redirect URI must use http or https")
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return 0, "", fmt.Errorf("redirect URI must point at a loopback host, got %q", host)
	}
	p := u.Port()
	if p == "" {
		return 0, "", fmt.Errorf("redirect URI must specify an explicit port")
	}
	portNum, err := strconv.Atoi(p)
	if err != nil {
		return 0, "", fmt.Errorf("invalid port in redirect URI: %w", err)
	}
	path = u.Path
	if path == "" {
		path = "/"
	}
	return portNum, path, nil
}

// RedirectURI returns the actual redirect_uri this server answers on,
// derived from the bound listener's port so the caller (Manager.Login) uses
// the port actually acquired rather than the one requested.
func (cs *CallbackServer) RedirectURI() string {
	addr := cs.listener.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d%s", addr.Port, cs.path)
}

// Wait blocks until the callback server receives a matching authorization
// response, an error response, ctx is cancelled, or timeout elapses
// (defaulting to DefaultCallbackTimeout when timeout <= 0). Only one Wait
// call may be in flight at a time.
//
// It returns the authorization code and, per RFC 9207 §3, the "iss" query
// parameter the authorization server included on the redirect (empty if the
// server didn't send one). Callers MUST validate iss against the issuer
// recorded from AS metadata before exchanging code for tokens — Wait itself
// only captures the raw value; it does not know the expected issuer.
func (cs *CallbackServer) Wait(ctx context.Context, state string, timeout time.Duration) (code string, iss string, err error) {
	if timeout <= 0 {
		timeout = DefaultCallbackTimeout
	}

	cs.mu.Lock()
	if cs.waiting {
		cs.mu.Unlock()
		return "", "", fmt.Errorf("mcpauth: callback server already has a pending Wait call")
	}
	cs.state = state
	cs.waiting = true
	cs.resultCh = make(chan callbackResult, 1)
	resultCh := cs.resultCh
	cs.mu.Unlock()

	defer func() {
		cs.mu.Lock()
		cs.waiting = false
		cs.mu.Unlock()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case res := <-resultCh:
		return res.code, res.iss, res.err
	case <-ctx.Done():
		return "", "", ctx.Err()
	case <-timer.C:
		return "", "", fmt.Errorf("mcpauth: timed out after %s waiting for the OAuth callback", timeout)
	}
}

// Close shuts down the HTTP server and releases the listener. It is
// idempotent: calling it more than once (or after the server already shut
// itself down) is a no-op.
func (cs *CallbackServer) Close() error {
	cs.mu.Lock()
	if cs.closed {
		cs.mu.Unlock()
		return nil
	}
	cs.closed = true
	cs.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return cs.server.Shutdown(ctx)
}

// handleCallback serves the configured redirect path. Every security check
// described in the phase-3/4 spec lives here: state presence/match, error
// propagation, missing code, and the fact that a request is answered (and
// the pending Wait resolved) at most once.
//
// The state check runs BEFORE the error-param check (Phase 4 fix): state is
// the only thing binding this HTTP request to a specific in-flight login,
// so an attacker who does not know it must not be able to influence a
// pending Wait at all — not even by resolving it with a spoofed
// error/error_description, which the pre-Phase-4 ordering allowed since the
// error branch returned before ever looking at state.
func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	state := q.Get("state")
	cs.mu.Lock()
	expectedState := cs.state
	cs.mu.Unlock()

	if state == "" || expectedState == "" || state != expectedState {
		// Do NOT resolve the pending Wait on a state mismatch: an attacker
		// probing the callback endpoint with a guessed/absent state must not
		// be able to poison (or prematurely resolve) a legitimate,
		// still-pending login. Reject the request itself with 400.
		http.Error(w, "invalid or missing state parameter (possible CSRF)", http.StatusBadRequest)
		return
	}

	// RFC 9207 §3: capture iss (if present) so the caller can validate it
	// against the issuer recorded from AS metadata before exchanging any
	// code. This handler does not know the expected issuer, so it never
	// rejects on iss itself — Manager.Login owns that decision.
	iss := q.Get("iss")

	if errCode := q.Get("error"); errCode != "" {
		desc := q.Get("error_description")
		msg := errCode
		if desc != "" {
			msg = fmt.Sprintf("%s: %s", errCode, desc)
		}
		cs.deliver(callbackResult{iss: iss, err: fmt.Errorf("authorization server returned an error: %s", msg)})
		writeCallbackPage(w, http.StatusOK, false, fmt.Sprintf("Authorization failed: %s", msg))
		return
	}

	code := q.Get("code")
	if code == "" {
		cs.deliver(callbackResult{iss: iss, err: fmt.Errorf("authorization callback is missing the code parameter")})
		writeCallbackPage(w, http.StatusOK, false, "The authorization response did not include a code.")
		return
	}

	cs.deliver(callbackResult{code: code, iss: iss})
	writeCallbackPage(w, http.StatusOK, true, "")
}

// deliver sends res to the currently pending Wait call, if any. It is safe
// to call even when no Wait is pending (e.g. a duplicate browser request
// after the first one already resolved): the send is dropped rather than
// blocking, since resultCh is buffered with capacity 1 and only one send per
// Wait is expected to succeed.
func (cs *CallbackServer) deliver(res callbackResult) {
	cs.mu.Lock()
	ch := cs.resultCh
	waiting := cs.waiting
	cs.mu.Unlock()
	if !waiting || ch == nil {
		return
	}
	select {
	case ch <- res:
	default:
	}
}

// writeCallbackPage serves a minimal, self-contained HTML page (no external
// assets) telling the user whether the login succeeded, so they know it is
// safe to close the browser tab and return to the terminal.
func writeCallbackPage(w http.ResponseWriter, status int, ok bool, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	title := "Authorization successful"
	body := "You can close this tab and return to Pando."
	color := "#2e7d32"
	if !ok {
		title = "Authorization failed"
		body = "You can close this tab and return to Pando to try again."
		color = "#c62828"
		if detail != "" {
			body = detail + " " + body
		}
	}

	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Pando MCP Authorization</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
         display: flex; align-items: center; justify-content: center; height: 100vh;
         margin: 0; background: #f5f5f5; color: #1a1a1a; }
  .card { background: #fff; border-radius: 12px; padding: 2.5rem 3rem; box-shadow: 0 2px 12px rgba(0,0,0,0.08);
          text-align: center; max-width: 420px; }
  h1 { color: %s; font-size: 1.4rem; margin: 0 0 0.75rem; }
  p { margin: 0; color: #555; }
</style>
</head>
<body>
  <div class="card">
    <h1>%s</h1>
    <p>%s</p>
  </div>
</body>
</html>
`, color, title, body)
}
