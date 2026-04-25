package acp

import (
	"context"

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
)

// AgentEvent represents an event from the agent service
type AgentEvent struct {
	Type       AgentEventType
	Message    message.Message
	Error      error
	Delta      string
	ToolCall   *message.ToolCall
	ToolResult *message.ToolResult
}

// ACPModelInfo holds minimal model metadata for ACP responses.
// Defined here to avoid importing internal/llm/models from this package.
type ACPModelInfo struct {
	ID   string
	Name string
}

// ACPToolInfo holds minimal tool metadata for ACP available_commands_update.
// Defined here to avoid importing internal/llm/tools from this package.
type ACPToolInfo struct {
	Name        string
	Description string
}

// AgentService defines the interface for interacting with Pando's LLM agent.
// This is intentionally minimal to avoid import cycles.
type AgentService interface {
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	Cancel(sessionID string)
	// CurrentModelID returns the ID of the currently active model.
	CurrentModelID() string
	// AvailableModels returns the list of available models with name metadata.
	AvailableModels() []ACPModelInfo
	// SetModelOverride temporarily changes the active model (in-memory only).
	// Pass empty string to clear any previous override.
	SetModelOverride(modelID string) error
	// ListPersonas returns the names of all available personas.
	ListPersonas() []string
	// GetActivePersona returns the currently active persona name (empty = none).
	GetActivePersona() string
	// SetActivePersona sets the active persona by name. Pass empty string to clear.
	SetActivePersona(name string) error
	// ListAvailableTools returns the name and description of all tools available to the agent.
	ListAvailableTools() []ACPToolInfo
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
	ListSessions(ctx context.Context) ([]ACPSessionInfo, error)
	// GetMessages returns the messages for a session in chronological order.
	// Used by LoadSession to replay conversation history to connecting clients.
	GetMessages(ctx context.Context, sessionID string) ([]message.Message, error)
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
