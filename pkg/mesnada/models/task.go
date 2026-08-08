// Package models defines the core domain types for the mesnada orchestrator.
package models

import (
	"time"
)

// TaskStatus represents the current state of a task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusPaused    TaskStatus = "paused"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// Engine represents the CLI engine to use for spawning agents.
type Engine string

const (
	// EngineCopilot uses GitHub Copilot CLI (default).
	EngineCopilot Engine = "copilot"
	// EngineClaude uses Anthropic Claude CLI.
	EngineClaude Engine = "claude"
	// EngineGemini uses Google Gemini CLI.
	EngineGemini Engine = "gemini"
	// EngineOpenCode uses OpenCode.ai CLI.
	EngineOpenCode Engine = "opencode"
	// EngineOllamaClaude uses Ollama with Claude integration.
	EngineOllamaClaude Engine = "ollama-claude"
	// EngineOllamaOpenCode uses Ollama with OpenCode integration.
	EngineOllamaOpenCode Engine = "ollama-opencode"
	// EngineMistral uses Mistral Vibe CLI.
	EngineMistral Engine = "mistral"
	// EngineACP uses ACP (Agent Client Protocol) generic agent.
	EngineACP Engine = "acp"
	// EngineACPClaudeCode uses Claude Code via ACP.
	EngineACPClaudeCode Engine = "acp-claude"
	// EngineACPCodex uses OpenAI Codex via ACP.
	EngineACPCodex Engine = "acp-codex"
	// EngineACPCustom uses a custom ACP agent.
	EngineACPCustom Engine = "acp-custom"
	// EngineACPServer represents ACP sessions managed by the Pando ACP server.
	EngineACPServer Engine = "acp-server"
	// EnginePando uses Pando itself as an ACP subagent (shorthand for engine=acp + acp_agent=pando).
	EnginePando Engine = "pando"
	// EngineWarmACP marks a delegated task that ran inside an already-running
	// ("warm") per-project Pando ACP instance reused by the orchestrator instead
	// of cold-spawning a CLI subprocess. The conclusion is captured over the ACP
	// wire rather than scanned from stdout.
	EngineWarmACP Engine = "warm-acp"
)

// ValidEngine checks if an engine is valid.
func ValidEngine(e Engine) bool {
	return e == EngineCopilot || e == EngineClaude || e == EngineGemini || e == EngineOpenCode || e == EngineOllamaClaude || e == EngineOllamaOpenCode || e == EngineMistral || e == EngineACP || e == EngineACPClaudeCode || e == EngineACPCodex || e == EngineACPCustom || e == EngineACPServer || e == EnginePando || e == EngineWarmACP || e == ""
}

// DefaultEngine returns the default engine.
// Pando is the default engine since it is the host application and provides
// native ACP support without requiring external process configuration.
func DefaultEngine() Engine {
	return EnginePando
}

// IsWarmEligibleEngine reports whether a task with this engine may be routed
// through a warm per-project Pando ACP instance. Warm reuse is an optimization
// for Pando itself (empty / engine=pando, or a previous warm-acp breadcrumb).
// Explicit CLI engines (claude, copilot, gemini, …) and third-party ACP engines
// must stay on the cold subprocess path.
func IsWarmEligibleEngine(e Engine) bool {
	switch e {
	case "", EnginePando, EngineWarmACP:
		return true
	default:
		return false
	}
}

