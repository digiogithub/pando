package mcpgateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digiogithub/pando/internal/llm/tools"
)

// CatalogTool implements tools.BaseTool as "mcp_query_catalog".
// It searches the MCP tool registry by description/task keyword.
type CatalogTool struct {
	gateway *Gateway
}

// NewCatalogTool returns a BaseTool that wraps the catalog search capability.
func NewCatalogTool(gw *Gateway) tools.BaseTool {
	return &CatalogTool{gateway: gw}
}

func (t *CatalogTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        "mcp_query_catalog",
		Description: "Search available MCP tools by description/task. Returns matching tools with their schemas.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Keyword or description of the task you want to accomplish",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default 10)",
			},
		},
		Required: []string{"query"},
	}
}

func (t *CatalogTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	var input struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal([]byte(params.Input), &input); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("invalid parameters: %s", err)), nil
	}
	if input.MaxResults <= 0 {
		input.MaxResults = 10
	}

	results, err := t.gateway.SearchTools(ctx, input.Query, input.MaxResults)
	if err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("search failed: %s", err)), nil
	}

	if len(results) == 0 {
		return tools.NewTextResponse("No tools found matching the query."), nil
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return tools.NewTextErrorResponse("failed to serialize results"), nil
	}
	return tools.NewTextResponse(string(out)), nil
}

// CallToolProxy implements tools.BaseTool as "mcp_call_tool".
// It executes any MCP tool by name, using the gateway to route the call.
type CallToolProxy struct {
	gateway *Gateway
}

// NewCallToolProxy returns a BaseTool that proxies arbitrary MCP tool calls.
func NewCallToolProxy(gw *Gateway) tools.BaseTool {
	return &CallToolProxy{gateway: gw}
}

func (t *CallToolProxy) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        "mcp_call_tool",
		Description: "Execute any MCP tool by name. Use mcp_query_catalog first to discover available tools.",
		Parameters: map[string]any{
			"tool_name": map[string]any{
				"type":        "string",
				"description": "Name of the tool to execute (tool_name or server_name/tool_name)",
			},
			"parameters": map[string]any{
				"type":        "object",
				"description": "Parameters to pass to the tool",
			},
			"server_name": map[string]any{
				"type":        "string",
				"description": "Optional server name to disambiguate when multiple servers have the same tool name",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Optional session ID for usage tracking",
			},
		},
		Required: []string{"tool_name"},
	}
}

func (t *CallToolProxy) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	var input struct {
		ToolName   string                 `json:"tool_name"`
		Parameters map[string]interface{} `json:"parameters"`
		ServerName string                 `json:"server_name"`
		SessionID  string                 `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(params.Input), &input); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("invalid parameters: %s", err)), nil
	}

	toolID := input.ToolName
	if input.ServerName != "" {
		toolID = fmt.Sprintf("%s/%s", input.ServerName, input.ToolName)
	}

	if input.Parameters == nil {
		input.Parameters = map[string]interface{}{}
	}

	result, err := t.gateway.CallTool(ctx, toolID, input.Parameters, input.SessionID)
	if err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("tool call failed: %s", err)), nil
	}

	switch v := result.(type) {
	case string:
		return tools.NewTextResponse(v), nil
	default:
		out, _ := json.Marshal(v)
		return tools.NewTextResponse(string(out)), nil
	}
}
