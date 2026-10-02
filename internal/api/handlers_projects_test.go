package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/project"
	"github.com/digiogithub/pando/internal/pubsub"
)

type fakeProjectManager struct {
	runtimeFn        func(projectID, path string) (bool, bool, int)
	delegationInfoFn func(projectID string) (int, bool, bool)
	listFn           func(context.Context) ([]project.Project, error)
	registerFn       func(context.Context, string, string) (*project.Project, error)
	unregisterFn     func(context.Context, string) error
	activateFn       func(context.Context, string) error
	deactivateFn     func(context.Context) error
	completeInitFn   func(context.Context, string) error
	activeProjectFn  func(context.Context) (*project.Project, error)
	stopReportFn     func(context.Context, string) (int, error)
	renameFn         func(context.Context, string, string) error
	subscribeFn      func(context.Context) <-chan pubsub.Event[project.ManagerEvent]
	openWebFn        func(context.Context, string) (project.WebInstanceSnapshot, error)
	closeWebFn       func(context.Context, string) error
	webInstanceFn    func(string) (project.WebInstanceSnapshot, bool)
	webInstancesFn   func() []project.WebInstanceSnapshot
	webProxyTargetFn func(string) (string, string, http.RoundTripper, bool)
}

func (m fakeProjectManager) Runtime(projectID, path string) (bool, bool, int) {
	if m.runtimeFn != nil {
		return m.runtimeFn(projectID, path)
	}
	return false, false, 0
}

func (m fakeProjectManager) DelegationInfo(projectID string) (int, bool, bool) {
	if m.delegationInfoFn != nil {
		return m.delegationInfoFn(projectID)
	}
	return 0, false, false
}

func (m fakeProjectManager) List(ctx context.Context) ([]project.Project, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, nil
}

func (m fakeProjectManager) Register(ctx context.Context, name, path string) (*project.Project, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, name, path)
	}
	return nil, nil
}

func (m fakeProjectManager) Unregister(ctx context.Context, projectID string) error {
	if m.unregisterFn != nil {
		return m.unregisterFn(ctx, projectID)
	}
	return nil
}

func (m fakeProjectManager) Activate(ctx context.Context, projectID string) error {
	if m.activateFn != nil {
		return m.activateFn(ctx, projectID)
	}
	return nil
}

func (m fakeProjectManager) Deactivate(ctx context.Context) error {
	if m.deactivateFn != nil {
		return m.deactivateFn(ctx)
	}
	return nil
}

func (m fakeProjectManager) CompleteInit(ctx context.Context, projectID string) error {
	if m.completeInitFn != nil {
		return m.completeInitFn(ctx, projectID)
	}
	return nil
}

func (m fakeProjectManager) ActiveProject(ctx context.Context) (*project.Project, error) {
	if m.activeProjectFn != nil {
		return m.activeProjectFn(ctx)
	}
	return nil, nil
}

func (m fakeProjectManager) StopReport(ctx context.Context, projectID string) (int, error) {
	if m.stopReportFn != nil {
		return m.stopReportFn(ctx, projectID)
	}
	return 0, nil
}

func (m fakeProjectManager) Rename(ctx context.Context, projectID, newName string) error {
	if m.renameFn != nil {
		return m.renameFn(ctx, projectID, newName)
	}
	return nil
}

func (m fakeProjectManager) Subscribe(ctx context.Context) <-chan pubsub.Event[project.ManagerEvent] {
	if m.subscribeFn != nil {
		return m.subscribeFn(ctx)
	}
	ch := make(chan pubsub.Event[project.ManagerEvent])
	close(ch)
	return ch
}

func (m fakeProjectManager) OpenWeb(ctx context.Context, projectID string) (project.WebInstanceSnapshot, error) {
	if m.openWebFn != nil {
		return m.openWebFn(ctx, projectID)
	}
	return project.WebInstanceSnapshot{}, nil
}

