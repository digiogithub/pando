// Package acp provides types and utilities for working with the Agent Client Protocol.
package acp

import (
	"context"
	"os/exec"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// ACPAgentConfig represents the configuration of an ACP agent in config.yaml.
type ACPAgentConfig struct {
	// Name is the unique identifier for this agent configuration
	Name string `json:"name" yaml:"name"`

	// Title is a human-readable name for the agent
	Title string `json:"title" yaml:"title"`

	// Command is the binary to execute (e.g., "claude-code", "zed-acp")
	Command string `json:"command" yaml:"command"`

	// Args are the command-line arguments to pass to the agent
	Args []string `json:"args" yaml:"args"`

	// Env is a map of environment variables to set for the agent
	Env map[string]string `json:"env" yaml:"env"`

	// WorkDir is the default working directory for spawned tasks
	WorkDir string `json:"work_dir" yaml:"work_dir"`

	// Mode is the default operation mode: "code", "ask", or "architect"
	Mode string `json:"mode" yaml:"mode"`

	// McpServers is a list of MCP servers to pass to the agent
	McpServers []McpServerConfig `json:"mcp_servers" yaml:"mcp_servers"`
}

// McpServerConfig represents the configuration of an MCP server to pass to an ACP agent.
type McpServerConfig struct {
	// Name is the unique identifier for this MCP server
	Name string `json:"name" yaml:"name"`

	// Command is the binary to execute for stdio-based MCP servers
	Command string `json:"command" yaml:"command"`

	// Args are the command-line arguments for the MCP server
	Args []string `json:"args" yaml:"args"`

	// Env is a map of environment variables for the MCP server
	Env map[string]string `json:"env" yaml:"env"`

	// Type is the transport type: "stdio", "http", or "sse"
	Type string `json:"type,omitempty" yaml:"type,omitempty"`

	// URL is the endpoint URL for http or sse transports
	URL string `json:"url,omitempty" yaml:"url,omitempty"`

	// Headers are additional HTTP headers for http/sse transports
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// ACPSession represents an active session with an ACP agent.
type ACPSession struct {
	// TaskID is the mesnada task identifier
	TaskID string

	// SessionID is the ACP session identifier returned by the agent
	SessionID acpsdk.SessionId

	// Conn is the client-side connection to the ACP agent
	Conn *acpsdk.ClientSideConnection

	// Cmd is the os/exec.Cmd for the running agent process
	Cmd *exec.Cmd

	// Cancel is the context cancellation function for this session
	Cancel context.CancelFunc
}

// SessionUpdateInfo represents information about a session update from the ACP agent.
// This is used to communicate agent state changes to the mesnada orchestrator.
type SessionUpdateInfo struct {
	// TaskID is the mesnada task identifier
	TaskID string

	// MessageText is the text content from the agent (for text blocks)
	MessageText string

	// ThinkingText is the model's reasoning/thinking content (agent_thought_chunk).
	// This is distinct from MessageText and represents extended thinking output.
	ThinkingText string

	// ToolCall contains information about tool calls made by the agent
	ToolCall *ToolCallInfo

	// Plan contains planning information from the agent
	Plan string

	// StopReason indicates why the agent stopped (if applicable)
	StopReason string

	// Error contains any error message
	Error string
}

// ToolCallInfo represents information about a tool call from the ACP agent.
type ToolCallInfo struct {
	// ID is the unique identifier for this tool call
	ID string

	// Name is the name of the tool being called
	Name string

	// Kind is the ACP tool kind used by clients for rendering
	Kind string

	// Arguments are the arguments passed to the tool
	Arguments map[string]interface{}

	// Locations references files or paths associated with the tool call
	Locations []string

	// Status is the status of the tool call: "started", "progress", "completed", "failed"
	Status string

	// Result is the result of the tool call (if completed)
	Result string

	// Title is the rendered title shown by ACP clients
	Title string

	// Content contains rich content blocks for the tool call (e.g. diffs, text)
	Content []acpsdk.ToolCallContent

	// Diffs contains extracted file modification diffs for quick access
	Diffs map[string]string

	// RawInput is the original input to the tool
	RawInput interface{}

	// RawOutput is the original output from the tool
	RawOutput interface{}

	// Meta carries ACP-specific metadata associated with the tool update.
	Meta map[string]interface{}
}

// PlanEntry represents a single entry in a plan sent via ACP session update.
// This matches the OpenCode standard for plan communication.
type PlanEntry struct {
	// Priority is the priority level of the task: "high", "medium", "low"
	Priority string `json:"priority"`

	// Status is the current status of the task: "pending", "in_progress", "completed"
	Status string `json:"status"`

	// Content is the description or title of the task
	Content string `json:"content"`
}

// SessionPlan represents a complete plan sent to ACP clients.
type SessionPlan struct {
	// Entries is the list of plan entries
	Entries []PlanEntry `json:"entries"`
}

// SessionUpdateType represents the different types of session updates.
type SessionUpdateType string

const (
	// SessionUpdateUsage is for usage/cost updates
	SessionUpdateUsage SessionUpdateType = "usage_update"

	// SessionUpdateToolCall is for tool call status updates
	SessionUpdateToolCall SessionUpdateType = "tool_call_update"

	// SessionUpdatePlan is for plan information (new feature)
	SessionUpdatePlan SessionUpdateType = "plan"

	// SessionUpdateAgentMessage is for streaming agent messages
	SessionUpdateAgentMessage SessionUpdateType = "agent_message_chunk"

	// SessionUpdateUserMessage is for streaming user messages
	SessionUpdateUserMessage SessionUpdateType = "user_message_chunk"

	// SessionUpdateAgentThought is for streaming agent thoughts/reasoning
	SessionUpdateAgentThought SessionUpdateType = "agent_thought_chunk"
)

// SessionUpdate represents a structured session update message.
// This extends the current SessionUpdateInfo to support the OpenCode standard.
type SessionUpdate struct {
	// Type is the type of update
	Type SessionUpdateType `json:"type"`

	// SessionID is the ACP session ID
	SessionID string `json:"sessionId"`

	// MessageText is the text content (for message updates)
	MessageText string `json:"messageText,omitempty"`

	// ThinkingText is the model's reasoning content (for thought updates)
	ThinkingText string `json:"thinkingText,omitempty"`

	// ToolCall contains tool information (for tool updates)
	ToolCall *ToolCallInfo `json:"toolCall,omitempty"`

	// Plan contains planning information (for plan updates)
	Plan *SessionPlan `json:"plan,omitempty"`

	// Usage contains usage information (for usage updates)
	Usage *UsageInfo `json:"usage,omitempty"`

	// StopReason indicates why the agent stopped (if applicable)
	StopReason string `json:"stopReason,omitempty"`

	// Error contains any error message
	Error string `json:"error,omitempty"`
}

// UsageInfo represents token usage and cost information.
type UsageInfo struct {
	// InputTokens is the number of input tokens used
	InputTokens int64 `json:"inputTokens"`

	// OutputTokens is the number of output tokens used
	OutputTokens int64 `json:"outputTokens"`

	// TotalTokens is the total tokens used
	TotalTokens int64 `json:"totalTokens"`

	// Cost is the estimated cost in USD
	Cost float64 `json:"cost"`
}