// TaskProgress represents the progress of a task.
type TaskProgress struct {
	Percentage  int       `json:"percentage"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ACPStatus represents the status of an ACP agent.
type ACPStatus struct {
	SessionID    string `json:"session_id,omitempty"`
	Mode         string `json:"mode,omitempty"`
	AgentName    string `json:"agent_name,omitempty"`
	IsConnected  bool   `json:"is_connected"`
	ToolCalls    int    `json:"tool_calls,omitempty"`
	LastActivity string `json:"last_activity,omitempty"`
}

// Task represents a CLI agent task.
type Task struct {
	ID           string        `json:"id"`
	Prompt       string        `json:"prompt"`
	WorkDir      string        `json:"work_dir"`
	Status       TaskStatus    `json:"status"`
	Engine       Engine        `json:"engine,omitempty"`
	PID          int           `json:"pid,omitempty"`
	Output       string        `json:"output,omitempty"`
	OutputTail   string        `json:"output_tail,omitempty"`
	RawError     string        `json:"raw_error,omitempty"`
	Error        string        `json:"error,omitempty"`
	ExitCode     *int          `json:"exit_code,omitempty"`
	Model        string        `json:"model,omitempty"`
	LogFile      string        `json:"log_file,omitempty"`
	Progress     *TaskProgress `json:"progress,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	StartedAt    *time.Time    `json:"started_at,omitempty"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
	Dependencies []string      `json:"dependencies,omitempty"`
	// GateDeps is a subset of Dependencies that must additionally PASS a
	// conclusion gate (not merely reach status=completed) before this task can
	// start. Used by the swarm verifier→synthesizer topology: the synthesizer
	// gates on the verifier's conclusion. Empty (the default) preserves the plain
	// "all dependencies completed" semantics exactly.
	GateDeps  []string `json:"gate_deps,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Priority  int      `json:"priority,omitempty"`
	Timeout   Duration `json:"timeout,omitempty"`
	MCPConfig string   `json:"mcp_config,omitempty"`
	ExtraArgs []string `json:"extra_args,omitempty"`
	Persona   string   `json:"persona,omitempty"`
	// ACP-specific fields
	ACPSessionID string      `json:"acp_session_id,omitempty"`
	ACPMode      string      `json:"acp_mode,omitempty"`
	ACPStatus    *ACPStatus  `json:"acp_status,omitempty"`
	CurrentTool  string      `json:"current_tool,omitempty"`
	ToolCalls    []*ToolCall `json:"tool_calls,omitempty"`
	// Delegation correlation fields. These link a delegated task back to the
	// parent agent session/task that spawned it and to the project it ran in.
	// They are populated by the spawn tool + orchestrator and are durable so a
	// task's origin can always be reconstructed.
	ParentSessionID string      `json:"parent_session_id,omitempty"` // session that spawned this task
	ParentTaskID    string      `json:"parent_task_id,omitempty"`    // for depth/trace
	CorrelationID   string      `json:"correlation_id,omitempty"`    // idempotency key
	ProjectID       string      `json:"project_id,omitempty"`        // resolved from work_dir
	ProjectPath     string      `json:"project_path,omitempty"`      // CanonicalProjectPath
	ProjectName     string      `json:"project_name,omitempty"`      // resolved folder/display name
	Conclusion      *Conclusion `json:"conclusion,omitempty"`        // captured task conclusion
	Depth           int         `json:"depth,omitempty"`             // anti-fork-bomb cap
	// Circuit-breaker / respawn-guard state. ConsecutiveFailures counts failed
	// executions since the last success and accumulates across retry chains (a
	// Retry carries it into the new task) so an unfixable task trips the breaker
	// instead of being respawned forever. MaxRetries overrides the configured
	// limit for this task (0 = use the configured default). LastFailureKind
	// classifies the last failure ("rate_limit", "auth", "other") so the respawn
	// guard can defer a quota-walled or auth-blocked task rather than thrash.
	ConsecutiveFailures int        `json:"consecutive_failures,omitempty"`
	MaxRetries          int        `json:"max_retries,omitempty"`
	LastFailureKind     string     `json:"last_failure_kind,omitempty"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	// Claim-lease dispatch state. ClaimLock holds the id of the dispatcher that
	// reserved this task for execution; a task is only ever started by the
	// dispatcher that won its claim, which makes concurrent readiness
	// evaluations idempotent. ClaimExpires bounds the reservation: a dispatcher
	// that dies between claiming and spawning cannot strand the task, because
	// the lease expires and the task returns to the dispatchable pool.
	ClaimLock    string     `json:"claim_lock,omitempty"`
	ClaimedAt    *time.Time `json:"claimed_at,omitempty"`
	ClaimExpires *time.Time `json:"claim_expires,omitempty"`
}