func (m fakeProjectManager) CloseWeb(ctx context.Context, projectID string) error {
	if m.closeWebFn != nil {
		return m.closeWebFn(ctx, projectID)
	}
	return nil
}

func (m fakeProjectManager) WebInstance(projectID string) (project.WebInstanceSnapshot, bool) {
	if m.webInstanceFn != nil {
		return m.webInstanceFn(projectID)
	}
	return project.WebInstanceSnapshot{}, false
}

func (m fakeProjectManager) WebInstances() []project.WebInstanceSnapshot {
	if m.webInstancesFn != nil {
		return m.webInstancesFn()
	}
	return nil
}

func (m fakeProjectManager) WebProxyTarget(projectID string) (string, string, http.RoundTripper, bool) {
	if m.webProxyTargetFn != nil {
		return m.webProxyTargetFn(projectID)
	}
	return "", "", nil, false
}

func newProjectHandlerServer(projects map[string]*project.Project, mgr projectManagerAPI) *Server {
	return &Server{
		app: &app.App{
			Projects: stubProjectService{projects: projects},
		},
		projectManager: mgr,
		config:         ServerConfig{StartupMode: "serve"},
	}
}

func newWebSnapshot(id, name, path string, port, pid int, state project.WebInstanceState, startedAt time.Time) project.WebInstanceSnapshot {
	return project.WebInstanceSnapshot{
		Project:   project.Project{ID: id, Name: name, Path: path},
		Port:      port,
		PID:       pid,
		State:     state,
		StartedAt: startedAt,
	}
}

func decodeJSONBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return payload
}

