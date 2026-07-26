package mcpauth

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// maxStepUpAttempts caps how many times Manager will hand back a fresh
// ErrInsufficientScope for the same (server, accumulated-scope-set) pair
// before giving up with a hard error instead. Without this cap, a
// misconfigured resource server that always answers 403 insufficient_scope
// — even after the client already holds the exact scope it asked for —
// would otherwise send the caller into an endless re-authorization loop.
const maxStepUpAttempts = 2

// ErrInsufficientScope is returned when an MCP server rejects a request
// with HTTP 403 and a WWW-Authenticate challenge carrying
// error="insufficient_scope" (RFC 6750 §3.1): the currently-held access
// token is valid but lacks a scope the server now requires for this
// operation. Scopes is the accumulated set (previously requested union
// challenged) that a fresh login should request.
type ErrInsufficientScope struct {
	ServerName string
	Scopes     []string
}

func (e *ErrInsufficientScope) Error() string {
	return fmt.Sprintf("MCP server %q needs additional permissions (scope: %s). Run: pando mcp login %s",
		e.ServerName, strings.Join(e.Scopes, " "), e.ServerName)
}

// IsInsufficientScope reports whether err is (or wraps) an
// *ErrInsufficientScope, returning it for callers that want the challenged
// scope list.
func IsInsufficientScope(err error) (*ErrInsufficientScope, bool) {
	var target *ErrInsufficientScope
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// RecordGrantedScopes persists scopes as the server's accumulated
// RequestedScopes, for use by a later StepUpScopes call. It is normally
// called by Manager.Login on success (with the scope set it actually used),
// not by callers directly.
func (m *Manager) RecordGrantedScopes(serverName string, scopes []string) error {
	var serverURL string
	if entry, ok, err := m.store.Get(serverName); err == nil && ok {
		serverURL = entry.ServerURL
	}
	if err := m.store.UpdateRequestedScopes(serverName, scopes, serverURL); err != nil {
		return fmt.Errorf("mcpauth: record granted scopes for %q: %w", serverName, err)
	}
	return nil
}

// StepUpScopes returns the union of serverName's previously-requested
// scopes (if any are persisted) and challenged, preserving the order
// previously-requested scopes were first seen in and then appending any new
// challenged scopes not already present. The MCP authorization spec
// requires clients accumulate scopes on a 403 insufficient_scope step-up
// rather than replace the previous grant, so a permission the user already
// approved is never silently dropped by a later, narrower request.
func (m *Manager) StepUpScopes(serverName string, challenged []string) []string {
	var previous []string
	if entry, ok, err := m.store.Get(serverName); err == nil && ok {
		previous = entry.RequestedScopes
	}
	return unionScopes(previous, challenged)
}

// unionScopes returns a and b combined, in a's order followed by any of b's
// elements not already present, with duplicates removed.
func unionScopes(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range b {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// stepUpAttempts tracks, per (server, accumulated-scope-set) key, how many
// times HandleInsufficientScope has handed back a re-authorization prompt,
// so a misbehaving server cannot loop a caller forever. Guarded by
// stepUpMu.
type stepUpState struct {
	mu       sync.Mutex
	attempts map[string]int
}

// HandleInsufficientScope is the single entry point transport-layer code
// (internal/mcpclient) calls when it detects a 403 insufficient_scope
// response for serverName: it accumulates challenged with any previously
// requested scopes (StepUpScopes), enforces maxStepUpAttempts for that
// accumulated set, and returns either a fresh *ErrInsufficientScope (for the
// caller to surface as "run pando mcp login <name>") or a terminal error
// once the cap is hit.
func (m *Manager) HandleInsufficientScope(serverName string, challenged []string) error {
	accumulated := m.StepUpScopes(serverName, challenged)
	key := serverName + "\x00" + strings.Join(accumulated, " ")

	m.stepUp.mu.Lock()
	if m.stepUp.attempts == nil {
		m.stepUp.attempts = map[string]int{}
	}
	m.stepUp.attempts[key]++
	attempt := m.stepUp.attempts[key]
	m.stepUp.mu.Unlock()

	if attempt > maxStepUpAttempts {
		return fmt.Errorf("mcpauth: MCP server %q keeps rejecting requests as insufficient_scope even after %d re-authorization attempt(s) for scope %q; giving up — check the server's OAuth scope configuration", serverName, maxStepUpAttempts, strings.Join(accumulated, " "))
	}
	return &ErrInsufficientScope{ServerName: serverName, Scopes: accumulated}
}
