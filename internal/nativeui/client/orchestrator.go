package client

import (
	"context"
	"net/url"
)

// OrchestratorToolCall is a tool call of a Mesnada task.
type OrchestratorToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Title     string         `json:"title"`
	Kind      string         `json:"kind"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Result    string         `json:"result,omitempty"`
	Status    string         `json:"status"`
	Locations []string       `json:"locations,omitempty"`
	StartedAt string         `json:"started_at"`
	EndedAt   string         `json:"ended_at,omitempty"`
}

// OrchestratorTask mirrors api.TaskResponse (the fields the UI shows).
type OrchestratorTask struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Prompt      string                 `json:"prompt,omitempty"`
	Agent       string                 `json:"agent"`
	Model       string                 `json:"model"`
	Persona     string                 `json:"persona,omitempty"`
	Status      string                 `json:"status"`
	Progress    int                    `json:"progress"`
	Tokens      int                    `json:"tokens"`
	CreatedAt   string                 `json:"created_at"`
	Output      string                 `json:"output,omitempty"`
	CurrentTool string                 `json:"current_tool,omitempty"`
	Error       string                 `json:"error,omitempty"`
	ToolCalls   []OrchestratorToolCall `json:"tool_calls,omitempty"`
}

// DelegationMetrics are the warm-routing counters.
type DelegationMetrics struct {
	WarmAttempts              int     `json:"warm_attempts"`
	WarmHits                  int     `json:"warm_hits"`
	ExternalHits              int     `json:"external_hits"`
	WarmFailures              int     `json:"warm_failures"`
	ColdFallbacks             int     `json:"cold_fallbacks"`
	CapRejections             int     `json:"cap_rejections"`
	Resurrections             int     `json:"resurrections"`
	LiveInjections            int     `json:"live_injections"`
	ExternalReattachRecovered int     `json:"external_reattach_recovered"`
	ExternalReattachFailed    int     `json:"external_reattach_failed"`
	WarmHitRate               float64 `json:"warm_hit_rate"`
}

// ListOrchestratorTasks lists Mesnada tasks.
func (c *Client) ListOrchestratorTasks(ctx context.Context) ([]OrchestratorTask, error) {
	var out struct {
		Tasks []OrchestratorTask `json:"tasks"`
	}
	err := c.Get(ctx, "/api/v1/orchestrator/tasks", &out)
	return out.Tasks, err
}

// GetOrchestratorTask returns one task with tool calls and output.
func (c *Client) GetOrchestratorTask(ctx context.Context, id string) (OrchestratorTask, error) {
	var out OrchestratorTask
	err := c.Get(ctx, "/api/v1/orchestrator/tasks/"+url.PathEscape(id), &out)
	return out, err
}

// CreateOrchestratorTask spawns a background task. The server requires a
// prompt; model and engine are optional.
func (c *Client) CreateOrchestratorTask(ctx context.Context, prompt, model, engine string) (OrchestratorTask, error) {
	body := map[string]any{"prompt": prompt, "background": true}
	if model != "" {
		body["model"] = model
	}
	if engine != "" {
		body["engine"] = engine
	}
	var out OrchestratorTask
	err := c.Post(ctx, "/api/v1/orchestrator/tasks", body, &out)
	return out, err
}

// CancelOrchestratorTask cancels a running task.
func (c *Client) CancelOrchestratorTask(ctx context.Context, id string) error {
	return c.Post(ctx, "/api/v1/orchestrator/tasks/"+url.PathEscape(id)+"/cancel", map[string]any{}, nil)
}

// DeleteOrchestratorTask deletes a finished task.
func (c *Client) DeleteOrchestratorTask(ctx context.Context, id string) error {
	return c.Delete(ctx, "/api/v1/orchestrator/tasks/"+url.PathEscape(id), nil)
}

// DelegationMetrics returns the delegation counters.
func (c *Client) DelegationMetrics(ctx context.Context) (DelegationMetrics, error) {
	var out DelegationMetrics
	err := c.Get(ctx, "/api/v1/orchestrator/delegation/metrics", &out)
	return out, err
}

// CronJob is a scheduled prompt.
type CronJob struct {
	Name     string   `json:"name"`
	Schedule string   `json:"schedule"`
	Enabled  bool     `json:"enabled"`
	Prompt   string   `json:"prompt,omitempty"`
	Engine   string   `json:"engine,omitempty"`
	Model    string   `json:"model,omitempty"`
	WorkDir  string   `json:"workDir,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Timeout  string   `json:"timeout,omitempty"`
	NextRun  string   `json:"nextRun,omitempty"`
}

// CronJobCreate is the body of POST /api/v1/cronjobs.
type CronJobCreate struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Prompt   string `json:"prompt"`
	Enabled  bool   `json:"enabled"`
	Engine   string `json:"engine,omitempty"`
	Model    string `json:"model,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
}

// ListCronJobs lists cron jobs and whether the cron service is enabled.
func (c *Client) ListCronJobs(ctx context.Context) ([]CronJob, bool, error) {
	var out struct {
		Jobs    []CronJob `json:"jobs"`
		Enabled bool      `json:"enabled"`
	}
	err := c.Get(ctx, "/api/v1/cronjobs", &out)
	return out.Jobs, out.Enabled, err
}

// CreateCronJob creates a cron job.
func (c *Client) CreateCronJob(ctx context.Context, j CronJobCreate) error {
	return c.Post(ctx, "/api/v1/cronjobs", j, nil)
}

// SetCronJobEnabled enables or disables a job.
func (c *Client) SetCronJobEnabled(ctx context.Context, name string, enabled bool) error {
	return c.Put(ctx, "/api/v1/cronjobs/"+url.PathEscape(name), map[string]any{"enabled": enabled}, nil)
}

// RunCronJob triggers a job now and returns the spawned task id.
func (c *Client) RunCronJob(ctx context.Context, name string) (string, error) {
	var out struct {
		TaskID string `json:"taskId"`
	}
	err := c.Post(ctx, "/api/v1/cronjobs/"+url.PathEscape(name)+"/run", map[string]any{}, &out)
	return out.TaskID, err
}

// DeleteCronJob deletes a job.
func (c *Client) DeleteCronJob(ctx context.Context, name string) error {
	return c.Delete(ctx, "/api/v1/cronjobs/"+url.PathEscape(name), nil)
}
