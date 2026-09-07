package provider

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/logging"
)

// Per-request decoration of outgoing provider calls.
//
// Provider accounts already carry static extra headers, applied when the
// client is built. This is the dynamic counterpart: a hook consulted once per
// HTTP request, so a value that changes between two calls made by the same
// client (a conversation id, the kind of work the call is doing) can reach the
// provider. It is installed as SDK middleware rather than as client options
// precisely because the client is built once and used many times.
//
// The hook is process-wide and set at most once during start-up, the same way
// the session package receives its IPC publisher. That keeps this package free
// of any dependency on the extension system: it exposes a function type, and
// whoever wires the process decides what fills it.

// RequestInfo names the destination of the request being decorated.
type RequestInfo struct {
	// Provider is the provider identifier ("anthropic", "openai", ...).
	Provider string
	// Model is the API model id the request will name.
	Model string
}

// RequestDecorator returns the headers to add to one outgoing provider
// request. ctx is the request's own context, so the returned values may differ
// from call to call. Returning nil adds nothing.
//
// It is called on the goroutine performing the request, in front of a network
// call the user is waiting on, and must therefore be cheap and non-blocking.
type RequestDecorator func(ctx context.Context, info RequestInfo) map[string]string

// requestDecorator holds the process-wide hook. A pointer in an atomic keeps
// the read on the request path lock-free; the value is written at most once,
// during start-up.
var requestDecorator atomic.Pointer[RequestDecorator]

// SetRequestDecorator installs the process-wide request decorator, replacing
// any previous one. Passing nil removes it, which restores exactly the
// behaviour of a build that never called this function.
func SetRequestDecorator(fn RequestDecorator) {
	if fn == nil {
		requestDecorator.Store(nil)
		return
	}
	requestDecorator.Store(&fn)
}

// currentRequestDecorator returns the installed decorator, or nil.
func currentRequestDecorator() RequestDecorator {
	if p := requestDecorator.Load(); p != nil {
		return *p
	}
	return nil
}

// protectedHeaderPrefixes are header families the host owns end to end. A
// decorator may not add or replace anything under them.
//
// "anthropic-" covers the protocol headers the Anthropic SDK and this package
// negotiate between them (anthropic-version, anthropic-beta, ...): a decorator
// that changed one would silently alter the API contract of the call.
var protectedHeaderPrefixes = []string{"anthropic-"}

// protectedHeaderNames are the individual headers a decorator may not add or
// replace: the credential headers, the cookie headers, and the framing headers
// that describe the body this package built.
var protectedHeaderNames = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"set-cookie":          {},
	"x-api-key":           {},
	"api-key":             {},
	"x-goog-api-key":      {},
	"host":                {},
	"content-type":        {},
	"content-length":      {},
	"transfer-encoding":   {},
}

// IsProtectedRequestHeader reports whether name is a header the host owns and
// a decorator may not set. Comparison is case-insensitive, as HTTP header
// names are.
//
// It is exported so that the adapter which calls an extension can drop a bad
// entry at the boundary, where it can be reported against the extension that
// produced it, as well as here where it is enforced.
func IsProtectedRequestHeader(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return true
	}
	if _, ok := protectedHeaderNames[name]; ok {
		return true
	}
	for _, prefix := range protectedHeaderPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// applyDecoratedHeaders consults the installed decorator and writes what it
// returns onto req. It is a no-op when no decorator is installed, which is the
// standard build.
func applyDecoratedHeaders(req *http.Request, info RequestInfo) {
	decorate := currentRequestDecorator()
	if decorate == nil || req == nil {
		return
	}
	headers := decorate(req.Context(), info)
	for name, value := range headers {
		if IsProtectedRequestHeader(name) {
			logging.Warn("Ignoring provider request header: the host owns it",
				"header", name, "provider", info.Provider)
			continue
		}
		req.Header.Set(name, value)
	}
}

// decoratorMiddleware builds the SDK middleware that applies the decorator to
// every request the client makes.
//
// The signature is spelled out rather than written in terms of one SDK's
// option package because the Anthropic and OpenAI SDKs declare the same
// middleware shape as type aliases; one function therefore satisfies both.
func decoratorMiddleware(info RequestInfo) func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	return func(req *http.Request, next func(*http.Request) (*http.Response, error)) (*http.Response, error) {
		applyDecoratedHeaders(req, info)
		return next(req)
	}
}
