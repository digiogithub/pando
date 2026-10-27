package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectsAndDerivedClient(t *testing.T) {
	var childToken string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"parent-tok"}`))
	})
	mux.HandleFunc("/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"p1","name":"One","path":"/x","web_state":"running","web_port":9}]}`))
	})
	mux.HandleFunc("/api/v1/projects/p1/web/open", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"project_needs_init","path":"/x"}`))
	})
	mux.HandleFunc("/api/v1/projects/p1/web/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		childToken = r.Header.Get(HeaderToken)
		_, _ = w.Write([]byte(`{"sessions":[]}`))
	})
	mux.HandleFunc("/api/v1/projects/p1/web/api/v1/token", func(w http.ResponseWriter, r *http.Request) {
		t.Error("derived client must not fetch the placeholder child token")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ps, err := c.ListProjects(ctx)
	if err != nil || len(ps) != 1 || ps[0].WebState != "running" || ps[0].WebPort != 9 {
		t.Fatalf("projects %+v %v", ps, err)
	}
	_, err = c.OpenProjectWeb(ctx, "p1")
	if pe := ParseProjectError(err); pe.Error != "project_needs_init" || pe.Path != "/x" {
		t.Fatalf("error body %+v (%v)", pe, err)
	}
	child := c.Derive(ProjectWebPrefix("p1"))
	if !strings.HasSuffix(child.BaseURL(), "/api/v1/projects/p1/web") {
		t.Fatal(child.BaseURL())
	}
	var out map[string]any
	if err := child.Get(ctx, "/api/v1/sessions", &out); err != nil {
		t.Fatal(err)
	}
	if childToken != "parent-tok" {
		t.Fatalf("child call carried token %q", childToken)
	}
}
