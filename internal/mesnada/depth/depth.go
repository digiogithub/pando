// Package depth carries the delegation nesting level of a running agent across
// every path a delegated task can take, so the MaxDepth cap holds no matter how
// a subagent was launched:
//
//   - cold CLI / ACP subprocesses read EnvVar, set by the spawner;
//   - external CLI agents (claude, copilot, ...) reach Pando through the per-task
//     MCP config, whose "pando" stdio entry carries EnvVar too;
//   - warm ACP instances and IPC delegations run many sessions inside one
//     long-lived process, so the level travels with the request (ACP
//     session/prompt _meta under MetaKey, or the IPC delegation params) and is
//     recorded per session with SetSession;
//   - HTTP MCP callers may send HeaderName, which lands in the request context.
//
// The value is always the level the agent itself runs at: 0 for an interactive
// top-level session, 1 for a subagent it spawned, and so on. A spawn from an
// agent at level N creates a task at level N+1.
//
// The package is a leaf (standard library only) so the orchestrator, the
// project manager, the IPC bridge, the ACP agent and the tools can all share it
// without import cycles.
package depth

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	// EnvVar is the environment variable a delegated subprocess reads to learn
	// its own nesting level.
	EnvVar = "PANDO_DELEGATION_DEPTH"
	// MetaKey is the ACP _meta key (session/prompt) that carries the level of a
	// warm delegated session.
	MetaKey = "pando:delegationDepth"
	// HeaderName is the HTTP header an MCP client may send to declare its level.
	HeaderName = "X-Pando-Delegation-Depth"
)

type ctxKey struct{}

// WithContext returns a context that records the caller's own nesting level.
// Negative values are ignored.
func WithContext(ctx context.Context, level int) context.Context {
	if level < 0 {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, level)
}

// FromContext returns the level recorded by WithContext, if any.
func FromContext(ctx context.Context) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	v, ok := ctx.Value(ctxKey{}).(int)
	return v, ok
}

// FromEnv returns the level declared by EnvVar for this process, or 0 when it
// is unset or invalid (an interactive top-level Pando).
func FromEnv() int {
	if n, ok := Parse(os.Getenv(EnvVar)); ok {
		return n
	}
	return 0
}

// Parse decodes a non-negative level from a string (env var or header value).
func Parse(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// FromMeta reads MetaKey from an ACP _meta map. JSON numbers decode as float64,
// but a string or int is accepted too so non-Go clients are not penalised.
func FromMeta(meta map[string]any) (int, bool) {
	if meta == nil {
		return 0, false
	}
	switch v := meta[MetaKey].(type) {
	case float64:
		if v >= 0 {
			return int(v), true
		}
	case int:
		if v >= 0 {
			return v, true
		}
	case int64:
		if v >= 0 {
			return int(v), true
		}
	case string:
		return Parse(v)
	}
	return 0, false
}

// Meta returns an ACP _meta map that carries level.
func Meta(level int) map[string]any {
	return map[string]any{MetaKey: level}
}

var (
	sessionsMu sync.RWMutex
	sessions   = map[string]int{}
)

// SetSession records the nesting level of a session that runs inside a
// long-lived process (warm ACP instance, IPC delegation target). The entry is
// kept for the session's lifetime: a delegated session may be resumed later
// (await / resurrection) and must still know its level then.
func SetSession(sessionID string, level int) {
	if sessionID == "" || level < 0 {
		return
	}
	sessionsMu.Lock()
	sessions[sessionID] = level
	sessionsMu.Unlock()
}

// Session returns the level recorded for sessionID, if any.
func Session(sessionID string) (int, bool) {
	if sessionID == "" {
		return 0, false
	}
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	v, ok := sessions[sessionID]
	return v, ok
}

// ClearSession forgets a session's level (session deleted / closed).
func ClearSession(sessionID string) {
	sessionsMu.Lock()
	delete(sessions, sessionID)
	sessionsMu.Unlock()
}

// Resolve returns the caller's own nesting level using, in order: an explicit
// context value, the level recorded for sessionID, then the process env var.
func Resolve(ctx context.Context, sessionID string) int {
	if v, ok := FromContext(ctx); ok {
		return v
	}
	if v, ok := Session(sessionID); ok {
		return v
	}
	return FromEnv()
}
