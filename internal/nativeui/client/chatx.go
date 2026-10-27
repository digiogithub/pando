package client

import (
	"context"
	"encoding/json"
	"net/url"
)

// This file holds the endpoints the full chat uses besides the run itself:
// slash commands, @ file search, personas, auto-approve, goal, questions,
// subagent tasks and the agent-vcs data behind the diff viewer. They mirror
// the calls of web-ui/packages/pando-client (stores/sessionStore.ts and the
// chat components).

// SlashCommand is an entry of GET /api/v1/commands.
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	AcceptsArgs bool   `json:"acceptsArgs"`
}

// Commands lists the slash commands (built-in, custom and extension ones).
func (c *Client) Commands(ctx context.Context) ([]SlashCommand, error) {
	var out []SlashCommand
	err := c.Get(ctx, "/api/v1/commands", &out)
	return out, err
}

// FileHit is a result of the @ file search.
type FileHit struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// SearchFiles looks for files of the workspace whose name contains query
// (GET /api/v1/files/search).
func (c *Client) SearchFiles(ctx context.Context, query string) ([]FileHit, error) {
	var out struct {
		Results []FileHit `json:"results"`
	}
	err := c.Get(ctx, "/api/v1/files/search?q="+url.QueryEscape(query), &out)
	return out.Results, err
}

// Workspace is GET /api/v1/project: the working directory of the instance.
type Workspace struct {
	CWD     string `json:"cwd"`
	Version string `json:"version"`
}

// Workspace returns the instance's working directory.
func (c *Client) Workspace(ctx context.Context) (Workspace, error) {
	var out Workspace
	err := c.Get(ctx, "/api/v1/project", &out)
	return out, err
}

// ActivePersona is GET /api/v1/personas/active.
type ActivePersona struct {
	// Active is the manually selected persona, "" for Auto.
	Active string `json:"active"`
	// Auto: no manual selection and auto-select is enabled.
	Auto          bool   `json:"auto"`
	DecisionModel bool   `json:"decisionModel"`
	Applied       string `json:"applied"`
	Source        string `json:"source"`
}

// Personas lists the persona names.
func (c *Client) Personas(ctx context.Context) ([]string, error) {
	var out struct {
		Personas []string `json:"personas"`
	}
	err := c.Get(ctx, "/api/v1/personas", &out)
	return out.Personas, err
}

// GetActivePersona returns the selected persona and, with a session id, the
// one auto-selection applied to it.
func (c *Client) GetActivePersona(ctx context.Context, sessionID string) (ActivePersona, error) {
	path := "/api/v1/personas/active"
	if sessionID != "" {
		path += "?sessionId=" + url.QueryEscape(sessionID)
	}
	var out ActivePersona
	err := c.Get(ctx, path, &out)
	return out, err
}

// SetActivePersona selects a persona; "" returns to Auto.
func (c *Client) SetActivePersona(ctx context.Context, name string) error {
	return c.Put(ctx, "/api/v1/personas/active", map[string]string{"name": name}, nil)
}

// AutoApprove reports a session's auto-approve ("auto mode") state.
func (c *Client) AutoApprove(ctx context.Context, sessionID string) (bool, error) {
	var out struct {
		Enabled bool `json:"enabled"`
	}
	err := c.Get(ctx, "/api/v1/sessions/"+pathEscape(sessionID)+"/auto-approve", &out)
	return out.Enabled, err
}

// SetAutoApprove turns a session's auto-approve on or off.
func (c *Client) SetAutoApprove(ctx context.Context, sessionID string, enabled bool) error {
	return c.Post(ctx, "/api/v1/sessions/"+pathEscape(sessionID)+"/auto-approve", map[string]bool{"enabled": enabled}, nil)
}

// GoalResponse is GET /sessions/{id}/goal.
type GoalResponse struct {
	Goal *GoalStatus `json:"goal"`
}

// Goal returns the goal of a session (nil when there is none).
func (c *Client) Goal(ctx context.Context, sessionID string) (*GoalStatus, error) {
	var out GoalResponse
	err := c.Get(ctx, "/api/v1/sessions/"+pathEscape(sessionID)+"/goal", &out)
	return out.Goal, err
}

// CancelGoal cancels the running goal of a session.
func (c *Client) CancelGoal(ctx context.Context, sessionID string) (*GoalStatus, error) {
	var out GoalResponse
	err := c.Post(ctx, "/api/v1/sessions/"+pathEscape(sessionID)+"/goal/cancel", map[string]any{}, &out)
	return out.Goal, err
}

// QuestionAnswer is the answer to one question of a QuestionRequest.
type QuestionAnswer struct {
	QuestionID string   `json:"questionId"`
	Selected   []string `json:"selected"`
	OtherText  string   `json:"otherText"`
}

// RespondQuestion answers an AskUserQuestion prompt.
func (c *Client) RespondQuestion(ctx context.Context, sessionID, id string, answers []QuestionAnswer) error {
	return c.Post(ctx, "/api/v1/questions/respond", map[string]any{"id": id, "sessionId": sessionID, "answers": answers}, nil)
}