// ClaimActive reports whether the task currently holds a dispatch claim that has
// not expired at the given instant.
func (t *Task) ClaimActive(now time.Time) bool {
	if t == nil || t.ClaimLock == "" {
		return false
	}
	if t.ClaimExpires == nil {
		return true // a lease without an expiry never lapses on its own
	}
	return now.Before(*t.ClaimExpires)
}

// ClearClaim drops the dispatch claim, returning the task to the dispatchable
// pool.
func (t *Task) ClearClaim() {
	if t == nil {
		return
	}
	t.ClaimLock = ""
	t.ClaimedAt = nil
	t.ClaimExpires = nil
}

// Conclusion is the enriched, durable result of a delegated task. The subagent
// emits only the model-known fields via a sentinel block; the orchestrator fills
// in launch metadata it owns. Synthesized is true when no block was emitted and
// the conclusion was derived from output/error.
type Conclusion struct {
	Status      string    `json:"status"` // success|partial|failed|blocked
	Summary     string    `json:"summary"`
	Artifacts   []string  `json:"artifacts,omitempty"`
	MemoryRefs  []string  `json:"memory_refs,omitempty"`
	FollowUp    string    `json:"follow_up,omitempty"`
	Confidence  float64   `json:"confidence,omitempty"`
	Synthesized bool      `json:"synthesized,omitempty"` // true if no block emitted
	CapturedAt  time.Time `json:"captured_at"`
	// Warnings accumulates human-readable soft-validation messages produced
	// during parsing (unknown status, out-of-range confidence, missing summary,
	// blank entries, malformed body, etc.). The conclusion is never discarded
	// due to warnings — data is always preserved.
	Warnings []string `json:"warnings,omitempty"`
}

// ToolCall represents a single tool execution within a task.
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Title     string                 `json:"title"`
	Kind      string                 `json:"kind"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Result    string                 `json:"result,omitempty"`
	Status    string                 `json:"status"`
	Locations []string               `json:"locations,omitempty"`
	Diffs     map[string]string      `json:"diffs,omitempty"`
	StartedAt time.Time              `json:"started_at"`
	EndedAt   *time.Time             `json:"ended_at,omitempty"`
}

// Duration is a wrapper around time.Duration for JSON marshaling.
type Duration time.Duration

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Duration(d).String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	if len(b) < 2 {
		return nil
	}
	// Remove quotes
	s := string(b[1 : len(b)-1])
	if s == "" {
		*d = 0
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

// IsTerminal returns true if the task is in a terminal state.
func (t *Task) IsTerminal() bool {
	return t.Status == TaskStatusCompleted ||
		t.Status == TaskStatusFailed ||
		t.Status == TaskStatusCancelled ||
		t.Status == TaskStatusPaused
}

// IsRunning returns true if the task is currently running.
func (t *Task) IsRunning() bool {
	return t.Status == TaskStatusRunning
}

// IsPending returns true if the task is pending execution.
func (t *Task) IsPending() bool {
	return t.Status == TaskStatusPending
}

// GetProgress returns the current progress percentage and description.
func (t *Task) GetProgress() (int, string) {
	if t.Progress == nil {
		return 0, ""
	}
	return t.Progress.Percentage, t.Progress.Description
}

// TaskSummary provides a condensed view of a task for listing.
type TaskSummary struct {
	ID          string     `json:"id"`
	Prompt      string     `json:"prompt"`
	WorkDir     string     `json:"work_dir"`
	Status      TaskStatus `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Duration    string     `json:"duration,omitempty"`
}

