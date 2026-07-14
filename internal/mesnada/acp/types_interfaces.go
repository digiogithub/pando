package acp

import (
	"context"

	acpsdk "github.com/madeindigio/acp-go-sdk"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/message"
)

// AgentEventType represents the type of agent event
type AgentEventType string

const (
	AgentEventTypeError         AgentEventType = "error"
	AgentEventTypeResponse      AgentEventType = "response"
	AgentEventTypeSummarize     AgentEventType = "summarize"
	AgentEventTypeContentDelta  AgentEventType = "content_delta"
	AgentEventTypeThinkingDelta AgentEventType = "thinking_delta"
	AgentEventTypeToolCall      AgentEventType = "tool_call"
	AgentEventTypeToolResult    AgentEventType = "tool_result"
	// AgentEventTypeSystemMessage carries internal status messages (context compaction,
	// retries, etc.) that must be shown to the user but are not part of the LLM response.
	AgentEventTypeSystemMessage AgentEventType = "system_message"
)

// AgentEvent represents an event from the agent service
type AgentEvent struct {
	Type       AgentEventType
	Message    message.Message
	Error      error
	Delta      string
	ToolCall   *message.ToolCall
	ToolResult *message.ToolResult
	// Progress is populated for AgentEventTypeSummarize events.
	Progress string
	// SystemMessage is populated for AgentEventTypeSystemMessage events.
	// It carries a human-readable status string (compaction, retry, etc.).
	SystemMessage string
}

// ACPModelInfo holds minimal model metadata for ACP responses.
// Defined here to avoid importing internal/llm/models from this package.
type ACPModelInfo struct {
	ID   string
	Name string
}

type SessionLLMOverrides struct {
	// Model selects a per-session model (empty = agent default). Applying it via
	// per-session overrides (instead of a global SetModelOverride) is what lets a
	// single ACP server run several concurrent sessions with different models.
	Model           string
	ReasoningEffort string
	ThinkingMode    string
	// Persona / PersonaScoped carry the session's persona authoritatively so
	// concurrent sessions never clobber a shared global persona. PersonaScoped
	// must be set for the Persona (or auto-selection) to take effect.
	Persona       string
	PersonaScoped bool
}

// AgentService defines the interface for interacting with Pando's LLM agent.
// This is intentionally minimal to avoid import cycles.
type AgentService interface {
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	RunGoal(ctx context.Context, sessionID string, objective string) (<-chan AgentEvent, error)
	Cancel(sessionID string)
	// Steer queues a mid-run feedback message for an active session. It is injected
	// into the agent loop at the next safe boundary without cancelling the run.
	// Returns ErrSessionNotBusy if there is no active run for the session.
	Steer(sessionID string, content string, attachments ...message.Attachment) error
	// PendingSteering reports how many steering messages are queued for the session.
	PendingSteering(sessionID string) int
	// IsSessionBusy reports whether a run is currently active for the session.
	IsSessionBusy(sessionID string) bool
	// LastRunSystemMessages returns internal status/progress messages emitted while
	// preparing or running the most recent prompt for the session (persona selection,
	// self-improvement prompt tuning, retries, compaction, etc.).
	LastRunSystemMessages(sessionID string) []string
	// CurrentModelID returns the ID of the currently active model.
	CurrentModelID() string
	// AvailableModels returns the list of available models with name metadata.
	AvailableModels() []ACPModelInfo
	// SetModelOverride temporarily changes the active model (in-memory only).
	// Pass empty string to clear any previous override.
	SetModelOverride(modelID string) error
	// SetSessionLLMOverrides applies request-scoped inference overrides for the session.
	SetSessionLLMOverrides(sessionID string, overrides SessionLLMOverrides)
	// SetPonytailMode sets the per-session ponytail ("lazy senior dev") intensity.
	// mode is one of "lite", "full", "ultra" (or "off"/empty to disable). It
	// returns the normalized mode label applied and whether the input was valid.
	SetPonytailMode(sessionID string, mode string) (applied string, ok bool)
	// SetCavemanMode sets the per-session caveman output-brevity level. mode is
	// one of "lite", "full", "ultra", "wenyan" (or "off"/empty to disable). It
	// returns the normalized level applied and whether the input was valid.
	SetCavemanMode(sessionID string, mode string) (applied string, ok bool)
	// SetSuperpowersMode enables or disables the per-session Superpowers workflow
	// policy (the opt-in plan-first, verify-always development lifecycle).
	SetSuperpowersMode(sessionID string, enabled bool)
	// SuperpowersMode reports whether the Superpowers policy is active for the session.
	SuperpowersMode(sessionID string) bool
	// SuperpowersFinish runs the Superpowers closing turn (verify, report, list what
	// is left) as a normal agent turn. The mode is disabled only if that turn
	// succeeds, so a cancelled or failed close keeps the workflow active.
	SuperpowersFinish(ctx context.Context, sessionID string) (<-chan AgentEvent, error)
	// ListPersonas returns the names of all available personas.
	ListPersonas() []string
	// GetActivePersona returns the currently active persona name (empty = none).
	GetActivePersona() string
	// SetActivePersona sets the active persona by name. Pass empty string to clear.
	SetActivePersona(name string) error
	// Summarize performs a manual conversation summary/compaction for the session.
	Summarize(ctx context.Context, sessionID string) error
	// OpenCopilotUsage opens the Copilot usage/features page when Copilot auth is available.
	OpenCopilotUsage() error
	// OpenClaudeUsage opens the Claude usage page when Claude OAuth auth is available.
	OpenClaudeUsage() error
}