func TestOpenProjectWebHandler(t *testing.T) {
	projectPath := t.TempDir()
	projectRecord := &project.Project{ID: "p1", Name: "One", Path: projectPath}
	startedAt := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	runningSnapshot := newWebSnapshot("p1", "One", projectPath, 4310, 9101, project.WebStateRunning, startedAt)

	tests := []struct {
		name         string
		projects     map[string]*project.Project
		manager      projectManagerAPI
		wantCode     int
		wantContains []string
	}{
		{
			name:     "opened",
			projects: map[string]*project.Project{"p1": projectRecord},
			manager: fakeProjectManager{
				openWebFn: func(context.Context, string) (project.WebInstanceSnapshot, error) {
					return runningSnapshot, nil
				},
			},
			wantCode:     http.StatusOK,
			wantContains: []string{`"status":"opened"`, `"project_id":"p1"`, `"web_url":"/api/v1/projects/p1/web/"`, `"web_port":4310`},
		},
		{
			name:     "already open",
			projects: map[string]*project.Project{"p1": projectRecord},
			manager: fakeProjectManager{
				webInstanceFn: func(string) (project.WebInstanceSnapshot, bool) {
					return runningSnapshot, true
				},
				openWebFn: func(context.Context, string) (project.WebInstanceSnapshot, error) {
					return runningSnapshot, nil
				},
			},
			wantCode:     http.StatusOK,
			wantContains: []string{`"status":"already_open"`},
		},
		{
			name:     "needs init",
			projects: map[string]*project.Project{"p1": projectRecord},
			manager: fakeProjectManager{
				openWebFn: func(context.Context, string) (project.WebInstanceSnapshot, error) {
					return project.WebInstanceSnapshot{}, project.ErrProjectNeedsInit
				},
			},
			wantCode:     http.StatusConflict,
			wantContains: []string{`"error":"project_needs_init"`, `"project_id":"p1"`, `"path":"` + projectPath + `"`},
		},
		{
			name:     "child instance",
			projects: map[string]*project.Project{"p1": projectRecord},
			manager: fakeProjectManager{
				openWebFn: func(context.Context, string) (project.WebInstanceSnapshot, error) {
					return project.WebInstanceSnapshot{}, project.ErrChildInstance
				},
			},
			wantCode:     http.StatusConflict,
			wantContains: []string{`"error":"child_instance"`},
		},
		{
			name:     "delegations in flight",
			projects: map[string]*project.Project{"p1": projectRecord},
			manager: fakeProjectManager{
				openWebFn: func(context.Context, string) (project.WebInstanceSnapshot, error) {
					return project.WebInstanceSnapshot{}, &project.DelegationsInFlightError{Count: 2}
				},
			},
			wantCode:     http.StatusConflict,
			wantContains: []string{`"error":"delegations_in_flight"`, `"project_id":"p1"`, `"delegations":2`},
		},
		{
			name:     "startup failed",
			projects: map[string]*project.Project{"p1": projectRecord},
			manager: fakeProjectManager{
				openWebFn: func(context.Context, string) (project.WebInstanceSnapshot, error) {
					return project.WebInstanceSnapshot{}, &project.ChildStartupError{
						Detail: "exit status 1\nboom from child",
						Cause:  project.ErrChildStartupFailed,
					}
				},
			},
			wantCode:     http.StatusBadGateway,
			wantContains: []string{`"error":"child_startup_failed"`, `"detail":"exit status 1\nboom from child"`},
		},
		{
			name:         "project not found",
			projects:     map[string]*project.Project{},
			manager:      fakeProjectManager{},
			wantCode:     http.StatusNotFound,
			wantContains: []string{`"error":"project not found"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newProjectHandlerServer(tc.projects, tc.manager)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/web/open", nil)
			req.SetPathValue("id", "p1")
			rec := httptest.NewRecorder()

			s.handleOpenProjectWeb(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.wantCode, rec.Body.String())
			}
			body := rec.Body.String()
			for _, want := range tc.wantContains {
				if !strings.Contains(body, want) {
					t.Fatalf("body %q does not contain %q", body, want)
				}
			}
		})
	}
}

func TestCloseProjectWebHandler(t *testing.T) {
	s := newProjectHandlerServer(nil, fakeProjectManager{
		delegationInfoFn: func(projectID string) (int, bool, bool) {
			if projectID != "p1" {
				t.Fatalf("DelegationInfo projectID = %q, want p1", projectID)
			}
			return 3, false, false
		},
		closeWebFn: func(_ context.Context, projectID string) error {
			if projectID != "p1" {
				t.Fatalf("CloseWeb projectID = %q, want p1", projectID)
			}
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/web/close", nil)
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()

	s.handleCloseProjectWeb(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if got := body["status"]; got != "closed" {
		t.Fatalf("status field = %v, want closed", got)
	}
	if got := body["project_id"]; got != "p1" {
		t.Fatalf("project_id = %v, want p1", got)
	}
	if got := body["cancelled_delegations"]; got != float64(3) {
		t.Fatalf("cancelled_delegations = %v, want 3", got)
	}
}

func TestListProjectWebInstancesHandler(t *testing.T) {
	startA := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	startB := startA.Add(time.Minute)
	s := newProjectHandlerServer(nil, fakeProjectManager{
		webInstancesFn: func() []project.WebInstanceSnapshot {
			return []project.WebInstanceSnapshot{
				newWebSnapshot("p2", "Two", "/tmp/two", 4312, 9102, project.WebStateStarting, startB),
				newWebSnapshot("p1", "One", "/tmp/one", 4310, 9101, project.WebStateRunning, startA),
			}
		},
		delegationInfoFn: func(projectID string) (int, bool, bool) {
			switch projectID {
			case "p1":
				return 2, false, true
			case "p2":
				return 0, false, true
			default:
				return 0, false, false
			}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/web", nil)
	rec := httptest.NewRecorder()
	s.handleListProjectWebInstances(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Instances []projectWebInstanceResponse `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(payload.Instances) != 2 {
		t.Fatalf("instances len = %d, want 2", len(payload.Instances))
	}
	first := payload.Instances[0]
	if first.ProjectID != "p1" || first.WebURL != "/api/v1/projects/p1/web/" || first.State != "running" {
		t.Fatalf("unexpected first instance: %+v", first)
	}
	if first.StartedAt != "2026-10-01T20:00:00Z" {
		t.Fatalf("started_at = %q, want 2026-10-01T20:00:00Z", first.StartedAt)
	}
	if first.Delegations != 2 {
		t.Fatalf("delegations = %d, want 2", first.Delegations)
	}
}

func TestEnrichRuntimeAddsWebFields(t *testing.T) {
	proj := project.Project{
		ID:     "p1",
		Name:   "One",
		Path:   "/tmp/one",
		Status: project.StatusRunning,
	}
	s := newProjectHandlerServer(nil, fakeProjectManager{
		runtimeFn: func(projectID, path string) (bool, bool, int) {
			if projectID != "p1" || path != "/tmp/one" {
				t.Fatalf("Runtime(%q, %q)", projectID, path)
			}
			return true, false, 0
		},
		delegationInfoFn: func(string) (int, bool, bool) {
			return 4, true, true
		},
		webInstanceFn: func(string) (project.WebInstanceSnapshot, bool) {
			return newWebSnapshot("p1", "One", "/tmp/one", 4310, 9101, project.WebStateRunning, time.Now().UTC()), true
		},
	})

	resp := toProjectResponse(proj)
	s.enrichRuntime(&resp, proj)

	if resp.WebState != "running" {
		t.Fatalf("web_state = %q, want running", resp.WebState)
	}
	if resp.WebPort != 4310 {
		t.Fatalf("web_port = %d, want 4310", resp.WebPort)
	}
	if resp.WebURL != "/api/v1/projects/p1/web/" {
		t.Fatalf("web_url = %q, want /api/v1/projects/p1/web/", resp.WebURL)
	}
	if resp.Delegations != 4 || !resp.DelegationSpawned {
		t.Fatalf("delegation fields = (%d, %v), want (4, true)", resp.Delegations, resp.DelegationSpawned)
	}
}

func TestProjectEventsMapWebLifecycleEventNames(t *testing.T) {
	events := make(chan pubsub.Event[project.ManagerEvent], 4)
	s := newProjectHandlerServer(nil, fakeProjectManager{
		subscribeFn: func(context.Context) <-chan pubsub.Event[project.ManagerEvent] {
			return events
		},
	})

	go func() {
		events <- pubsub.Event[project.ManagerEvent]{Type: pubsub.UpdatedEvent, Payload: project.ManagerEvent{
			Type:      project.EvWebStarted,
			ProjectID: "p1",
			Port:      4310,
		}}
		events <- pubsub.Event[project.ManagerEvent]{Type: pubsub.UpdatedEvent, Payload: project.ManagerEvent{
			Type:      project.EvWebStopped,
			ProjectID: "p1",
			Port:      4310,
		}}
		events <- pubsub.Event[project.ManagerEvent]{Type: pubsub.UpdatedEvent, Payload: project.ManagerEvent{
			Type:      project.EvWebError,
			ProjectID: "p1",
			Port:      4310,
			Error:     "boom",
		}}
		close(events)
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/events", nil)
	rec := httptest.NewRecorder()
	s.handleProjectEvents(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"event: web_started",
		"event: web_stopped",
		"event: web_error",
		`"project_id":"p1"`,
		`"web_port":4310`,
		`"error":"boom"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream %q missing %q", body, want)
		}
	}
}

func TestOpenProjectWebHandlerPassesThroughUnexpectedErrors(t *testing.T) {
	s := newProjectHandlerServer(map[string]*project.Project{
		"p1": {ID: "p1", Name: "One", Path: t.TempDir()},
	}, fakeProjectManager{
		openWebFn: func(context.Context, string) (project.WebInstanceSnapshot, error) {
			return project.WebInstanceSnapshot{}, errors.New("boom")
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/web/open", nil)
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	s.handleOpenProjectWeb(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 body=%s", rec.Code, rec.Body.String())
	}
}
