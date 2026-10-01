package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newChildModeServer() *Server {
	return &Server{
		config: ServerConfig{
			Version:          "1.2.3",
			StartupMode:      "project-child",
			ParentInstanceID: "parent-1",
			ProjectID:        "project-1",
			ProjectName:      "Project One",
			PublicBasePath:   "",
		},
	}
}

func TestHealthReportsProjectChildServerInfo(t *testing.T) {
	s := newChildModeServer()

	rec := httptest.NewRecorder()
	s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var payload healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.StartupMode != "project-child" {
		t.Fatalf("startup_mode = %q, want project-child", payload.StartupMode)
	}
	if payload.ParentInstanceID != "parent-1" {
		t.Fatalf("parent_instance_id = %q, want parent-1", payload.ParentInstanceID)
	}
	if payload.ProjectID != "project-1" {
		t.Fatalf("project_id = %q, want project-1", payload.ProjectID)
	}
	if payload.ProjectName != "Project One" {
		t.Fatalf("project_name = %q, want Project One", payload.ProjectName)
	}
	if payload.PublicBasePath != "" {
		t.Fatalf("public_base_path = %q, want empty", payload.PublicBasePath)
	}
}

func TestChildModeProjectAndInstanceHandlersReturnConflict(t *testing.T) {
	s := newChildModeServer()

	for _, tc := range []struct {
		name    string
		request *http.Request
		handler http.HandlerFunc
	}{
		{
			name:    "projects list",
			request: httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil),
			handler: s.handleListProjects,
		},
		{
			name:    "instances list",
			request: httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil),
			handler: s.handleListInstances,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, tc.request)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409", rec.Code)
			}
			if got := rec.Body.String(); got != "{\"error\":\"not_available_in_child\"}\n" {
				t.Fatalf("body = %q", got)
			}
		})
	}
}
