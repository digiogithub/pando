package extension

import "context"

// The tool contract below is declared in terms of the standard library only,
// deliberately: an out-of-tree module cannot import
// github.com/digiogithub/pando/internal/llm/tools, so the types a tool exchanges
// with the agent have to live here. Core adapts these to its internal tool
// types at the wiring point.
//
// Tools contributed here reach the model: the agent collects ToolProvider and
// the middleware interfaces below on every tool-set build (see
// internal/extensions/tools.go and internal/llm/agent/extension_tools.go).

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

// ToolMiddleware is the base interface for extensions that observe or alter the
// agent's tool set. It carries only the ordering rule; the actual work is
// declared by ToolFilter, ToolInterceptor, or both.
//
// Middleware sees *every* tool, core tools included, not just the tools
// contributed by extensions. That is the point: an enterprise policy module
// exists to constrain what the model can reach.
type ToolMiddleware interface {
	Extension
	// Priority orders middleware. Lower runs closer to the tool: filters with a
	// lower priority run first, and interceptors with a lower priority sit
	// innermost, so a high-priority interceptor observes the calls a
	// low-priority one made. Ties break on extension ID, so ordering is stable
	// across builds.
	Priority() int
}

// ToolFilter rewrites the tool list before it is offered to the model. It may
// drop, reorder or wrap tools, and it must return a slice — returning nil
// removes every tool, which is a valid (if drastic) policy.
//
// Filters run once per tool-set build, not per call.
type ToolFilter interface {
	ToolMiddleware
	FilterTools(tools []Tool) []Tool
}

// ToolInterceptor wraps the execution of every tool call, core tools included.
// The implementation must call next exactly once unless it is deliberately
// refusing the call, in which case it returns a ToolResponse with IsError set
// and never calls next.
//
// Interceptors are the audit/redaction/quota hook. Returning a Go error aborts
// the call; prefer an error ToolResponse so the model can react.
type ToolInterceptor interface {
	ToolMiddleware
	InterceptTool(ctx context.Context, call ToolCall, next ToolFunc) (ToolResponse, error)
}

// ToolFunc is the next link in an interceptor chain.
type ToolFunc func(ctx context.Context, call ToolCall) (ToolResponse, error)
