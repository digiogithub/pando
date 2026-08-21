package extensions

import (
	"net/http"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// ExtRoutePrefix is the single prefix every extension HTTP route lives under.
// Nothing an extension serves is reachable outside it, which is what makes the
// API surface auditable and lets a reverse proxy treat extensions as one block.
const ExtRoutePrefix = "/api/ext/"

// RegisterRoutes mounts the routes of every loaded HTTPEndpointProvider on mux.
// It returns the full patterns registered, for logging and tests.
//
// A nil manager registers nothing, so callers need no guard.
func RegisterRoutes(mgr *extension.Manager, mux *http.ServeMux) []string {
	if mgr == nil || mux == nil {
		return nil
	}

	var registered []string
	bases := make(map[string]extension.ID)
	for _, p := range extension.Capability[extension.HTTPEndpointProvider](mgr) {
		id := p.ExtensionInfo().ID
		base := strings.Trim(strings.TrimSpace(p.BasePath()), "/")
		if !validBase(base) {
			logging.Error("Extension base path rejected",
				"extension", id, "base_path", p.BasePath(),
				"reason", "must be a single segment of lowercase letters, digits, '_' or '-'")
			continue
		}
		if owner, dup := bases[base]; dup {
			logging.Error("Duplicate extension base path ignored",
				"base_path", base, "extension", id, "already_owned_by", owner)
			continue
		}
		bases[base] = id

		for _, route := range p.Routes() {
			pattern, ok := fullPattern(base, route.Pattern)
			if !ok || route.Handler == nil {
				logging.Error("Extension route ignored",
					"extension", id, "pattern", route.Pattern, "has_handler", route.Handler != nil)
				continue
			}
			// A panic here is a duplicate-pattern programming error in the
			// extension; contain it so one bad route cannot stop the server
			// from starting with the rest of the API.
			if err := safeHandle(mux, pattern, route.Handler); err != nil {
				logging.Error("Extension route rejected", "extension", id, "pattern", pattern, "error", err)
				continue
			}
			registered = append(registered, pattern)
			logging.Debug("Extension route mounted", "extension", id, "pattern", pattern)
		}
	}
	return registered
}

// validBase reports whether base is a single ID-shaped path segment.
func validBase(base string) bool {
	if base == "" || strings.Contains(base, "/") {
		return false
	}
	return extension.ID(base).Valid()
}

// fullPattern builds the ServeMux pattern for one route, preserving a leading
// method ("GET reports/{id}" becomes "GET /api/ext/acme/reports/{id}").
func fullPattern(base, pattern string) (string, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", false
	}
	method := ""
	if i := strings.Index(pattern, " "); i > 0 {
		method, pattern = pattern[:i], strings.TrimSpace(pattern[i+1:])
	}
	pattern = strings.TrimPrefix(pattern, "/")
	full := ExtRoutePrefix + base + "/" + pattern
	if method != "" {
		full = method + " " + full
	}
	return full, true
}

func safeHandle(mux *http.ServeMux, pattern string, h http.Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errFromPanic(r)
		}
	}()
	mux.Handle(pattern, h)
	return nil
}

// WrapHTTP applies every loaded HTTPMiddlewareProvider around next. The highest
// priority ends up outermost, so it sees the request first — which is what an
// authentication middleware needs.
func WrapHTTP(mgr *extension.Manager, next http.Handler) http.Handler {
	if mgr == nil || next == nil {
		return next
	}
	mws := extension.Capability[extension.HTTPMiddlewareProvider](mgr)
	if len(mws) == 0 {
		return next
	}
	sort.SliceStable(mws, func(i, j int) bool {
		pi, pj := mws[i].Priority(), mws[j].Priority()
		if pi != pj {
			return pi < pj
		}
		return mws[i].ExtensionInfo().ID < mws[j].ExtensionInfo().ID
	})

	// Applied in ascending priority: each wrap puts the new middleware outside
	// the previous one, so the last (highest) ends up outermost.
	for _, mw := range mws {
		wrapped := safeWrap(mw, next)
		if wrapped == nil {
			logging.Error("Extension HTTP middleware returned nil, ignored", "extension", mw.ExtensionInfo().ID)
			continue
		}
		next = wrapped
	}
	return next
}

// safeWrap contains a panic in WrapHTTP itself. A middleware that cannot even
// build is dropped: the alternative is a server that refuses to start.
//
// Note this only covers construction. A panic *inside* the returned handler is
// the http.Server's business, and it already recovers per request.
func safeWrap(mw extension.HTTPMiddlewareProvider, next http.Handler) (out http.Handler) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension HTTP middleware panicked while wrapping",
				"extension", mw.ExtensionInfo().ID, "panic", r)
			out = nil
		}
	}()
	return mw.WrapHTTP(next)
}
