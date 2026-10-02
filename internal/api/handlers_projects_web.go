package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// projectWebCookieName is the cookie that authenticates browser-initiated
// loads of a project's WebUI through the proxy: the iframe navigation and the
// scripts, styles and images it pulls cannot carry the X-Pando-Token header.
// It holds a random secret minted at server start (never the API token), is
// HttpOnly and SameSite=Strict, is scoped to projectWebCookiePath, and is
// honoured only on proxy paths (see isProjectWebCookiePath); it is never
// forwarded to the child. Requests it alone authenticates must also pass
// cookieRequestAllowed.
const (
	projectWebCookieName = "pando_project_web"
	projectWebCookiePath = "/api/v1/projects/"
)

// setProjectWebCookie issues the proxy cookie. It is called by the endpoints
// the WebUI uses right before showing project frames (open and list).
func (s *Server) setProjectWebCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     projectWebCookieName,
		Value:    s.projectWebCookieSecret,
		Path:     projectWebCookiePath,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

// isProjectWebCookiePath reports whether path is served by the project web
// proxy, where the proxy cookie may stand in for the API token. The open and
// close control endpoints are excluded: the WebUI calls them with the header.
func isProjectWebCookiePath(method, path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/v1/projects/")
	if !ok {
		return false
	}
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return false
	}
	tail := rest[slash:]
	if method == http.MethodPost && (tail == "/web/open" || tail == "/web/close") {
		return false
	}
	return tail == "/web" || strings.HasPrefix(tail, "/web/")
}

// hasValidProjectWebCookie reports whether r carries the proxy cookie with this
// server's cookie secret.
func (s *Server) hasValidProjectWebCookie(r *http.Request) bool {
	c, err := r.Cookie(projectWebCookieName)
	if err != nil || c.Value == "" || s.projectWebCookieSecret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.projectWebCookieSecret)) == 1
}

// cookieRequestAllowed decides whether a request authenticated only by the
// ambient proxy cookie may proceed. Plain reads are free; every other method and
// every websocket upgrade needs proof that the browser issued it from this very
// origin, so a page on another port or site cannot ride the cookie.
func cookieRequestAllowed(r *http.Request) bool {
	upgrade := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && !upgrade {
		return true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Host, r.Host)
}

// stripProjectWebCookie removes the proxy cookie from a request bound for a
// child, keeping any other cookie intact.
func stripProjectWebCookie(req *http.Request) {
	cookies := req.Cookies()
	req.Header.Del("Cookie")
	for _, c := range cookies {
		if c.Name != projectWebCookieName {
			req.AddCookie(c)
		}
	}
}

type projectWebProxyTarget struct {
	baseURL   string
	apiToken  string
	transport http.RoundTripper
}

func projectWebBrowserURL(projectID string) string {
	return projectWebPrefix(projectID) + "/"
}

func (s *Server) handleProjectWebRedirect(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path + "/"
	if rawQuery := r.URL.RawQuery; rawQuery != "" {
		target += "?" + rawQuery
	}
	http.Redirect(w, r, target, http.StatusPermanentRedirect)
}

func (s *Server) handleProjectWebProxy(w http.ResponseWriter, r *http.Request) {
	target, ok := s.lookupProjectWebProxyTarget(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_web_not_open"})
		return
	}
	if target.transport == nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "project_web_unavailable"})
		return
	}

	prefix := projectWebPrefix(r.PathValue("id"))
	pathSuffix, rawPathSuffix, valid := validateProjectWebProxyPath(r, prefix)
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_project_web_path"})
		return
	}

	// The framed UI must never learn the child's token: answer its token
	// exchange here with a placeholder. Its requests are authenticated by the
	// proxy cookie and the proxy swaps in the real header.
	if pathSuffix == "/api/v1/token" {
		writeJSON(w, http.StatusOK, map[string]string{"token": projectWebPlaceholderToken})
		return
	}
	// Design previews are served from the child's origin, which is the parent's
	// origin here, with no sandbox: hostile project files could script the
	// parent UI. Refuse them until previews get an opaque or separate origin.
	if pathSuffix == "/preview" || strings.HasPrefix(pathSuffix, "/preview/") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not_available_in_project_tab"})
		return
	}

	baseURL, err := url.Parse(target.baseURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "project_web_unavailable"})
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = baseURL.Scheme
			req.URL.Host = baseURL.Host
			req.Host = baseURL.Host
			req.URL.Path = singleJoiningSlash(baseURL.Path, pathSuffix)
			if rawPathSuffix != "" {
				req.URL.RawPath = singleJoiningSlash(baseURL.EscapedPath(), rawPathSuffix)
			} else {
				req.URL.RawPath = ""
			}

			query := req.URL.Query()
			query.Del("token")
			req.URL.RawQuery = query.Encode()
			req.RequestURI = ""

			req.Header.Del("X-Pando-Token")
			req.Header.Del("Authorization")
			stripProjectWebCookie(req)
			req.Header.Set("X-Pando-Token", target.apiToken)
			req.Header.Set("X-Pando-Client", "web")
		},
		Transport:     target.transport,
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			stripProjectWebProxyResponseHeaders(resp.Header)
			if err := rewriteProjectWebLocation(resp.Header, baseURL, prefix); err != nil {
				return err
			}
			protectProjectWebFraming(resp.Header)
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			if errors.Is(err, errProjectWebBadRedirect) {
				writeJSON(rw, http.StatusBadGateway, map[string]string{"error": "project_web_bad_redirect"})
				return
			}
			if err != nil && req.Context().Err() != nil && (req.Context().Err() == context.Canceled || req.Context().Err() == context.DeadlineExceeded) {
				return
			}
			writeJSON(rw, http.StatusBadGateway, map[string]string{"error": "project_web_unavailable"})
		},
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) lookupProjectWebProxyTarget(projectID string) (projectWebProxyTarget, bool) {
	if s.projectWebProxyLookup != nil {
		baseURL, apiToken, transport, ok := s.projectWebProxyLookup(projectID)
		if !ok || strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiToken) == "" {
			return projectWebProxyTarget{}, false
		}
		return projectWebProxyTarget{baseURL: baseURL, apiToken: apiToken, transport: transport}, true
	}
	mgr := s.projectManagerAPI()
	if mgr == nil {
		return projectWebProxyTarget{}, false
	}
	baseURL, apiToken, transport, ok := mgr.WebProxyTarget(projectID)
	if !ok {
		return projectWebProxyTarget{}, false
	}
	return projectWebProxyTarget{
		baseURL:   baseURL,
		apiToken:  apiToken,
		transport: transport,
	}, true
}