// ACPSessionInfo is a minimal session descriptor used by the ACP layer.
// Using a local struct avoids importing the session package and breaking the
// session→llm/tools→acp import cycle.
type ACPSessionInfo struct {
	ID        string
	Title     string
	UpdatedAt int64
	// PromptTokens are the tokens consumed by the prompt (input + cache writes).
	PromptTokens int64
	// CompletionTokens are the tokens produced by the model (output + cache reads).
	CompletionTokens int64
	// ContextWindow is the total context window size for the current model (0 = unknown).
	ContextWindow int64
}

// SessionService defines the minimal interface needed by the ACP agent.
// Using a narrow interface avoids the session→llm/tools→acp import cycle.
type SessionService interface {
	CreateSession(ctx context.Context, title string) (string, error)
	GetSession(ctx context.Context, id string) (ACPSessionInfo, error)
	SaveSessionTitle(ctx context.Context, id string, title string) (ACPSessionInfo, error)
	ListSessions(ctx context.Context) ([]ACPSessionInfo, error)
	GetACPSessionState(ctx context.Context, sessionID string) (string, error)
	SaveACPSessionState(ctx context.Context, sessionID string, state string) error
	GetActiveGoal(ctx context.Context, sessionID string) (db.SessionGoal, error)
	GetGoalBySession(ctx context.Context, sessionID string) (db.SessionGoal, error)
	CancelGoal(ctx context.Context, sessionID string) (db.SessionGoal, error)
	// GetMessages returns the messages for a session in chronological order.
	// Used by LoadSession to replay conversation history to connecting clients.
	GetMessages(ctx context.Context, sessionID string) ([]message.Message, error)
}

// GoalStateUpdate is the ACP-friendly projection of a persisted goal state.
type GoalStateUpdate struct {
	Objective     string `json:"objective"`
	Status        string `json:"status"`
	Iteration     int    `json:"iteration"`
	MaxIterations int    `json:"max_iterations"`
	Progress      string `json:"progress,omitempty"`
	NextStep      string `json:"next_step,omitempty"`
	ElapsedTime   string `json:"elapsed_time,omitempty"`
}

// PermissionRequestData carries the full details of a tool permission request.
// This mirrors permission.CreatePermissionRequest but is defined here to avoid
// importing the permission package and breaking the tool→acp import graph.
type PermissionRequestData struct {
	SessionID   string
	ToolName    string
	Description string
	Action      string
	Path        string
	Params      any
}

// PermissionService is a minimal interface for configuring tool permissions per session.
// This avoids import cycles with the permission package.
type PermissionService interface {
	AutoApproveSession(sessionID string)
	RemoveAutoApproveSession(sessionID string)
	RegisterSessionHandler(sessionID string, handler func(req PermissionRequestData) bool)
	UnregisterSessionHandler(sessionID string)
}

// editToolInput is used to parse fields from tool call input JSON for edit/write tools.
type editToolInput struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`    // write tool
	OldString string `json:"old_string"` // edit tool
	NewString string `json:"new_string"` // edit tool
}

// TodoInfo represents a single todo/task in a plan.
// This matches the structure used by the todowrite tool in OpenCode.
type TodoInfo struct {
	// Content is the description of the todo/task
	Content string `json:"content"`

	// Status is the current status: "pending", "in_progress", "completed", "cancelled"
	Status string `json:"status"`

	// Priority is the priority level: "high", "medium", "low"
	Priority string `json:"priority"`
}

// DecodeTodosResult represents the result of decoding todo output.
type DecodeTodosResult struct {
	// Success indicates if the decoding was successful
	Success bool `json:"success"`

	// Todos contains the decoded todos if successful
	Todos []TodoInfo `json:"todos,omitempty"`

	// Error contains any error message if decoding failed
	Error string `json:"error,omitempty"`
}

// ToolHandlerFunc is a function type for handling tool completion events.
type ToolHandlerFunc func(ctx context.Context, toolName string, toolOutput string) error

// PlanService defines the interface for plan-related operations.
type PlanService interface {
	// DecodeTodos parses the output of a todowrite tool into TodoInfo structures
	DecodeTodos(output string) DecodeTodosResult

	// CreatePlanEntry converts TodoInfo to PlanEntry with proper state mapping
	CreatePlanEntry(todo TodoInfo) PlanEntry

	// CreateSessionPlan converts a list of TodoInfo to a SessionPlan
	CreateSessionPlan(todos []TodoInfo) *SessionPlan

	// ValidateSessionPlan validates a complete session plan
	ValidateSessionPlan(plan *SessionPlan) error

	// SendPlanUpdate sends a plan update to the ACP client
	SendPlanUpdate(ctx context.Context, conn *acpsdk.AgentSideConnection, sessionID acpsdk.SessionId, plan *SessionPlan) error
}

// PersonaInfo describes a single available persona for ACP responses.
type PersonaInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionPersonaState represents the persona selection state for a session,
// analogous to SessionModelState. It is carried via the _meta extension field
// in ACP responses since the ACP spec does not yet define a native persona type.
type SessionPersonaState struct {
	AvailablePersonas []PersonaInfo `json:"availablePersonas"`
	CurrentPersonaId  string        `json:"currentPersonaId"`
}
