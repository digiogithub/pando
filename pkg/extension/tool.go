package extension

import "context"

// The tool contract below is declared in terms of the standard library only,
// deliberately: an out-of-tree module cannot import
// github.com/digiogithub/pando/internal/llm/tools, so the types a tool exchanges
// with the agent have to live here. Core adapts these to its internal tool
// types at the wiring point.
//
// NOTE: ToolProvider is not consumed by the agent yet — that wiring lands with
// P1 of the extension plan. An extension may implement it today, but its tools
// will not reach the model until then. Manager.Statuses reports it so the gap
// is visible rather than silent.

// ToolInfo describes a tool to the model.
type ToolInfo struct {
	// Name is the tool name the model calls. Must be unique across all tools in
	// a build; namespace it with a vendor prefix to avoid clashing with core.
	Name string
	// Description tells the model what the tool does and when to use it.
	Description string
	// Parameters is a JSON Schema "properties" object describing the input.
	Parameters map[string]any
	// Required lists the required parameter names.
	Required []string
}

// ToolCall is one invocation of a tool by the model.
type ToolCall struct {
	// ID is the provider-assigned call identifier.
	ID string
	// Name is the tool being called.
	Name string
	// Input is the raw JSON argument object.
	Input string
}

// ToolResponse is the result handed back to the model.
type ToolResponse struct {
	// Content is the textual result.
	Content string
	// Metadata is an optional JSON object carried alongside the content, shown
	// in the UI but not necessarily sent to the model.
	Metadata string
	// IsError marks the call as failed. Prefer returning a ToolResponse with
	// IsError set over returning a Go error: the former is reported to the
	// model, the latter aborts the call.
	IsError bool
}

// NewTextResponse builds a successful textual response.
func NewTextResponse(content string) ToolResponse {
	return ToolResponse{Content: content}
}

// NewErrorResponse builds a failed response the model can read and react to.
func NewErrorResponse(content string) ToolResponse {
	return ToolResponse{Content: content, IsError: true}
}

// Tool is a single tool contributed by an extension.
type Tool interface {
	Info() ToolInfo
	Run(ctx context.Context, call ToolCall) (ToolResponse, error)
}

// ToolProvider is implemented by extensions that add tools to the agent.
type ToolProvider interface {
	Extension
	// Tools returns the tools to register. It is called once per agent build,
	// so it may return different tools as configuration changes.
	Tools() []Tool
}