func projectWebPrefix(projectID string) string {
	return "/api/v1/projects/" + projectID + "/web"
}

func validateProjectWebProxyPath(r *http.Request, prefix string) (pathSuffix, rawPathSuffix string, ok bool) {
	pathSuffix = strings.TrimPrefix(r.URL.Path, prefix)
	rawPathSuffix = strings.TrimPrefix(projectWebRawPath(r), prefix)
	if pathSuffix == "" {
		pathSuffix = "/"
	}
	if rawPathSuffix == "" {
		rawPathSuffix = "/"
	}

	if !validateProjectWebDecodedPath(pathSuffix) {
		return "", "", false
	}
	if !validateProjectWebRawPath(rawPathSuffix) {
		return "", "", false
	}
	return pathSuffix, rawPathSuffix, true
}

func projectWebRawPath(r *http.Request) string {
	requestURI := r.RequestURI
	if idx := strings.IndexByte(requestURI, '?'); idx >= 0 {
		return requestURI[:idx]
	}
	if requestURI != "" {
		return requestURI
	}
	return r.URL.EscapedPath()
}

func validateProjectWebDecodedPath(pathSuffix string) bool {
	for _, segment := range strings.Split(strings.TrimPrefix(pathSuffix, "/"), "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validateProjectWebRawPath(rawPathSuffix string) bool {
	for _, rawSegment := range strings.Split(strings.TrimPrefix(rawPathSuffix, "/"), "/") {
		if rawSegment == "" {
			continue
		}
		segment, err := url.PathUnescape(rawSegment)
		if err != nil {
			return false
		}
		if segment == "." || segment == ".." {
			return false
		}
		if strings.Contains(segment, "/") || strings.Contains(segment, `\`) {
			return false
		}
	}
	return true
}

// projectWebPlaceholderToken is what the proxy answers to the framed UI's token
// exchange. It authenticates nothing: the proxy replaces it with the real token.
const projectWebPlaceholderToken = "proxied"

var errProjectWebBadRedirect = errors.New("project web: redirect leaves the project child")

// rewriteProjectWebLocation maps a redirect that points inside the child onto
// the parent's prefix and rejects one that points anywhere else.
func rewriteProjectWebLocation(headers http.Header, baseURL *url.URL, prefix string) error {
	loc := headers.Get("Location")
	if loc == "" {
		return nil
	}
	u, err := url.Parse(loc)
	if err != nil {
		return errProjectWebBadRedirect
	}
	if u.Scheme != "" || u.Host != "" {
		if !strings.EqualFold(u.Scheme, baseURL.Scheme) || !strings.EqualFold(u.Host, baseURL.Host) {
			return errProjectWebBadRedirect
		}
	} else if strings.HasPrefix(loc, "//") {
		return errProjectWebBadRedirect
	}
	if u.Scheme == "" && u.Host == "" && !strings.HasPrefix(u.Path, "/") {
		// Relative to the current document: the browser resolves it under the
		// prefix already.
		return nil
	}
	path := u.EscapedPath()
	if path == prefix || strings.HasPrefix(path, prefix+"/") {
		path = strings.TrimPrefix(path, prefix)
	}
	out := prefix + path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	headers.Set("Location", out)
	return nil
}

// protectProjectWebFraming keeps proxied HTML from being framed by another
// origin.
func protectProjectWebFraming(headers http.Header) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(headers.Get("Content-Type"))), "text/html") {
		return
	}
	const directive = "frame-ancestors 'self'"
	values := headers.Values("Content-Security-Policy")
	if len(values) == 0 {
		headers.Set("Content-Security-Policy", directive)
	} else {
		merged := make([]string, 0, len(values))
		for _, value := range values {
			parts := strings.Split(value, ";")
			kept := parts[:0]
			for _, part := range parts {
				name, _, _ := strings.Cut(strings.TrimSpace(part), " ")
				if strings.EqualFold(name, "frame-ancestors") || strings.TrimSpace(part) == "" {
					continue
				}
				kept = append(kept, strings.TrimSpace(part))
			}
			merged = append(merged, strings.Join(append(kept, directive), "; "))
		}
		headers["Content-Security-Policy"] = merged
	}
	headers.Set("X-Frame-Options", "SAMEORIGIN")
}

func stripProjectWebProxyResponseHeaders(headers http.Header) {
	for name := range headers {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), "Access-Control-") {
			headers.Del(name)
		}
	}
	headers.Del("X-Pando-Token")
	headers.Del("Authorization")
	headers.Del("Set-Cookie")
}

func singleJoiningSlash(a, b string) string {
	switch {
	case strings.HasSuffix(a, "/") && strings.HasPrefix(b, "/"):
		return a + b[1:]
	case !strings.HasSuffix(a, "/") && !strings.HasPrefix(b, "/"):
		return a + "/" + b
	default:
		return a + b
	}
}