// ToSummary converts a Task to a TaskSummary.
func (t *Task) ToSummary() TaskSummary {
	summary := TaskSummary{
		ID:          t.ID,
		Prompt:      truncateString(t.Prompt, 100),
		WorkDir:     t.WorkDir,
		Status:      t.Status,
		CreatedAt:   t.CreatedAt,
		CompletedAt: t.CompletedAt,
	}
	if t.CompletedAt != nil && t.StartedAt != nil {
		summary.Duration = t.CompletedAt.Sub(*t.StartedAt).String()
	}
	return summary
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ACPMCPServer represents an MCP server configuration for ACP.
type ACPMCPServer struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// SpawnRequest represents a request to spawn a new agent.
type SpawnRequest struct {
	Prompt       string   `json:"prompt"`
	WorkDir      string   `json:"work_dir,omitempty"`
	Model        string   `json:"model,omitempty"`
	Engine       Engine   `json:"engine,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	// GateDeps (subset of Dependencies) must pass a conclusion gate, not just
	// complete, before the task starts. See Task.GateDeps.
	GateDeps              []string `json:"gate_deps,omitempty"`
	Tags                  []string `json:"tags,omitempty"`
	Priority              int      `json:"priority,omitempty"`
	Timeout               string   `json:"timeout,omitempty"`
	MCPConfig             string   `json:"mcp_config,omitempty"`
	ExtraArgs             []string `json:"extra_args,omitempty"`
	Persona               string   `json:"persona,omitempty"`
	Background            bool     `json:"background"`
	IncludeDependencyLogs bool     `json:"include_dependency_logs,omitempty"`
	DependencyLogLines    int      `json:"dependency_log_lines,omitempty"`
	LogFile               string   `json:"log_file,omitempty"`
	// ACP-specific fields
	ACPMode          string                 `json:"acp_mode,omitempty"`
	ACPAgent         string                 `json:"acp_agent,omitempty"`
	ACPConfigOptions map[string]interface{} `json:"acp_config_options,omitempty"`
	ACPMCPServers    []ACPMCPServer         `json:"acp_mcp_servers,omitempty"`
	// Delegation correlation fields. Flow from the spawn tool into the created
	// Task so its origin (parent session/task, project, depth) is persisted.
	ParentSessionID string `json:"parent_session_id,omitempty"`
	ParentTaskID    string `json:"parent_task_id,omitempty"`
	CorrelationID   string `json:"correlation_id,omitempty"`
	ProjectID       string `json:"project_id,omitempty"`
	ProjectPath     string `json:"project_path,omitempty"`
	Depth           int    `json:"depth,omitempty"`
	// MaxRetries overrides the configured circuit-breaker limit for this task
	// (0 = use the configured default). See Task.MaxRetries.
	MaxRetries int `json:"max_retries,omitempty"`
	// ConsecutiveFailures seeds the new task's breaker counter so a retry chain
	// accumulates failures instead of resetting the breaker on every retry.
	// Populated by Retry; leave zero for a fresh task.
	ConsecutiveFailures int `json:"consecutive_failures,omitempty"`
}

// WaitRequest represents a request to wait for task completion.
type WaitRequest struct {
	TaskID  string `json:"task_id"`
	Timeout string `json:"timeout,omitempty"`
}

// WaitMultipleRequest represents a request to wait for multiple tasks.
type WaitMultipleRequest struct {
	TaskIDs []string `json:"task_ids"`
	WaitAll bool     `json:"wait_all"`
	Timeout string   `json:"timeout,omitempty"`
}

// ListRequest represents a request to list tasks.
type ListRequest struct {
	Status []TaskStatus `json:"status,omitempty"`
	Tags   []string     `json:"tags,omitempty"`
	Limit  int          `json:"limit,omitempty"`
	Offset int          `json:"offset,omitempty"`
}
