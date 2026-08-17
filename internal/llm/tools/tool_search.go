package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolSearchProvider is the interface the tool_search tool uses to query the
// active tool registry. It is satisfied by *tooldiscovery.Registry — we use
// an interface here to avoid an import cycle.
type ToolSearchProvider interface {
	// Search returns up to limit results ranked by relevance to query.
	// Each result must be JSON-marshallable.
	SearchTools(query string, limit int) []ToolSearchEntry
	// MarkDiscovered marks a canonical tool name as visible for this session.
	MarkDiscovered(name string)
}

// ToolExecutor executes any registered tool by canonical name or alias. It is
// satisfied by *tooldiscovery.Registry and lets tool_search act as the single
// unified discovery + execution entry point for internal and MCP tools alike.
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, name string, params map[string]interface{}) (ToolResponse, error)
}

// ToolSearchEntry is the shape returned by a ToolSearchProvider.
type ToolSearchEntry struct {
	CanonicalName string   `json:"name"`
	Aliases       []string `json:"aliases,omitempty"`
	ServerName    string   `json:"server,omitempty"`
	Source        string   `json:"source,omitempty"`
	Description   string   `json:"description"`
}

type toolSearchTool struct {
	provider     ToolSearchProvider
	executor     ToolExecutor
	defaultLimit int
}

// NewToolSearchTool creates the built-in tool_search tool backed by provider.
// provider should be set to the active *tooldiscovery.Registry after it is
// wrapped with a thin adapter (see tooldiscovery.RegistrySearchAdapter).
// defaultLimit is the number of results returned when the caller does not
// specify one; values <= 0 fall back to 8. The returned tool only supports
// search; use NewToolSearchToolWithExecutor to also enable execution.
func NewToolSearchTool(provider ToolSearchProvider, defaultLimit int) BaseTool {
	return NewToolSearchToolWithExecutor(provider, nil, defaultLimit)
}

// NewToolSearchToolWithExecutor creates the unified tool_search tool that can
// both discover tools (query mode) and execute them (tool_name mode). When
// executor is nil, execution requests return an error response.
func NewToolSearchToolWithExecutor(provider ToolSearchProvider, executor ToolExecutor, defaultLimit int) BaseTool {
	if defaultLimit <= 0 {
		defaultLimit = 8
	}
	return &toolSearchTool{provider: provider, executor: executor, defaultLimit: defaultLimit}
}

func (t *toolSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name: "tool_search",
		Description: "Unified tool discovery and execution. Search for available tools (internal " +
			"and MCP) by natural-language query, or execute a previously discovered tool by its " +
			"canonical name. Use this when you need a capability but are unsure of the exact tool " +
			"name. Returns a ranked list of matching tools with their names and descriptions. " +
			"Pass tool_name (plus parameters) to execute a tool returned by a previous search.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language description of the capability you are looking for.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default 8, max 20).",
			},
			"source": map[string]any{
				"type":        "string",
				"description": "Optional source filter: core, mcp, internal, lua, mesnada, rag, gateway.",
			},
			"tool_name": map[string]any{
				"type":        "string",
				"description": "Canonical name (or alias) of a tool to execute. When set, the tool is executed instead of searching.",
			},
			"parameters": map[string]any{
				"type":        "object",
				"description": "Parameters passed to the tool being executed.",
			},
		},
		Required: []string{},
	}
}

func (t *toolSearchTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var input struct {
		Query      string                 `json:"query"`
		Limit      int                    `json:"limit"`
		Source     string                 `json:"source"`
		ToolName   string                 `json:"tool_name"`
		Parameters map[string]interface{} `json:"parameters"`
	}
	if err := json.Unmarshal([]byte(params.Input), &input); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %s", err)), nil
	}

	// Execution mode: tool_name selects a registered tool to run.
	if input.ToolName != "" {
		if t.executor == nil {
			return NewTextErrorResponse("tool execution is not available in this context"), nil
		}
		if input.Parameters == nil {
			input.Parameters = map[string]interface{}{}
		}
		resp, err := t.executor.ExecuteTool(ctx, input.ToolName, input.Parameters)
		if err != nil {
			return NewTextErrorResponse(fmt.Sprintf("execute %s: %s", input.ToolName, err)), nil
		}
		return resp, nil
	}

	if input.Query == "" {
		return NewTextErrorResponse("query is required (or set tool_name to execute a tool)"), nil
	}
	limit := input.Limit
	if limit <= 0 {
		limit = t.defaultLimit
	}
	if limit > 20 {
		limit = 20
	}

	results := t.provider.SearchTools(input.Query, limit)

	// Filter by source if requested.
	if input.Source != "" {
		filtered := results[:0]
		for _, r := range results {
			if r.Source == input.Source {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if len(results) == 0 {
		return NewTextResponse("No tools found matching the query. Try broader terms."), nil
	}

	// Mark results as discovered so the selection policy can expose them.
	for _, r := range results {
		t.provider.MarkDiscovered(r.CanonicalName)
	}

	return NewStructuredResponse(results), nil
}
