package mcpgateway

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"testing"
	"time"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/stretchr/testify/require"
	toon "github.com/toon-format/toon-go"
)

// catalogPayload is the decoded form of the response rendered by
// tools.NewStructuredResponse, whose preferred format is TOON (it falls back to
// TOML and then JSON only when TOON cannot encode the value).
//
// It decodes into map[string]any rather than a struct on purpose: toon.Unmarshal
// does not honour struct tags and silently leaves fields zeroed instead of
// erroring, which would make these assertions vacuous.
type catalogPayload map[string]any

func decodeCatalog(t *testing.T, content string) catalogPayload {
	t.Helper()
	value, err := toon.DecodeString(content)
	require.NoError(t, err, "catalog response is not valid TOON:\n%s", content)
	payload, ok := value.(map[string]any)
	require.True(t, ok, "expected a TOON object, got %T", value)
	return payload
}

// toolIDs returns the "ID" of every tool in the response, in order. The keys are
// the JSON tag names of catalogResponse; RegisteredTool has no tags, so its keys
// are the exported field names verbatim.
func toolIDs(t *testing.T, payload catalogPayload) []string {
	t.Helper()
	raw, ok := payload["tools"]
	if !ok {
		return nil
	}
	entries, ok := raw.([]any)
	require.True(t, ok, "expected tools to be a list, got %T", raw)

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		tool, ok := entry.(map[string]any)
		require.True(t, ok, "expected a tool object, got %T", entry)
		id, ok := tool["ID"].(string)
		require.True(t, ok, "expected tool ID to be a string, got %T", tool["ID"])
		ids = append(ids, id)
	}
	return ids
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

	got := decodeCatalog(t, resp.Content)
	require.EqualValues(t, 3, got["total"])
	require.Len(t, toolIDs(t, got), 3)
	require.Equal(t, false, got["has_more"])
	// next_offset is omitempty: on the last page it is absent rather than zero.
	require.EqualValues(t, 0, valueOrZero(got["next_offset"]))
}

// valueOrZero normalizes an omitempty numeric field that may be absent.
func valueOrZero(v any) any {
	if v == nil {
		return 0
	}
	return v
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

		got := decodeCatalog(t, resp.Content)
		require.EqualValues(t, totalTools, got["total"])
		for _, id := range toolIDs(t, got) {
			seen[id] = true
		}
		pages++
		require.LessOrEqual(t, pages, totalTools, "pagination did not terminate")
		if got["has_more"] != true {
			require.EqualValues(t, 0, valueOrZero(got["next_offset"]))
			break
		}
		returned, err := strconv.Atoi(fmt.Sprint(got["returned"]))
		require.NoError(t, err)
		nextOffset, err := strconv.Atoi(fmt.Sprint(got["next_offset"]))
		require.NoError(t, err)
		require.Equal(t, offset+returned, nextOffset)
		offset = nextOffset
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

	got := decodeCatalog(t, resp.Content)
	require.EqualValues(t, 4, got["total"])
	require.Len(t, toolIDs(t, got), 2)
	require.EqualValues(t, 1, got["offset"])
	require.Equal(t, true, got["has_more"])
	require.EqualValues(t, 3, got["next_offset"])
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
