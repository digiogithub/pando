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
)

// ValidEngine checks if an engine is valid.
func ValidEngine(e Engine) bool {
	return e == EngineCopilot || e == EngineClaude || e == EngineGemini || e == EngineOpenCode || e == EngineOllamaClaude || e == EngineOllamaOpenCode || e == EngineMistral || e == EngineACP || e == EngineACPClaudeCode || e == EngineACPCodex || e == EngineACPCustom || e == EngineACPServer || e == EnginePando || e == ""
}

// DefaultEngine returns the default engine.
// Pando is the default engine since it is the host application and provides
// native ACP support without requiring external process configuration.
func DefaultEngine() Engine {
	return EnginePando
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
	Error        string        `json:"error,omitempty"`
	ExitCode     *int          `json:"exit_code,omitempty"`
	Model        string        `json:"model,omitempty"`
	LogFile      string        `json:"log_file,omitempty"`
	Progress     *TaskProgress `json:"progress,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	StartedAt    *time.Time    `json:"started_at,omitempty"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
	Dependencies []string      `json:"dependencies,omitempty"`
	Tags         []string      `json:"tags,omitempty"`
	Priority     int           `json:"priority,omitempty"`
	Timeout      Duration      `json:"timeout,omitempty"`
	MCPConfig    string        `json:"mcp_config,omitempty"`
	ExtraArgs    []string      `json:"extra_args,omitempty"`
	Persona      string     `json:"persona,omitempty"`
	// ACP-specific fields
	ACPSessionID string      `json:"acp_session_id,omitempty"`
	ACPMode      string      `json:"acp_mode,omitempty"`
	ACPStatus    *ACPStatus  `json:"acp_status,omitempty"`
	CurrentTool  string      `json:"current_tool,omitempty"`
	ToolCalls    []*ToolCall `json:"tool_calls,omitempty"`
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
	Prompt                string   `json:"prompt"`
	WorkDir               string   `json:"work_dir,omitempty"`
	Model                 string   `json:"model,omitempty"`
	Engine                Engine   `json:"engine,omitempty"`
	Dependencies          []string `json:"dependencies,omitempty"`
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
