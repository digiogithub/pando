package agent

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/mcpgateway"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// discoveryStubTool is a minimal BaseTool for discovery wiring tests.
type discoveryStubTool struct {
	name string
}

func (d *discoveryStubTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: d.name, Description: "stub " + d.name}
}

func (d *discoveryStubTool) Run(_ context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	return tools.NewTextResponse("ok"), nil
}

func newDiscoveryTestGateway(t *testing.T) *mcpgateway.Gateway {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
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
CREATE TABLE IF NOT EXISTS mcp_tool_usage_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_id TEXT NOT NULL,
    session_id TEXT,
    called_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    duration_ms INTEGER,
    success BOOLEAN DEFAULT TRUE,
    FOREIGN KEY(tool_id) REFERENCES mcp_tool_registry(id) ON DELETE CASCADE
);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO mcp_tool_registry (id, server_name, tool_name, description, input_schema, last_discovered)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"github/create_issue", "github", "create_issue",
		"Create a new issue in a GitHub repository", "{}", time.Now().UTC(),
	); err != nil {
		t.Fatalf("insert catalog row: %v", err)
	}

	return mcpgateway.NewGateway(db, mcpgateway.FavoriteConfig{
		Threshold: 5, MaxFavorites: 15, WindowDays: 30, DecayDays: 14,
	})
}

// TestApplyToolDiscovery_UnifiedGatewayPath verifies the single-tool, single
// activation point design: with ToolDiscovery enabled and a gateway present,
// the MCP catalog becomes searchable through tool_search (no
// mcp_query_catalog/mcp_call_tool needed) and calls are routed to the gateway.
func TestApplyToolDiscovery_UnifiedGatewayPath(t *testing.T) {
	gw := newDiscoveryTestGateway(t)

	prev := config.Get()
	config.SetForTests(&config.Config{
		ToolDiscovery: config.ToolDiscoveryConfig{
			Enabled:        true,
			Mode:           "always",
			SearchLimit:    8,
			MaxDirectTools: 64,
		},
	})
	t.Cleanup(func() { config.SetForTests(prev) })

	ResetSharedDiscoveryRegistry()
	t.Cleanup(ResetSharedDiscoveryRegistry)

	live := []tools.BaseTool{
		&discoveryStubTool{name: "bash"},
		&discoveryStubTool{name: "edit"},
	}
	visible := ApplyToolDiscovery(live, gw)

	names := map[string]tools.BaseTool{}
	for _, v := range visible {
		names[v.Info().Name] = v
	}

	// The unified tool must be present; legacy proxy tools must not.
	if _, ok := names["tool_search"]; !ok {
		t.Fatal("tool_search must be in the visible set")
	}
	for _, legacy := range []string{"mcp_query_catalog", "mcp_call_tool"} {
		if _, ok := names[legacy]; ok {
			t.Errorf("legacy proxy tool %q must not be exposed in the unified path", legacy)
		}
	}

	search := names["tool_search"]

	// 1) Discovery: the persisted MCP catalog is searchable.
	resp, err := search.Run(context.Background(), tools.ToolCall{
		Input: `{"query":"create github issue"}`,
	})
	if err != nil {
		t.Fatalf("search run: %v", err)
	}
	if resp.IsError || !strings.Contains(resp.Content, "github_create_issue") {
		t.Fatalf("expected github_create_issue in search results, got: %s", resp.Content)
	}

	// 2) Execution: calls to catalog-only tools are routed to the gateway.
	// No MCP server "github" exists in the test config, so the gateway must
	// fail with its controlled "server not found" error — proving the wiring
	// without needing a live MCP connection.
	resp, err = search.Run(context.Background(), tools.ToolCall{
		Input: `{"tool_name":"github_create_issue","parameters":{"title":"t"}}`,
	})
	if err != nil {
		t.Fatalf("call run: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content, "server not found in config") {
		t.Fatalf("expected gateway routing error, got: %+v", resp)
	}
}

// TestApplyToolDiscovery_DisabledReturnsAll verifies the legacy fallback: with
// ToolDiscovery disabled the tool list passes through unchanged.
func TestApplyToolDiscovery_DisabledReturnsAll(t *testing.T) {
	prev := config.Get()
	config.SetForTests(&config.Config{ToolDiscovery: config.ToolDiscoveryConfig{Enabled: false}})
	t.Cleanup(func() { config.SetForTests(prev) })

	live := []tools.BaseTool{&discoveryStubTool{name: "bash"}}
	out := ApplyToolDiscovery(live, nil)
	if len(out) != 1 || out[0].Info().Name != "bash" {
		t.Fatalf("expected unchanged tool list, got %d tools", len(out))
	}
}
