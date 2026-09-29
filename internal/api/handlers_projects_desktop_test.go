package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/project"
)

type stubProjectService struct {
	project.Service
	projects map[string]*project.Project
}

func (s stubProjectService) Get(_ context.Context, id string) (*project.Project, error) {
	if p, ok := s.projects[id]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func openDesktopRequest(t *testing.T, s *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+id+"/open-desktop", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	s.handleOpenProjectDesktop(rec, req)
	return rec
}

func stubDesktopLaunch(t *testing.T, live bool) *[]string {
	t.Helper()
	var spawned []string
	origSpawn, origLive := spawnDesktopInstance, liveDesktopForPath
	spawnDesktopInstance = func(dir string) error { spawned = append(spawned, dir); return nil }
	liveDesktopForPath = func(string) bool { return live }
	t.Cleanup(func() { spawnDesktopInstance, liveDesktopForPath = origSpawn, origLive })
	return &spawned
}

func TestOpenProjectDesktopSpawnsInstance(t *testing.T) {
	dir := t.TempDir()
	spawned := stubDesktopLaunch(t, false)
	s := &Server{
		app:    &app.App{Projects: stubProjectService{projects: map[string]*project.Project{"p1": {ID: "p1", Path: dir}}}},
		config: ServerConfig{StartupMode: "desktop", CWD: t.TempDir()},
	}

	rec := openDesktopRequest(t, s, "p1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"opened"`) {
		t.Fatalf("unexpected response %d: %s", rec.Code, rec.Body.String())
	}
	if len(*spawned) != 1 || (*spawned)[0] != dir {
		t.Fatalf("expected one spawn for %s, got %v", dir, *spawned)
	}
}

func TestOpenProjectDesktopSkipsLiveOrCurrent(t *testing.T) {
	dir := t.TempDir()
	projects := stubProjectService{projects: map[string]*project.Project{"p1": {ID: "p1", Path: dir}}}

	spawned := stubDesktopLaunch(t, true)
	s := &Server{app: &app.App{Projects: projects}, config: ServerConfig{StartupMode: "desktop", CWD: t.TempDir()}}
	if rec := openDesktopRequest(t, s, "p1"); !strings.Contains(rec.Body.String(), `"already_open"`) {
		t.Fatalf("expected already_open, got %s", rec.Body.String())
	}

	s.config.CWD = dir
	if rec := openDesktopRequest(t, s, "p1"); !strings.Contains(rec.Body.String(), `"current"`) {
		t.Fatalf("expected current, got %s", rec.Body.String())
	}
	if len(*spawned) != 0 {
		t.Fatalf("expected no spawn, got %v", *spawned)
	}
}

func TestOpenProjectDesktopRejectsNonDesktopAndMissing(t *testing.T) {
	spawned := stubDesktopLaunch(t, false)
	projects := stubProjectService{projects: map[string]*project.Project{
		"gone": {ID: "gone", Path: "/nonexistent/pando-open-desktop-test"},
	}}

	s := &Server{app: &app.App{Projects: projects}, config: ServerConfig{StartupMode: "serve"}}
	if rec := openDesktopRequest(t, s, "gone"); rec.Code != http.StatusConflict {
		t.Fatalf("serve mode: expected 409, got %d", rec.Code)
	}

	s.config.StartupMode = "desktop"
	if rec := openDesktopRequest(t, s, "gone"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing folder: expected 404, got %d", rec.Code)
	}
	if rec := openDesktopRequest(t, s, "unknown"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown project: expected 404, got %d", rec.Code)
	}
	if len(*spawned) != 0 {
		t.Fatalf("expected no spawn, got %v", *spawned)
	}
}
