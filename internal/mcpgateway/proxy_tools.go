package mcpgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/notify"
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
		Description: "Search or list available MCP tools. Provide a 'query' to filter by description/task keyword, or omit it to list the entire catalog. Results are paginated: combine 'offset' with 'max_results' to page through the full list. The response reports 'total', 'has_more' and 'next_offset' so you can fetch every page.",
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Optional keyword or description of the task. Omit or leave empty to list all available tools.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results per page (default 10)",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Number of results to skip for pagination (default 0). Pass the 'next_offset' from a previous response to fetch the next page.",
			},
		},
		Required: []string{},
	}
}

// catalogResponse is the paginated payload returned by mcp_query_catalog.
type catalogResponse struct {
	Tools      []RegisteredTool `json:"tools"`
	Total      int              `json:"total"`
	Offset     int              `json:"offset"`
	Limit      int              `json:"limit"`
	Returned   int              `json:"returned"`
	HasMore    bool             `json:"has_more"`
	NextOffset int              `json:"next_offset,omitempty"`
}

func (t *CatalogTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	var input struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
		Offset     int    `json:"offset"`
	}
	if err := tools.DecodeToolInput(params.Input, &input); err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}
	return runCatalogQuery(ctx, t.gateway, input.Query, input.MaxResults, input.Offset), nil
}

// runCatalogQuery executes a catalog search/listing and renders the paginated
// response. It is shared by mcp_query_catalog and by mcp_call_tool, which some
// models use to route the catalog query indirectly.
func runCatalogQuery(ctx context.Context, gw *Gateway, query string, maxResults, offset int) tools.ToolResponse {
	if maxResults <= 0 {
		maxResults = 10
	}
	if offset < 0 {
		offset = 0
	}

	results, total, err := gw.ListCatalog(ctx, query, offset, maxResults)
	if err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("search failed: %s", err))
	}

	if total == 0 {
		if query == "" {
			return tools.NewTextResponse("No MCP tools are currently registered in the catalog.")
		}
		return tools.NewTextResponse("No tools found matching the query.")
	}

	resp := catalogResponse{
		Tools:    results,
		Total:    total,
		Offset:   offset,
		Limit:    maxResults,
		Returned: len(results),
		HasMore:  offset+len(results) < total,
	}
	if resp.HasMore {
		resp.NextOffset = offset + len(results)
	}

	return tools.NewStructuredResponse(resp)
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
	if err := tools.DecodeToolInput(params.Input, &input); err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}

	// Some models route every capability through mcp_call_tool, including the
	// gateway's own meta-tools (which live outside the MCP registry). Handle
	// those here instead of returning a confusing "tool not found".
	switch input.ToolName {
	case "mcp_query_catalog":
		return runCatalogQuery(ctx,
			t.gateway,
			paramString(input.Parameters, "query"),
			paramInt(input.Parameters, "max_results"),
			paramInt(input.Parameters, "offset"),
		), nil
	case "mcp_call_tool":
		return tools.NewTextErrorResponse(
			"mcp_call_tool cannot invoke itself; set tool_name to the target MCP tool instead",
		), nil
	}

	// Built-in agent tools live outside the MCP registry and cannot be executed
	// through the gateway. Redirect the model to call them directly instead of
	// failing with an opaque "tool not found".
	if tools.IsBuiltinTool(input.ToolName) {
		return tools.NewTextErrorResponse(fmt.Sprintf(
			"%q is a built-in agent tool, not an MCP catalog tool. Call %q directly instead of routing it through mcp_call_tool.",
			input.ToolName, input.ToolName,
		)), nil
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
		msg := fmt.Sprintf("tool call failed: %s", err)
		if strings.Contains(err.Error(), "tool not found") {
			msg += ". Use mcp_query_catalog (no query lists the whole catalog) to discover available tool names."
		}
		notify.Error(notify.SourceTool, msg)
		return tools.NewTextErrorResponse(msg), nil
	}

	switch v := result.(type) {
	case string:
		return tools.NewTextResponse(tools.FormatJSONLikeContent(v)), nil
	default:
		return tools.NewStructuredResponse(v), nil
	}
}

// paramString reads a string parameter from a loosely-typed parameters map.
func paramString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// paramInt reads an integer parameter from a loosely-typed parameters map,
// coercing the numeric shapes JSON/TOML decoders may produce.
func paramInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}
