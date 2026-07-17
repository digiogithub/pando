package api

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// basicAuthRealm is the realm advertised to non-browser clients (curl, scripts).
const basicAuthRealm = "Pando"

// webClientHeader is set by the Web UI on every API call. Its presence suppresses
// the WWW-Authenticate challenge so the SPA can render its own login dialog
// instead of the browser's native credential prompt.
const webClientHeader = "X-Pando-Client"

// isLoopbackHost reports whether a bind host only accepts local connections.
// An empty host or a wildcard bind (0.0.0.0, ::) is reachable from the network
// and therefore not loopback.
func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// basicAuthEnforced reports whether the request must present credentials.
// Basic auth only applies to the API surface, and only once the server is
// exposed — that is, started on a non-loopback host. The decision deliberately
// ignores where the request came from: a server on 0.0.0.0 is reachable from the
// network, so a local browser must authenticate too, otherwise anyone with a
// foothold on this machine (or any process able to reach 127.0.0.1) walks past
// the gate.
func (s *Server) basicAuthEnforced(r *http.Request) bool {
	if r.URL.Path == "/health" || !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	if isLoopbackHost(s.config.Host) {
		return false
	}
	cfg := config.Get()
	if cfg == nil || !cfg.Server.BasicAuth.Enabled || len(cfg.Server.BasicAuth.Users) == 0 {
		return false
	}
	return true
}

// basicAuthCredentialsValid checks user/password against every configured user
// without short-circuiting, so a wrong username and a wrong password cost the same.
func basicAuthCredentialsValid(users []config.BasicAuthUser, username, password string) bool {
	valid := false
	for _, user := range users {
		userMatch := subtle.ConstantTimeCompare([]byte(user.Username), []byte(username))
		passMatch := subtle.ConstantTimeCompare([]byte(user.Password), []byte(password))
		if userMatch&passMatch == 1 {
			valid = true
		}
	}
	return valid
}

// basicAuthMiddleware guards /api/ with the credentials configured in
// [Server.BasicAuth]. It reads the live config on every request so toggling the
// setting from the WebUI Access panel applies without a restart.
//
// A request already carrying the API token passes through: the token is the
// session credential, handed out by /api/v1/token, which is itself behind this
// gate. Clients therefore authenticate once and keep using the token — including
// SSE streams, which can only pass it as a query parameter and cannot set an
// Authorization header.
func (s *Server) basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.basicAuthEnforced(r) || s.hasValidToken(r) {
			next.ServeHTTP(w, r)
			return
		}

		username, password, ok := r.BasicAuth()
		if ok && basicAuthCredentialsValid(config.Get().Server.BasicAuth.Users, username, password) {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get(webClientHeader) == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+basicAuthRealm+`", charset="UTF-8"`)
		}
		writeError(w, http.StatusUnauthorized, "basic_auth_required")
	})
}