// CancelQuestion dismisses an AskUserQuestion prompt (the agent continues
// without an answer).
func (c *Client) CancelQuestion(ctx context.Context, sessionID, id string) error {
	return c.Post(ctx, "/api/v1/questions/respond", map[string]any{"id": id, "sessionId": sessionID, "cancelled": true}, nil)
}

// Task is a delegated (subagent) task of GET /api/v1/orchestrator/tasks.
type Task struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Agent       string `json:"agent"`
	Model       string `json:"model"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"`
	CurrentTool string `json:"current_tool"`
}

// Tasks lists the delegated tasks.
func (c *Client) Tasks(ctx context.Context) ([]Task, error) {
	var out struct {
		Tasks []Task `json:"tasks"`
	}
	err := c.Get(ctx, "/api/v1/orchestrator/tasks", &out)
	return out.Tasks, err
}

// AgentVCSDiff lists the files a session changed (empty when agent-vcs is
// disabled).
func (c *Client) AgentVCSDiff(ctx context.Context, sessionID string) ([]VCSDiffEntry, error) {
	var out struct {
		Diff []VCSDiffEntry `json:"diff"`
	}
	err := c.Get(ctx, "/api/v1/agentvcs/sessions/"+pathEscape(sessionID)+"/diff", &out)
	return out.Diff, err
}

// FileOriginal returns the content a file had when the session first touched
// it; found is false when the session recorded no history for it.
func (c *Client) FileOriginal(ctx context.Context, sessionID, path string) (original string, found bool, err error) {
	var out struct {
		Found    bool   `json:"found"`
		Original string `json:"original"`
	}
	err = c.Get(ctx, "/api/v1/sessions/"+pathEscape(sessionID)+"/file-original?path="+url.QueryEscape(path), &out)
	return out.Original, out.Found, err
}

// SetActiveModelResult is the answer of PUT /api/v1/models/active.
type SetActiveModelResult struct {
	Model string `json:"model"`
	// Scope is "session" when the selection applies to the session only.
	Scope string `json:"scope"`
}

// SelectModel selects a model (see SetActiveModel); with a sessionID the
// selection is scoped to that session.
func (c *Client) SelectModel(ctx context.Context, model, sessionID string) (SetActiveModelResult, error) {
	body := map[string]string{"model": model}
	if sessionID != "" {
		body["sessionId"] = sessionID
	}
	var out SetActiveModelResult
	err := c.Put(ctx, "/api/v1/models/active", body, &out)
	return out, err
}

// RoutingInfo is the structured model auto mode notice of a system_message
// (mirrors agent.RoutingInfo).
type RoutingInfo struct {
	RouteID        string   `json:"routeId"`
	Model          string   `json:"model"`
	Matched        bool     `json:"matched"`
	Probability    *float64 `json:"probability"`
	Confidence     *float64 `json:"confidence"`
	Reason         string   `json:"reason"`
	FallbackUsed   bool     `json:"fallbackUsed"`
	ErrorClass     string   `json:"errorClass"`
	RouterProvider string   `json:"routerProvider"`
	RouterModel    string   `json:"routerModel"`
	// Kind: routed | no_match | router_unavailable | route_unusable | failover.
	Kind   string `json:"kind"`
	Notice string `json:"notice"`
}

// FilterCounts are the kept/dropped counts of one context source.
type FilterCounts struct {
	Kept    int `json:"kept"`
	Dropped int `json:"dropped"`
}

// ContextFilterInfo is the structured context-filter notice of a
// system_message (mirrors agent.ContextFilterInfo).
type ContextFilterInfo struct {
	Kept           int                     `json:"kept"`
	Dropped        int                     `json:"dropped"`
	BySource       map[string]FilterCounts `json:"bySource"`
	Threshold      *float64                `json:"threshold"`
	LatencyMs      *float64                `json:"latencyMs"`
	RouterProvider string                  `json:"routerProvider"`
	RouterModel    string                  `json:"routerModel"`
	// Reason is empty on full success, otherwise "partial:<class>".
	Reason string `json:"reason"`
	Notice string `json:"notice"`
}

// RoutingInfo decodes the routing object of a system_message (nil if none).
func (e StreamEvent) RoutingInfo() *RoutingInfo {
	if len(e.Routing) == 0 {
		return nil
	}
	var r RoutingInfo
	if json.Unmarshal(e.Routing, &r) != nil {
		return nil
	}
	return &r
}

// ContextFilterInfo decodes the context_filter object of a system_message.
func (e StreamEvent) ContextFilterInfo() *ContextFilterInfo {
	if len(e.ContextFilter) == 0 {
		return nil
	}
	var r ContextFilterInfo
	if json.Unmarshal(e.ContextFilter, &r) != nil {
		return nil
	}
	return &r
}
