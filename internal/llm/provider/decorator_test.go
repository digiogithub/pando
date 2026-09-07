package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newDecoratorRequest builds a request that already carries the headers this
// package sets for a real provider call, so a test can assert that decoration
// leaves them alone.
func newDecoratorRequest(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer real-token")
	req.Header.Set("X-Api-Key", "real-key")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "real-beta")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func cloneHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func headersEqual(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
	}
	return true
}

// The standard build installs no decorator, and a request must then be exactly
// what it was before this capability existed.
func TestNoDecoratorUnchangedRequest(t *testing.T) {
	SetRequestDecorator(nil)
	req := newDecoratorRequest(t)
	before := cloneHeader(req.Header)

	applyDecoratedHeaders(req, RequestInfo{Provider: "anthropic", Model: "claude-x"})

	if !headersEqual(before, req.Header) {
		t.Fatalf("headers changed with no decorator installed:\nbefore %v\nafter  %v", before, req.Header)
	}
}

func TestSetRequestDecoratorAddsHeaders(t *testing.T) {
	t.Cleanup(func() { SetRequestDecorator(nil) })

	var gotInfo RequestInfo
	SetRequestDecorator(func(_ context.Context, info RequestInfo) map[string]string {
		gotInfo = info
		return map[string]string{"X-Task-Type": "review"}
	})

	req := newDecoratorRequest(t)
	applyDecoratedHeaders(req, RequestInfo{Provider: "anthropic", Model: "claude-x"})

	if req.Header.Get("X-Task-Type") != "review" {
		t.Fatalf("decorator header not applied: %v", req.Header)
	}
	if gotInfo.Provider != "anthropic" || gotInfo.Model != "claude-x" {
		t.Fatalf("request info not passed to the decorator: %+v", gotInfo)
	}
}

// The guard is enforced here as well as at the extension boundary: a caller
// that installs a decorator directly must not be able to replace the
// credential or protocol headers either.
func TestDecoratorCannotOverwriteProtectedHeaders(t *testing.T) {
	t.Cleanup(func() { SetRequestDecorator(nil) })

	SetRequestDecorator(func(context.Context, RequestInfo) map[string]string {
		return map[string]string{
			"Authorization":     "Bearer attacker",
			"x-api-key":         "stolen",
			"anthropic-beta":    "nonsense",
			"anthropic-version": "1999-01-01",
			"Content-Type":      "text/plain",
			"X-Task-Type":       "review",
		}
	})

	req := newDecoratorRequest(t)
	applyDecoratedHeaders(req, RequestInfo{Provider: "anthropic", Model: "claude-x"})

	for name, want := range map[string]string{
		"Authorization":     "Bearer real-token",
		"X-Api-Key":         "real-key",
		"anthropic-version": "2023-06-01",
		"anthropic-beta":    "real-beta",
		"Content-Type":      "application/json",
	} {
		if got := req.Header.Get(name); got != want {
			t.Errorf("protected header %q was changed: got %q, want %q", name, got, want)
		}
	}
	if req.Header.Get("X-Task-Type") != "review" {
		t.Error("an unprotected header was dropped along with the protected ones")
	}
}

func TestIsProtectedRequestHeader(t *testing.T) {
	protected := []string{
		"Authorization", "authorization", "Proxy-Authorization", "Cookie", "Set-Cookie",
		"X-Api-Key", "api-key", "x-goog-api-key", "Host", "Content-Type", "Content-Length",
		"Transfer-Encoding", "anthropic-version", "Anthropic-Beta", "", "   ",
	}
	for _, name := range protected {
		if !IsProtectedRequestHeader(name) {
			t.Errorf("%q should be protected", name)
		}
	}
	allowed := []string{"X-Task-Type", "X-Session-Id", "X-Tenant", "User-Agent", "X-Anthropic-Like"}
	for _, name := range allowed {
		if IsProtectedRequestHeader(name) {
			t.Errorf("%q should not be protected", name)
		}
	}
}

// The middleware must call through even when it decorates nothing.
func TestDecoratorMiddlewareCallsNext(t *testing.T) {
	SetRequestDecorator(nil)
	mw := decoratorMiddleware(RequestInfo{Provider: "openai", Model: "gpt-x"})

	called := false
	resp, err := mw(newDecoratorRequest(t), func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	if err != nil {
		t.Fatalf("middleware returned an error: %v", err)
	}
	if !called || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatal("middleware did not call through to the next handler")
	}
}
