package mcpgateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mcpclient"

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
//
// fingerprints maps a server name to ConfigFingerprint of its RAW
// configuration. A server whose catalog rows exist and whose stored
// fingerprint matches is not contacted at all (PANDO-US-0068): opening a
// connection has a cost beyond latency for servers such as Xcode's mcpbridge,
// which prompts the user on every new client. A nil map disables the cache.
func (r *Registry) DiscoverAll(ctx context.Context, mcpServers map[string]config.MCPServer, fingerprints map[string]string) error {
	for name, srv := range mcpServers {
		fp := fingerprints[name]
		if r.hasCachedCatalog(ctx, name, fp) {
			logging.Debug("MCP gateway: using cached catalog", "server", name)
			continue
		}
		logging.Debug("MCP gateway: discovering tools", "server", name)
		tools, err := listServerTools(ctx, name, srv)
		if err != nil {
			logging.Error("MCP gateway: failed to list tools", "server", name, "error", err)
			continue
		}
		stored := true
		for _, t := range tools {
			schema := map[string]interface{}{}
			if t.InputSchema.Properties != nil {
				schema = t.InputSchema.Properties
			}
			if err := r.UpsertTool(ctx, name, t.Name, t.Description, schema); err != nil {
				stored = false
				logging.Error("MCP gateway: failed to upsert tool", "server", name, "tool", t.Name, "error", err)
			}
		}
		if stored {
			if err := r.setServerFingerprint(ctx, name, fp); err != nil {
				logging.Debug("MCP gateway: failed to store config fingerprint", "server", name, "error", err)
			}
		}
		logging.Debug("MCP gateway: discovered tools", "server", name, "count", len(tools))
	}
	return nil
}

// DiscoverServer refreshes the catalog of a single server: it connects, lists
// the tools and replaces every row previously stored for that server. Unlike
// DiscoverAll it reports the connection error to the caller so that an
// interactive refresh (WebUI "reload", saving a server) can explain why a
// server ends up with zero tools instead of silently logging it.
//
// srv must already be resolved via config.ResolveMCPServerSecrets.
//
// fingerprint is ConfigFingerprint of the RAW configuration; it is stored so
// the next startup can reuse this catalog ("" leaves no fingerprint, which
// forces the next startup to rediscover).
func (r *Registry) DiscoverServer(ctx context.Context, name string, srv config.MCPServer, fingerprint string) (int, error) {
	tools, err := listServerTools(ctx, name, srv)
	if err != nil {
		return 0, err
	}
	// Drop the previous snapshot first so tools removed upstream disappear.
	if err := r.DeleteServer(ctx, name); err != nil {
		return 0, fmt.Errorf("clear previous tools: %w", err)
	}
	for _, t := range tools {
		schema := map[string]interface{}{}
		if t.InputSchema.Properties != nil {
			schema = t.InputSchema.Properties
		}
		if err := r.UpsertTool(ctx, name, t.Name, t.Description, schema); err != nil {
			return 0, fmt.Errorf("store tool %s: %w", t.Name, err)
		}
	}
	if err := r.setServerFingerprint(ctx, name, fingerprint); err != nil {
		logging.Debug("MCP registry: failed to store config fingerprint", "server", name, "error", err)
	}
	logging.Debug("MCP registry: refreshed server", "server", name, "count", len(tools))
	return len(tools), nil
}

// newDiscoveryClient builds the client used to list a server's tools. Tests
// replace it to count connections without spawning processes.
var newDiscoveryClient = func(ctx context.Context, name string, srv config.MCPServer) (mcpclient.Client, error) {
	return mcpclient.New(ctx, name, srv)
}

// listServerTools creates an MCP client for the given server, initializes it,
// lists its tools, and returns them.
func listServerTools(ctx context.Context, name string, srv config.MCPServer) ([]mcp.Tool, error) {
	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()

	c, err := newDiscoveryClient(clientCtx, name, srv)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	defer c.Close()

	timeout := mcpclient.ResolveTimeout(srv.Timeout, mcpclient.DefaultDiscoveryTimeout)
	// Handshake also reports the outcome on the extension mcp topic.
	if _, err := mcpclient.Handshake(ctx, c, name, srv, "pando-gateway"); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	listCtx, listCancel := mcpclient.WithTimeout(ctx, timeout)
	result, err := c.ListTools(listCtx, mcp.ListToolsRequest{})
	listCancel()
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

// ListCatalog returns a page of tools optionally filtered by a keyword query,
// together with the total number of tools matching the filter (ignoring
// pagination). An empty query lists the entire catalog. Results are ordered by
// server_name, tool_name so pagination via offset/limit is stable.
func (r *Registry) ListCatalog(ctx context.Context, query string, offset, limit int) ([]RegisteredTool, int, error) {
	return r.listCatalogExcluding(ctx, query, offset, limit, nil)
}

// listCatalogExcluding is ListCatalog without the rows of the servers listed
// in exclude (both in the page and in the total).
func (r *Registry) listCatalogExcluding(ctx context.Context, query string, offset, limit int, exclude []string) ([]RegisteredTool, int, error) {
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	var (
		conditions []string
		filterArgs []interface{}
	)
	if query != "" {
		pattern := "%" + query + "%"
		conditions = append(conditions, "(tool_name LIKE ? OR description LIKE ?)")
		filterArgs = append(filterArgs, pattern, pattern)
	}
	if len(exclude) > 0 {
		conditions = append(conditions, "server_name NOT IN (?"+strings.Repeat(", ?", len(exclude)-1)+")")
		for _, name := range exclude {
			filterArgs = append(filterArgs, name)
		}
	}
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM mcp_tool_registry "+whereClause, filterArgs...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	listSQL := `
		SELECT id, server_name, tool_name, description, input_schema, last_discovered
		FROM mcp_tool_registry ` + whereClause + `
		ORDER BY server_name, tool_name
		LIMIT ? OFFSET ?`
	args := append(append([]interface{}{}, filterArgs...), limit, offset)
	rows, err := r.db.QueryContext(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	page, err := scanTools(rows)
	if err != nil {
		return nil, 0, err
	}
	return page, total, nil
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

// DeleteServer removes all registry rows for a server; usage rows are cascade-deleted by FK.
// The server's config fingerprint goes too, so a later startup rediscovers it.
func (r *Registry) DeleteServer(ctx context.Context, serverName string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM mcp_tool_registry
		WHERE server_name = ?
	`, serverName)
	if err != nil {
		return err
	}
	if ferr := r.deleteServerFingerprint(ctx, serverName); ferr != nil {
		// Older schemas (and some tests) lack the table; the catalog rows are
		// gone, which already forces rediscovery.
		logging.Debug("MCP registry: failed to delete config fingerprint", "server", serverName, "error", ferr)
	}
	return nil
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
