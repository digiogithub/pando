package api

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

type projectWebProxyTarget struct {
	baseURL   string
	apiToken  string
	transport http.RoundTripper
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
			req.Header.Set("X-Pando-Token", target.apiToken)
			req.Header.Set("X-Pando-Client", "web")
		},
		Transport:     target.transport,
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			stripProjectWebProxyResponseHeaders(resp.Header)
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
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
	if s.app == nil || s.app.ProjectManager == nil {
		return projectWebProxyTarget{}, false
	}

	inst, ok := s.app.ProjectManager.WebInstance(projectID)
	if !ok || inst == nil || strings.TrimSpace(inst.BaseURL()) == "" || strings.TrimSpace(inst.APIToken()) == "" {
		return projectWebProxyTarget{}, false
	}

	return projectWebProxyTarget{
		baseURL:   inst.BaseURL(),
		apiToken:  inst.APIToken(),
		transport: s.app.ProjectManager.WebTransport(),
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

func stripProjectWebProxyResponseHeaders(headers http.Header) {
	for name := range headers {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), "Access-Control-") {
			headers.Del(name)
		}
	}
	headers.Del("X-Pando-Token")
	headers.Del("Authorization")
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
