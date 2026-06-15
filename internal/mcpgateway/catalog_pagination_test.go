package mcpgateway

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

// tomlCatalog mirrors catalogResponse for decoding the TOML rendered by
// tools.NewStructuredResponse. RegisteredTool has no struct tags, so its JSON
// keys (and therefore the TOML keys) are the exported field names verbatim.
type tomlCatalog struct {
	Total      float64 `toml:"total"`
	Returned   float64 `toml:"returned"`
	Offset     float64 `toml:"offset"`
	HasMore    bool    `toml:"has_more"`
	NextOffset float64 `toml:"next_offset"`
	Tools      []struct {
		ID string `toml:"ID"`
	} `toml:"tools"`
}

// insertToolWithDesc inserts a registry row with a controllable description so
// query-filtering can be exercised independently from the tool name.
func insertToolWithDesc(t *testing.T, db *sql.DB, serverName, toolName, description string) {
	t.Helper()
	id := fmt.Sprintf("%s/%s", serverName, toolName)
	_, err := db.Exec(
		`INSERT INTO mcp_tool_registry (id, server_name, tool_name, description, input_schema, last_discovered)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, serverName, toolName, description, "{}", time.Now().UTC(),
	)
	require.NoError(t, err)
}

func TestRegistryListCatalog_ListsAllPaginated(t *testing.T) {
	db := createProxyToolsTestDB(t)
	reg := NewRegistry(db)
	for i := 0; i < 5; i++ {
		insertToolWithDesc(t, db, "srv", fmt.Sprintf("tool%d", i), "generic tool")
	}

	ctx := context.Background()

	// First page: no query, limit 2.
	page, total, err := reg.ListCatalog(ctx, "", 0, 2)
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, page, 2)
	require.Equal(t, "srv/tool0", page[0].ID)
	require.Equal(t, "srv/tool1", page[1].ID)

	// Second page via offset.
	page, total, err = reg.ListCatalog(ctx, "", 2, 2)
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, page, 2)
	require.Equal(t, "srv/tool2", page[0].ID)

	// Last (partial) page.
	page, total, err = reg.ListCatalog(ctx, "", 4, 2)
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, page, 1)
	require.Equal(t, "srv/tool4", page[0].ID)
}

func TestRegistryListCatalog_FiltersByQuery(t *testing.T) {
	db := createProxyToolsTestDB(t)
	reg := NewRegistry(db)
	insertToolWithDesc(t, db, "srv", "search_docs", "search documents in the index")
	insertToolWithDesc(t, db, "srv", "send_email", "deliver an email message")
	insertToolWithDesc(t, db, "srv", "fetch_url", "search the web and fetch a page")

	ctx := context.Background()

	// Matches by description keyword ("search") -> search_docs + fetch_url.
	page, total, err := reg.ListCatalog(ctx, "search", 0, 10)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, page, 2)

	// Matches by tool name.
	page, total, err = reg.ListCatalog(ctx, "send_email", 0, 10)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "srv/send_email", page[0].ID)

	// No match.
	page, total, err = reg.ListCatalog(ctx, "nonexistent", 0, 10)
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Empty(t, page)
}

func TestCatalogToolRun_ListsAllWithoutQuery(t *testing.T) {
	db := createProxyToolsTestDB(t)
	for i := 0; i < 3; i++ {
		insertToolWithDesc(t, db, "srv", fmt.Sprintf("tool%d", i), "generic tool")
	}
	tool := &CatalogTool{gateway: &Gateway{registry: NewRegistry(db)}}

	// No query at all -> should list the whole catalog (first page).
	resp, err := tool.Run(context.Background(), llmtools.ToolCall{Input: "{}"})
	require.NoError(t, err)
	require.False(t, resp.IsError)

	var got tomlCatalog
	require.NoError(t, toml.Unmarshal([]byte(resp.Content), &got))
	require.Equal(t, float64(3), got.Total)
	require.Len(t, got.Tools, 3)
	require.False(t, got.HasMore)
	require.Equal(t, float64(0), got.NextOffset)
}

func TestCatalogToolRun_PaginationReachesEveryTool(t *testing.T) {
	db := createProxyToolsTestDB(t)
	const totalTools = 7
	for i := 0; i < totalTools; i++ {
		insertToolWithDesc(t, db, "srv", fmt.Sprintf("tool%02d", i), "generic tool")
	}
	tool := &CatalogTool{gateway: &Gateway{registry: NewRegistry(db)}}
	ctx := context.Background()

	seen := map[string]bool{}
	offset := 0
	pages := 0
	for {
		input := fmt.Sprintf(`{"max_results": 3, "offset": %d}`, offset)
		resp, err := tool.Run(ctx, llmtools.ToolCall{Input: input})
		require.NoError(t, err)
		require.False(t, resp.IsError)

		var got tomlCatalog
		require.NoError(t, toml.Unmarshal([]byte(resp.Content), &got))
		require.Equal(t, float64(totalTools), got.Total)
		for _, tl := range got.Tools {
			seen[tl.ID] = true
		}
		pages++
		require.LessOrEqual(t, pages, totalTools, "pagination did not terminate")
		if !got.HasMore {
			require.Zero(t, got.NextOffset)
			break
		}
		require.Equal(t, float64(offset)+got.Returned, got.NextOffset)
		offset = int(got.NextOffset)
	}

	require.Len(t, seen, totalTools, "every tool should be reachable through pagination")
	require.Equal(t, 3, pages) // 3 + 3 + 1
}

func TestCallToolProxy_RoutesCatalogMetaTool(t *testing.T) {
	db := createProxyToolsTestDB(t)
	for i := 0; i < 4; i++ {
		insertToolWithDesc(t, db, "srv", fmt.Sprintf("tool%d", i), "generic tool")
	}
	proxy := &CallToolProxy{gateway: &Gateway{registry: NewRegistry(db)}}

	// A model routing the catalog query through mcp_call_tool must still work.
	resp, err := proxy.Run(context.Background(), llmtools.ToolCall{
		Input: `{"tool_name": "mcp_query_catalog", "parameters": {"max_results": 2, "offset": 1}}`,
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)

	var got tomlCatalog
	require.NoError(t, toml.Unmarshal([]byte(resp.Content), &got))
	require.Equal(t, float64(4), got.Total)
	require.Len(t, got.Tools, 2)
	require.Equal(t, float64(1), got.Offset)
	require.True(t, got.HasMore)
	require.Equal(t, float64(3), got.NextOffset)
}

func TestCallToolProxy_RejectsSelfInvocation(t *testing.T) {
	db := createProxyToolsTestDB(t)
	proxy := &CallToolProxy{gateway: &Gateway{registry: NewRegistry(db)}}

	resp, err := proxy.Run(context.Background(), llmtools.ToolCall{
		Input: `{"tool_name": "mcp_call_tool", "parameters": {}}`,
	})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "cannot invoke itself")
}

func TestCallToolProxy_RedirectsBuiltinTools(t *testing.T) {
	db := createProxyToolsTestDB(t)
	proxy := &CallToolProxy{gateway: &Gateway{registry: NewRegistry(db)}}

	for _, name := range []string{"tool_search", "bash", "kb_search_documents", "code_find_symbol", "browser_navigate"} {
		resp, err := proxy.Run(context.Background(), llmtools.ToolCall{
			Input: fmt.Sprintf(`{"tool_name": %q, "parameters": {}}`, name),
		})
		require.NoError(t, err)
		require.True(t, resp.IsError, "expected redirect error for %q", name)
		require.Contains(t, resp.Content, "built-in agent tool")
		require.Contains(t, resp.Content, name)
	}
}

func TestCallToolProxy_UnknownToolHintsCatalog(t *testing.T) {
	db := createProxyToolsTestDB(t)
	proxy := &CallToolProxy{gateway: &Gateway{registry: NewRegistry(db)}}

	resp, err := proxy.Run(context.Background(), llmtools.ToolCall{
		Input: `{"tool_name": "totally_unknown_tool", "parameters": {}}`,
	})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "tool not found")
	require.Contains(t, resp.Content, "mcp_query_catalog")
}

func TestCatalogToolRun_EmptyCatalogMessage(t *testing.T) {
	db := createProxyToolsTestDB(t)
	tool := &CatalogTool{gateway: &Gateway{registry: NewRegistry(db)}}

	resp, err := tool.Run(context.Background(), llmtools.ToolCall{Input: "{}"})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "No MCP tools are currently registered")
}
