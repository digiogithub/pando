package mcpauth

import (
	"net/http"
	"sync"
)

// ScopeCapturingTransport wraps an http.RoundTripper and records the most
// recent 403 Forbidden response's WWW-Authenticate challenge it observes.
//
// This exists to work around a real gap in mcp-go v0.57.0: its request path
// (client/transport/streamable_http.go SendRequest, and the equivalent SSE
// codepath) only preserves response headers for a 401 Unauthorized —
// anything else, including a 403 with a WWW-Authenticate:
// Bearer error="insufficient_scope" challenge, falls into the generic
// non-2xx branch that returns just
// fmt.Errorf("request failed with status %d: %s", resp.StatusCode, body)
// and discards resp.Header entirely. There is no exported hook to observe
// the raw *http.Response for a non-401 status. Installing this transport as
// the http.Client mcp-go actually issues the tool-call request through
// (not the OAuthConfig.HTTPClient, which is a separate client instance used
// only by the OAuthHandler's own metadata/token/refresh requests) is the
// only place headers are still visible before mcp-go discards them.
type ScopeCapturingTransport struct {
	base http.RoundTripper

	mu   sync.Mutex
	last *Challenge
}

// NewScopeCapturingTransport returns a ScopeCapturingTransport delegating
// to base. A nil base uses http.DefaultTransport.
func NewScopeCapturingTransport(base http.RoundTripper) *ScopeCapturingTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &ScopeCapturingTransport{base: base}
}

var _ http.RoundTripper = (*ScopeCapturingTransport)(nil)

// RoundTrip implements http.RoundTripper, delegating to the wrapped
// transport and recording a 403 response's WWW-Authenticate challenge (if
// any) before returning.
func (t *ScopeCapturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusForbidden {
		if values := resp.Header.Values("WWW-Authenticate"); len(values) > 0 {
			ch := ParseChallenges(values)
			if ch.Scheme != "" {
				t.mu.Lock()
				cp := ch
				t.last = &cp
				t.mu.Unlock()
			}
		}
	}
	return resp, err
}

// LastChallenge returns (and clears) the most recently captured 403
// challenge, if any. Clearing on read means a later call that hits neither
// a 403 nor a WWW-Authenticate header does not accidentally reuse a stale
// challenge from an earlier, unrelated request made through the same
// client.
func (t *ScopeCapturingTransport) LastChallenge() (Challenge, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		return Challenge{}, false
	}
	ch := *t.last
	t.last = nil
	return ch, true
}
