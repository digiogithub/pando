package extension

import "net/http"

// The HTTP contract uses net/http directly. Unlike the tool and command
// contracts there is nothing to abstract away: net/http is the standard
// library, an extension and core already agree on it, and inventing a
// handler shape of our own would only lose the ecosystem of middleware that
// works with http.Handler.

// Route is one HTTP endpoint contributed by an extension.
type Route struct {
	// Pattern is a net/http ServeMux pattern *relative to the extension's base
	// path*, without a leading slash: "ping", "reports/{id}", "GET /status".
	// A method prefix is honoured exactly as ServeMux honours it.
	Pattern string
	// Handler serves the route.
	Handler http.Handler
}

// HTTPEndpointProvider is implemented by extensions that expose HTTP endpoints.
//
// Every route is mounted under /api/ext/<BasePath>/, never at an arbitrary
// path. That boundary is what makes the API surface auditable: core routes and
// extension routes can never collide, and a reverse proxy can treat the whole
// extension surface as one prefix.
type HTTPEndpointProvider interface {
	Extension
	// BasePath is the single path segment this extension owns under /api/ext/.
	// It must be a valid ID segment (lowercase letters, digits, '_' or '-') and
	// unique in the build; a collision is refused rather than resolved.
	BasePath() string
	// Routes returns the endpoints to mount. It is called once, when the API
	// server builds its mux, so the extension must already be provisioned —
	// which it is: the server is built after extensions load.
	Routes() []Route
}

// HTTPMiddlewareProvider is implemented by extensions that wrap the whole API
// handler: authentication, SSO, audit logging, rate limiting, request tagging.
//
// This is the hook an enterprise identity module uses for audit logging, SSO
// identity propagation, tenant tagging or rate limiting. It sees every request
// that reaches the API routes — core routes and extension routes alike — and a
// middleware that rejects a request has genuinely rejected it.
//
// It does *not* replace core's own protection: extension middleware runs inside
// core's CORS, basic-auth and token checks, so a request that fails those never
// reaches it. That is deliberate — adding an extension must not be able to
// weaken the API. Static WebUI assets are served outside this chain too.
type HTTPMiddlewareProvider interface {
	Extension
	// Priority orders middleware. Lower priority sits closer to the routes;
	// the highest priority is outermost and sees the request first. Ties break
	// on extension ID so ordering is stable across builds.
	Priority() int
	// WrapHTTP returns a handler wrapping next. Returning next unchanged is
	// valid and means "no opinion for this configuration".
	WrapHTTP(next http.Handler) http.Handler
}
