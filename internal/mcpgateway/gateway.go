package mcpgateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"

	"github.com/mark3labs/mcp-go/mcp"
)

// Gateway is the main MCP gateway orchestrator, combining the tool registry
// and usage statistics subsystems.
type Gateway struct {
	registry *Registry
	stats    *Stats
	pool     *MCPClientPool
	config   FavoriteConfig
	db       *sql.DB
}

// NewGateway creates a Gateway with the given database connection and favorites config.
func NewGateway(db *sql.DB, cfg FavoriteConfig) *Gateway {
	return &Gateway{
		registry: NewRegistry(db),
		stats:    NewStats(db, cfg),
		pool:     NewClientPool(),
		config:   cfg,
		db:       db,
	}
}

// Close releases all pooled MCP client connections.
func (g *Gateway) Close() {
	g.pool.StopAll()
}

// Initialize discovers all MCP server tools and populates the registry.
func (g *Gateway) Initialize(ctx context.Context, mcpServers map[string]config.MCPServer) error {
	if len(mcpServers) == 0 {
		logging.Info("MCP Gateway: no servers configured, skipping discovery")
		return nil
	}
	return g.registry.DiscoverAll(ctx, mcpServers)
}

// GetFavorites returns the current favorite tools based on usage statistics.
func (g *Gateway) GetFavorites(ctx context.Context) ([]RegisteredTool, error) {
	return g.stats.GetFavorites(ctx)
}

// SearchTools performs a keyword search over the registry.
func (g *Gateway) SearchTools(ctx context.Context, query string, maxResults int) ([]RegisteredTool, error) {
	return g.registry.SearchTools(ctx, query, maxResults)
}

// GetAllTools returns all tools in the registry.
func (g *Gateway) GetAllTools(ctx context.Context) ([]RegisteredTool, error) {
	return g.registry.GetAllTools(ctx)
}

// CallTool executes a registered MCP tool by its ID ("server/tool" or tool_name).
// It records usage statistics after the call.
func (g *Gateway) CallTool(ctx context.Context, toolID string, params map[string]interface{}, sessionID string) (interface{}, error) {
	tool, err := g.registry.GetTool(ctx, toolID)
	if err != nil {
		return nil, fmt.Errorf("get tool: %w", err)
	}
	if tool == nil {
		return nil, fmt.Errorf("tool not found: %s", toolID)
	}

	cfg := config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("configuration unavailable")
	}
	srv, ok := cfg.MCPServers[tool.ServerName]
	if !ok {
		return nil, fmt.Errorf("server not found in config: %s", tool.ServerName)
	}

	start := time.Now()
	result, callErr := g.callMCPTool(ctx, tool.ServerName, srv, tool.ToolName, params)
	durationMs := time.Since(start).Milliseconds()

	success := callErr == nil
	g.RecordUsage(ctx, tool.ID, sessionID, durationMs, success)

	if callErr != nil {
		return nil, callErr
	}
	return result, nil
}

// RecordUsage records a tool usage event asynchronously (errors are logged, not returned).
func (g *Gateway) RecordUsage(ctx context.Context, toolID, sessionID string, durationMs int64, success bool) {
	if err := g.stats.RecordUsage(ctx, toolID, sessionID, durationMs, success); err != nil {
		logging.Error("MCP gateway: failed to record usage", "toolID", toolID, "error", err)
	}
}

// callMCPTool uses the client pool to obtain an initialized MCP client for the
// given server and invokes the named tool. On error the pool entry is evicted
// so the next call will reconnect transparently.
func (g *Gateway) callMCPTool(ctx context.Context, serverName string, srv config.MCPServer, toolName string, params map[string]interface{}) (string, error) {
	c, err := g.pool.GetOrCreate(ctx, serverName, srv)
	if err != nil {
		return "", fmt.Errorf("get client: %w", err)
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("marshal params: %w", err)
	}

	var args map[string]any
	if err := json.Unmarshal(paramsJSON, &args); err != nil {
		return "", fmt.Errorf("unmarshal params: %w", err)
	}

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = toolName
	callReq.Params.Arguments = args

	result, err := c.CallTool(ctx, callReq)
	if err != nil {
		// Evict on error so the next caller gets a fresh connection.
		g.pool.Evict(serverName)
		return "", fmt.Errorf("call tool: %w", err)
	}

	output := ""
	for _, v := range result.Content {
		if tc, ok := v.(mcp.TextContent); ok {
			output = tc.Text
		} else {
			output = fmt.Sprintf("%v", v)
		}
	}
	return output, nil
}
