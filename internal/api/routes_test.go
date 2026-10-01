package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/project"
)

// TestRegisterRoutesNoPatternConflict guards against http.ServeMux pattern
// conflicts, which panic at registration time and therefore crash every command
// that builds an API server (`pando app`, `pando serve`, ...) before it can
// print anything useful.
func TestRegisterRoutesNoPatternConflict(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerRoutes panicked: %v", r)
		}
	}()

	(&Server{}).registerRoutes(http.NewServeMux())
}

func TestProjectRoutesPreferSpecificControlPaths(t *testing.T) {
	projectPath := t.TempDir()
	openSnapshot := newWebSnapshot("p1", "One", projectPath, 4310, 9101, project.WebStateRunning, mustParseRFC3339(t, "2026-10-01T20:00:00Z"))

	child := newChildTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, r.Method+" "+r.URL.Path)
	}))

	s := &Server{
		token: "parent-token",
		app: &app.App{
			Projects: stubProjectService{projects: map[string]*project.Project{
				"p1":  {ID: "p1", Name: "One", Path: projectPath},
				"web": {ID: "web", Name: "Wildcard", Path: t.TempDir()},
			}},
		},
		projectManager: fakeProjectManager{
			openWebFn: func(_ context.Context, projectID string) (project.WebInstanceSnapshot, error) {
				if projectID != "p1" {
					t.Fatalf("OpenWeb projectID = %q, want p1", projectID)
				}
				return openSnapshot, nil
			},
			webInstancesFn: func() []project.WebInstanceSnapshot {
				return []project.WebInstanceSnapshot{openSnapshot}
			},
		},
		config: ServerConfig{Host: "127.0.0.1", Port: 8765, StartupMode: "serve"},
		projectWebProxyLookup: func(projectID string) (string, string, http.RoundTripper, bool) {
			return child.URL, "child-token", child.Client().Transport, projectID == "p1"
		},
	}
	parent := newProjectsWebHTTPServer(t, s)

	postOpen, err := http.NewRequest(http.MethodPost, parent.URL+"/api/v1/projects/p1/web/open", nil)
	if err != nil {
		t.Fatalf("NewRequest POST open: %v", err)
	}
	postOpen.Header.Set("X-Pando-Token", "parent-token")
	openResp, err := parent.Client().Do(postOpen)
	if err != nil {
		t.Fatalf("Do POST open: %v", err)
	}
	defer openResp.Body.Close()
	if openResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /web/open status = %d, want 200", openResp.StatusCode)
	}
	if body := readAllString(t, openResp.Body); !strings.Contains(body, `"status":"opened"`) {
		t.Fatalf("POST /web/open body = %q", body)
	}

	getSPA, err := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/p1/web/open?token=parent-token", nil)
	if err != nil {
		t.Fatalf("NewRequest GET open: %v", err)
	}
	getSPA.Header.Set("X-Pando-Token", "parent-token")
	getResp, err := parent.Client().Do(getSPA)
	if err != nil {
		t.Fatalf("Do GET open: %v", err)
	}
	defer getResp.Body.Close()
	if body := strings.TrimSpace(readAllString(t, getResp.Body)); body != "GET /open" {
		t.Fatalf("GET /web/open body = %q, want %q", body, "GET /open")
	}

	postProxy, err := http.NewRequest(http.MethodPost, parent.URL+"/api/v1/projects/p1/web/api/v1/ping", nil)
	if err != nil {
		t.Fatalf("NewRequest POST proxy: %v", err)
	}
	postProxy.Header.Set("X-Pando-Token", "parent-token")
	proxyResp, err := parent.Client().Do(postProxy)
	if err != nil {
		t.Fatalf("Do POST proxy: %v", err)
	}
	defer proxyResp.Body.Close()
	if body := strings.TrimSpace(readAllString(t, proxyResp.Body)); body != "POST /api/v1/ping" {
		t.Fatalf("POST /web/api/v1/ping body = %q, want %q", body, "POST /api/v1/ping")
	}

	listReq, err := http.NewRequest(http.MethodGet, parent.URL+"/api/v1/projects/web", nil)
	if err != nil {
		t.Fatalf("NewRequest GET /projects/web: %v", err)
	}
	listReq.Header.Set("X-Pando-Token", "parent-token")
	listResp, err := parent.Client().Do(listReq)
	if err != nil {
		t.Fatalf("Do GET /projects/web: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /projects/web status = %d, want 200", listResp.StatusCode)
	}
	if body := readAllString(t, listResp.Body); !strings.Contains(body, `"instances"`) || strings.Contains(body, `"project":{"id":"web"`) {
		t.Fatalf("GET /projects/web body = %q", body)
	}
}

func mustParseRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("Parse %q: %v", value, err)
	}
	return parsed
}
