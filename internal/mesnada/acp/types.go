// Package acp provides types and utilities for working with the Agent Client Protocol.
package acp

import (
	"context"
	"os/exec"

	acpsdk "github.com/coder/acp-go-sdk"
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
