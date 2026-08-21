package extensions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/pkg/extension"
)

type httpExt struct {
	baseExt
	base   string
	routes []extension.Route
}

func (e *httpExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *httpExt) BasePath() string              { return e.base }
func (e *httpExt) Routes() []extension.Route     { return e.routes }

type mwExt struct {
	baseExt
	wrap func(http.Handler) http.Handler
}

func (e *mwExt) ExtensionInfo() extension.Info           { return e.info(e) }
func (e *mwExt) WrapHTTP(next http.Handler) http.Handler { return e.wrap(next) }

func textHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

func TestRegisterRoutesMountsUnderExtPrefix(t *testing.T) {
	mgr := managerWith(t, &httpExt{
		baseExt: baseExt{id: "api.acme"},
		base:    "acme",
		routes: []extension.Route{
			{Pattern: "ping", Handler: textHandler("pong")},
			{Pattern: "GET reports/{id}", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("report " + r.PathValue("id")))
			})},
		},
	})

	mux := http.NewServeMux()
	got := RegisterRoutes(mgr, mux)
	if len(got) != 2 {
		t.Fatalf("registered %v", got)
	}
	for _, pattern := range got {
		if !strings.Contains(pattern, ExtRoutePrefix+"acme/") {
			t.Errorf("route escaped the extension prefix: %q", pattern)
		}
	}

	if code, body := get(t, mux, "/api/ext/acme/ping"); code != http.StatusOK || body != "pong" {
		t.Errorf("ping = %d %q", code, body)
	}
	if code, body := get(t, mux, "/api/ext/acme/reports/42"); code != http.StatusOK || body != "report 42" {
		t.Errorf("reports = %d %q", code, body)
	}
}

// An extension must not be able to claim a path outside /api/ext/.
func TestRegisterRoutesRejectsBadBasePath(t *testing.T) {
	for _, base := range []string{"", "/", "acme/sub", "Acme", "../etc"} {
		mgr := managerWith(t, &httpExt{
			baseExt: baseExt{id: "api.acme"},
			base:    base,
			routes:  []extension.Route{{Pattern: "ping", Handler: textHandler("pong")}},
		})
		mux := http.NewServeMux()
		if got := RegisterRoutes(mgr, mux); len(got) != 0 {
			t.Errorf("base %q was accepted: %v", base, got)
		}
	}
}

func TestRegisterRoutesRejectsDuplicateBasePath(t *testing.T) {
	mgr := managerWith(t,
		&httpExt{baseExt: baseExt{id: "api.one"}, base: "acme",
			routes: []extension.Route{{Pattern: "a", Handler: textHandler("one")}}},
		&httpExt{baseExt: baseExt{id: "api.two"}, base: "acme",
			routes: []extension.Route{{Pattern: "b", Handler: textHandler("two")}}},
	)
	mux := http.NewServeMux()
	got := RegisterRoutes(mgr, mux)
	if len(got) != 1 {
		t.Fatalf("both extensions claimed the same base path: %v", got)
	}
}

// A duplicate pattern makes ServeMux panic; the server must still start.
func TestRegisterRoutesContainsDuplicatePatternPanic(t *testing.T) {
	mgr := managerWith(t, &httpExt{
		baseExt: baseExt{id: "api.acme"},
		base:    "acme",
		routes: []extension.Route{
			{Pattern: "ping", Handler: textHandler("first")},
			{Pattern: "ping", Handler: textHandler("second")},
		},
	})
	mux := http.NewServeMux()
	got := RegisterRoutes(mgr, mux)
	if len(got) != 1 {
		t.Fatalf("registered %v, want only the first", got)
	}
	if _, body := get(t, mux, "/api/ext/acme/ping"); body != "first" {
		t.Errorf("body = %q", body)
	}
}

func TestRegisterRoutesNilManager(t *testing.T) {
	if got := RegisterRoutes(nil, http.NewServeMux()); got != nil {
		t.Errorf("got %v", got)
	}
}

// Highest priority is outermost, so it sees the request first.
func TestWrapHTTPOrder(t *testing.T) {
	var order []string
	mk := func(id extension.ID, prio int) *mwExt {
		return &mwExt{baseExt: baseExt{id: id, priority: prio}, wrap: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, string(id))
				next.ServeHTTP(w, r)
			})
		}}
	}
	mgr := managerWith(t, mk("auth.inner", 0), mk("auth.outer", 50))

	h := WrapHTTP(mgr, textHandler("ok"))
	if code, body := get(t, h, "/anything"); code != http.StatusOK || body != "ok" {
		t.Fatalf("got %d %q", code, body)
	}
	if len(order) != 2 || order[0] != "auth.outer" || order[1] != "auth.inner" {
		t.Errorf("order = %v", order)
	}
}

func TestWrapHTTPCanRejectRequest(t *testing.T) {
	mgr := managerWith(t, &mwExt{baseExt: baseExt{id: "auth.acme"}, wrap: func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}})
	h := WrapHTTP(mgr, textHandler("secret"))
	if code, body := get(t, h, "/anything"); code != http.StatusForbidden || strings.Contains(body, "secret") {
		t.Errorf("got %d %q", code, body)
	}
}

func TestWrapHTTPContainsPanicAndNil(t *testing.T) {
	mgr := managerWith(t,
		&mwExt{baseExt: baseExt{id: "auth.bad"}, wrap: func(http.Handler) http.Handler { panic("boom") }},
		&mwExt{baseExt: baseExt{id: "auth.nil"}, wrap: func(http.Handler) http.Handler { return nil }},
	)
	h := WrapHTTP(mgr, textHandler("ok"))
	if code, body := get(t, h, "/anything"); code != http.StatusOK || body != "ok" {
		t.Errorf("got %d %q", code, body)
	}
}

func TestWrapHTTPNilManagerIsIdentity(t *testing.T) {
	base := textHandler("ok")
	if got := WrapHTTP(nil, base); got == nil {
		t.Fatal("nil manager dropped the handler")
	}
	_ = context.Background()
}
