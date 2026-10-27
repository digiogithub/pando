package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
)

// Project is a registered project (web-ui Project).
type Project struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Path              string `json:"path"`
	Status            string `json:"status"`
	Initialized       bool   `json:"initialized"`
	External          bool   `json:"external,omitempty"`
	Delegations       int    `json:"delegations,omitempty"`
	DelegationSpawned bool   `json:"delegation_spawned,omitempty"`
	WebState          string `json:"web_state,omitempty"`
	WebPort           int    `json:"web_port,omitempty"`
	WebURL            string `json:"web_url,omitempty"`
	LastOpened        int64  `json:"last_opened,omitempty"`
	CreatedAt         int64  `json:"created_at,omitempty"`
	UpdatedAt         int64  `json:"updated_at,omitempty"`
}

// ProjectWebInstance is a running project child instance
// (GET /projects/web).
type ProjectWebInstance struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	WebPort     int    `json:"web_port,omitempty"`
	WebURL      string `json:"web_url,omitempty"`
	State       string `json:"state"`
	StartedAt   string `json:"started_at,omitempty"`
	Delegations int    `json:"delegations,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ProjectOpenResult is the answer of POST /projects/{id}/web/open.
type ProjectOpenResult struct {
	Status    string `json:"status"` // opened | already_open
	ProjectID string `json:"project_id"`
	WebURL    string `json:"web_url,omitempty"`
	WebPort   int    `json:"web_port,omitempty"`
}

// ProjectEvent is a lifecycle event of /projects/events.
type ProjectEvent struct {
	// Type is switched, status_changed, delegation_changed, init_required,
	// web_started, web_stopped or web_error.
	Type        string
	ProjectID   string `json:"project_id"`
	Status      string `json:"status"`
	Error       string `json:"error"`
	Delegations int    `json:"delegations"`
	WebPort     int    `json:"web_port"`
}

// ProjectErrorBody is the JSON error body of the project endpoints
// (project_needs_init, web_instance_limit, child_startup_failed, ...).
type ProjectErrorBody struct {
	Error  string `json:"error"`
	Detail string `json:"detail"`
	Path   string `json:"path"`
	Limit  int    `json:"limit"`
}

// ParseProjectError extracts the JSON error body of an APIError.
func ParseProjectError(err error) ProjectErrorBody {
	var ae *APIError
	var out ProjectErrorBody
	if errors.As(err, &ae) {
		_ = json.Unmarshal([]byte(ae.Body), &out)
		if out.Error == "" {
			out.Error = ae.Code
		}
	}
	if out.Detail == "" && out.Error == "" && err != nil {
		out.Detail = err.Error()
	}
	return out
}

// DirListing is the answer of GET /fs/browse.
type DirListing struct {
	Path   string   `json:"path"`
	Parent string   `json:"parent"`
	Dirs   []string `json:"dirs"`
}

// ListProjects returns the registered projects.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var out struct {
		Projects []Project `json:"projects"`
	}
	err := c.Get(ctx, "/api/v1/projects", &out)
	return out.Projects, err
}

// ActiveProject returns the delegation target, or nil.
func (c *Client) ActiveProject(ctx context.Context) (*Project, error) {
	var out struct {
		Project *Project `json:"project"`
	}
	err := c.Get(ctx, "/api/v1/projects/active", &out)
	return out.Project, err
}

// AddProject registers a project directory.
func (c *Client) AddProject(ctx context.Context, path, name string) error {
	return c.Post(ctx, "/api/v1/projects", map[string]string{"path": path, "name": name}, nil)
}

// RenameProject renames a project.
func (c *Client) RenameProject(ctx context.Context, id, name string) error {
	return c.Patch(ctx, "/api/v1/projects/"+pathEscape(id), map[string]string{"name": name}, nil)
}

// DeleteProject unregisters a project.
func (c *Client) DeleteProject(ctx context.Context, id string) error {
	return c.Delete(ctx, "/api/v1/projects/"+pathEscape(id), nil)
}

// InitProject creates the Pando files of a project.
func (c *Client) InitProject(ctx context.Context, id string) error {
	return c.Post(ctx, "/api/v1/projects/"+pathEscape(id)+"/init", map[string]any{}, nil)
}

// DeactivateProject clears the delegation target.
func (c *Client) DeactivateProject(ctx context.Context, id string) error {
	return c.Post(ctx, "/api/v1/projects/"+pathEscape(id)+"/deactivate", map[string]any{}, nil)
}

// StopProject stops the project's instances and reports how many delegated
// loops it cancelled.
func (c *Client) StopProject(ctx context.Context, id string) (cancelled int, err error) {
	var out struct {
		Cancelled int `json:"cancelled_delegations"`
	}
	err = c.Post(ctx, "/api/v1/projects/"+pathEscape(id)+"/stop", map[string]any{}, &out)
	return out.Cancelled, err
}

// OpenProjectWeb starts (or reuses) the project's child instance. Errors
// carry a JSON body: see ParseProjectError.
func (c *Client) OpenProjectWeb(ctx context.Context, id string) (ProjectOpenResult, error) {
	var out ProjectOpenResult
	err := c.Post(ctx, "/api/v1/projects/"+pathEscape(id)+"/web/open", map[string]any{}, &out)
	return out, err
}

// CloseProjectWeb stops only the child instance.
func (c *Client) CloseProjectWeb(ctx context.Context, id string) error {
	return c.Post(ctx, "/api/v1/projects/"+pathEscape(id)+"/web/close", map[string]any{}, nil)
}

// ListProjectWebInstances returns the running child instances.
func (c *Client) ListProjectWebInstances(ctx context.Context) ([]ProjectWebInstance, error) {
	var out struct {
		Instances []ProjectWebInstance `json:"instances"`
	}
	err := c.Get(ctx, "/api/v1/projects/web", &out)
	return out.Instances, err
}

// OpenProjectDesktop asks the server to open the project in a new desktop
// window.
func (c *Client) OpenProjectDesktop(ctx context.Context, id string) error {
	return c.Post(ctx, "/api/v1/projects/"+pathEscape(id)+"/open-desktop", map[string]any{}, nil)
}

// SubscribeProjectEvents follows /projects/events until ctx ends.
func (c *Client) SubscribeProjectEvents(ctx context.Context, fn func(ProjectEvent)) error {
	return c.Subscribe(ctx, "/api/v1/projects/events", func(f SSEFrame) {
		if f.Event == "connected" || f.Event == "" {
			return
		}
		ev := ProjectEvent{Type: f.Event}
		_ = json.Unmarshal([]byte(f.Data), &ev)
		fn(ev)
	})
}

// BrowseDirectories lists the sub-directories of path ("~" is the home).
func (c *Client) BrowseDirectories(ctx context.Context, path string) (DirListing, error) {
	var out DirListing
	err := c.Get(ctx, "/api/v1/fs/browse?path="+url.QueryEscape(path), &out)
	return out, err
}
