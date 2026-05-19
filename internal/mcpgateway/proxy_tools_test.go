package mcpgateway

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/stretchr/testify/require"
)

func createProxyToolsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	schema := `
CREATE TABLE IF NOT EXISTS mcp_tool_registry (
    id TEXT PRIMARY KEY,
    server_name TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    description TEXT,
    input_schema TEXT,
    last_discovered TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(server_name, tool_name)
);
`
	require.NoError(t, func() error {
		_, err := db.Exec(schema)
		return err
	}())
	return db
}

func insertProxyTool(t *testing.T, db *sql.DB, id, serverName, toolName string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO mcp_tool_registry (id, server_name, tool_name, description, input_schema, last_discovered)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, serverName, toolName, "test tool", "{}", time.Now().UTC(),
	)
	require.NoError(t, err)
}

func TestCatalogToolRunRepairsMalformedJSONInput(t *testing.T) {
	db := createProxyToolsTestDB(t)
	insertProxyTool(t, db, "demo/search_docs", "demo", "search_docs")

	tool := &CatalogTool{gateway: &Gateway{registry: NewRegistry(db)}}

	resp, err := tool.Run(context.Background(), llmtools.ToolCall{Input: "```json\n{query:'search', max_results: 1,}\n```"})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "search_docs")
}

func TestCallToolProxyRunRepairsMalformedJSONInput(t *testing.T) {
	db := createProxyToolsTestDB(t)
	insertProxyTool(t, db, "demo/demo_tool", "demo", "demo_tool")

	tool := &CallToolProxy{gateway: &Gateway{registry: NewRegistry(db)}}

	resp, err := tool.Run(context.Background(), llmtools.ToolCall{Input: "{tool_name:'demo_tool', parameters:{foo:'bar',},}"})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "tool call failed:")
	require.NotContains(t, strings.ToLower(resp.Content), "invalid parameters")
}
