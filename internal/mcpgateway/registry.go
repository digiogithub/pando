package mcpgateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// Registry manages the MCP tool catalog in SQLite.
type Registry struct {
	db *sql.DB
}

// NewRegistry creates a new Registry backed by the given database connection.
func NewRegistry(db *sql.DB) *Registry {
	return &Registry{db: db}
}

// DiscoverAll iterates the configured MCP servers, calls ListTools on each,
// and upserts every discovered tool into mcp_tool_registry.
func (r *Registry) DiscoverAll(ctx context.Context, mcpServers map[string]config.MCPServer) error {
	for name, srv := range mcpServers {
		logging.Debug("MCP gateway: discovering tools", "server", name)
		tools, err := listServerTools(ctx, name, srv)
		if err != nil {
			logging.Error("MCP gateway: failed to list tools", "server", name, "error", err)
			continue
		}
		for _, t := range tools {
			schema := map[string]interface{}{}
			if t.InputSchema.Properties != nil {
				schema = t.InputSchema.Properties
			}
			if err := r.UpsertTool(ctx, name, t.Name, t.Description, schema); err != nil {
				logging.Error("MCP gateway: failed to upsert tool", "server", name, "tool", t.Name, "error", err)
			}
		}
		logging.Debug("MCP gateway: discovered tools", "server", name, "count", len(tools))
	}
	return nil
}

// listServerTools creates an MCP client for the given server, initializes it,
// lists its tools, and returns them.
func listServerTools(ctx context.Context, name string, srv config.MCPServer) ([]mcp.Tool, error) {
	var c interface {
		Initialize(ctx context.Context, req mcp.InitializeRequest) (*mcp.InitializeResult, error)
		ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
		Close() error
	}

	var err error
	switch srv.Type {
	case config.MCPStdio:
		c, err = client.NewStdioMCPClient(srv.Command, srv.Env, srv.Args...)
	case config.MCPSse:
		c, err = client.NewSSEMCPClient(srv.URL, client.WithHeaders(srv.Headers))
	case config.MCPStreamableHTTP:
		c, err = client.NewStreamableHttpClient(srv.URL, transport.WithHTTPHeaders(srv.Headers))
	default:
		return nil, fmt.Errorf("unknown MCP type: %s", srv.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	defer c.Close()

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "pando-gateway",
		Version: version.Version,
	}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	result, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	return result.Tools, nil
}

// UpsertTool inserts or updates a tool record in mcp_tool_registry.
func (r *Registry) UpsertTool(ctx context.Context, serverName, toolName, description string, inputSchema map[string]interface{}) error {
	schemaJSON, err := json.Marshal(inputSchema)
	if err != nil {
		schemaJSON = []byte("{}")
	}
	id := fmt.Sprintf("%s/%s", serverName, toolName)
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO mcp_tool_registry (id, server_name, tool_name, description, input_schema, last_discovered)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			description = excluded.description,
			input_schema = excluded.input_schema,
			last_discovered = excluded.last_discovered
	`, id, serverName, toolName, description, string(schemaJSON), time.Now().UTC())
	return err
}

// GetTool retrieves a tool by its composite ID ("server/tool") or by tool_name.
func (r *Registry) GetTool(ctx context.Context, toolID string) (*RegisteredTool, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, server_name, tool_name, description, input_schema, last_discovered
		FROM mcp_tool_registry
		WHERE id = ? OR tool_name = ?
		LIMIT 1
	`, toolID, toolID)
	return scanTool(row)
}

// SearchTools performs a keyword search over tool_name and description.
func (r *Registry) SearchTools(ctx context.Context, query string, maxResults int) ([]RegisteredTool, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	pattern := "%" + query + "%"
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, server_name, tool_name, description, input_schema, last_discovered
		FROM mcp_tool_registry
		WHERE tool_name LIKE ? OR description LIKE ?
		LIMIT ?
	`, pattern, pattern, maxResults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTools(rows)
}

// GetAllTools returns all registered tools.
func (r *Registry) GetAllTools(ctx context.Context) ([]RegisteredTool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, server_name, tool_name, description, input_schema, last_discovered
		FROM mcp_tool_registry
		ORDER BY server_name, tool_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTools(rows)
}

// GetToolsByIDs returns tools matching the given IDs.
func (r *Registry) GetToolsByIDs(ctx context.Context, ids []string) ([]RegisteredTool, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var result []RegisteredTool
	for _, id := range ids {
		t, err := r.GetTool(ctx, id)
		if err != nil || t == nil {
			continue
		}
		result = append(result, *t)
	}
	return result, nil
}

// scanTool reads a single tool row.
func scanTool(row *sql.Row) (*RegisteredTool, error) {
	var t RegisteredTool
	var schemaStr string
	var discovered time.Time
	if err := row.Scan(&t.ID, &t.ServerName, &t.ToolName, &t.Description, &schemaStr, &discovered); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	t.LastDiscovered = discovered
	_ = json.Unmarshal([]byte(schemaStr), &t.InputSchema)
	return &t, nil
}

// scanTools reads multiple tool rows.
func scanTools(rows *sql.Rows) ([]RegisteredTool, error) {
	var result []RegisteredTool
	for rows.Next() {
		var t RegisteredTool
		var schemaStr string
		var discovered time.Time
		if err := rows.Scan(&t.ID, &t.ServerName, &t.ToolName, &t.Description, &schemaStr, &discovered); err != nil {
			return nil, err
		}
		t.LastDiscovered = discovered
		_ = json.Unmarshal([]byte(schemaStr), &t.InputSchema)
		result = append(result, t)
	}
	return result, rows.Err()
}
