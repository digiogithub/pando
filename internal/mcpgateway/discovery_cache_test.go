package mcpgateway

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	llmtools "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/mcpclient"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// fakeDiscoveryClient advertises a fixed tool list and counts connections.
type fakeDiscoveryClient struct {
	tools []mcp.Tool
}

func (f *fakeDiscoveryClient) Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, nil
}

func (f *fakeDiscoveryClient) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{Tools: f.tools}, nil
}

func (f *fakeDiscoveryClient) CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("ok"), nil
}

func (f *fakeDiscoveryClient) Close() error { return nil }

// installFakeDiscovery replaces the discovery client factory; the returned
// counter reports how many connections discovery opened.
func installFakeDiscovery(t *testing.T, toolNames ...string) *atomic.Int32 {
	t.Helper()
	var connections atomic.Int32
	prev := newDiscoveryClient
	newDiscoveryClient = func(context.Context, string, config.MCPServer) (mcpclient.Client, error) {
		connections.Add(1)
		c := &fakeDiscoveryClient{}
		for _, name := range toolNames {
			c.tools = append(c.tools, mcp.NewTool(name, mcp.WithDescription("fake "+name)))
		}
		return c, nil
	}
	t.Cleanup(func() { newDiscoveryClient = prev })
	return &connections
}

func createDiscoveryCacheTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := createProxyToolsTestDB(t)
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS mcp_server_fingerprints (
    server_name TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`)
	require.NoError(t, err)
	return db
}

func TestInitializeSkipsServerWithCachedCatalog(t *testing.T) {
	connections := installFakeDiscovery(t, "build", "test")
	db := createDiscoveryCacheTestDB(t)
	gw := &Gateway{registry: NewRegistry(db), pool: NewClientPool()}
	servers := map[string]config.MCPServer{
		"xcode": {Type: config.MCPStdio, Command: "/usr/bin/xcrun", Args: []string{"mcpbridge"}},
	}
	ctx := context.Background()

	require.NoError(t, gw.Initialize(ctx, servers))
	require.EqualValues(t, 1, connections.Load(), "first start must discover")

	require.NoError(t, gw.Initialize(ctx, servers))
	require.NoError(t, gw.Initialize(ctx, servers))
	require.EqualValues(t, 1, connections.Load(), "unchanged config must reuse the cached catalog")

	all, err := gw.GetAllTools(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestInitializeRediscoversWhenConfigChanges(t *testing.T) {
	connections := installFakeDiscovery(t, "build")
	db := createDiscoveryCacheTestDB(t)
	gw := &Gateway{registry: NewRegistry(db), pool: NewClientPool()}
	ctx := context.Background()

	srv := config.MCPServer{Type: config.MCPStdio, Command: "/usr/bin/xcrun", Args: []string{"mcpbridge"}}
	require.NoError(t, gw.Initialize(ctx, map[string]config.MCPServer{"xcode": srv}))
	require.EqualValues(t, 1, connections.Load())

	srv.Env = []string{"MCP_XCODE_PID=42"}
	require.NoError(t, gw.Initialize(ctx, map[string]config.MCPServer{"xcode": srv}))
	require.EqualValues(t, 2, connections.Load(), "a changed config must be rediscovered")

	require.NoError(t, gw.Initialize(ctx, map[string]config.MCPServer{"xcode": srv}))
	require.EqualValues(t, 2, connections.Load(), "the new fingerprint must be cached")
}

func TestInitializeRediscoversServerWithoutCatalogRows(t *testing.T) {
	connections := installFakeDiscovery(t) // advertises no tools
	db := createDiscoveryCacheTestDB(t)
	gw := &Gateway{registry: NewRegistry(db), pool: NewClientPool()}
	ctx := context.Background()
	servers := map[string]config.MCPServer{"empty": {Type: config.MCPStdio, Command: "empty-server"}}

	require.NoError(t, gw.Initialize(ctx, servers))
	require.NoError(t, gw.Initialize(ctx, servers))
	require.EqualValues(t, 2, connections.Load(), "no catalog rows means no cache")
}

func TestRefreshServerAlwaysRediscovers(t *testing.T) {
	connections := installFakeDiscovery(t, "build")
	db := createDiscoveryCacheTestDB(t)
	gw := &Gateway{registry: NewRegistry(db), pool: NewClientPool()}
	ctx := context.Background()
	srv := config.MCPServer{Type: config.MCPStdio, Command: "mcpbridge"}

	require.NoError(t, gw.Initialize(ctx, map[string]config.MCPServer{"xcode": srv}))
	n, err := gw.RefreshServer(ctx, "xcode", srv)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.EqualValues(t, 2, connections.Load(), "an explicit refresh ignores the cache")

	// The refresh stored the fingerprint again, so the next start is cached.
	require.NoError(t, gw.Initialize(ctx, map[string]config.MCPServer{"xcode": srv}))
	require.EqualValues(t, 2, connections.Load())

	// Deleting the server data forgets the fingerprint.
	require.NoError(t, gw.DeleteServerData(ctx, "xcode"))
	fp, err := gw.registry.serverFingerprint(ctx, "xcode")
	require.NoError(t, err)
	require.Empty(t, fp)
}

func TestConfigFingerprintIsStableAndSensitive(t *testing.T) {
	a := config.MCPServer{Type: config.MCPSse, URL: "https://x", Headers: map[string]string{"A": "1", "B": "2"}}
	b := config.MCPServer{Type: config.MCPSse, URL: "https://x", Headers: map[string]string{"B": "2", "A": "1"}, Timeout: "5m"}
	require.Equal(t, ConfigFingerprint(a), ConfigFingerprint(b), "map order and timeout must not matter")
	b.Headers["A"] = "changed"
	require.NotEqual(t, ConfigFingerprint(a), ConfigFingerprint(b))
}

func TestSessionWithXcodeBridgeHidesGlobalBridgeServers(t *testing.T) {
	db := createProxyToolsTestDB(t)
	insertProxyTool(t, db, "xcode/BuildProject", "xcode", "BuildProject")
	insertProxyTool(t, db, "other/search_docs", "other", "search_docs")

	prevCfg := config.Get()
	config.SetForTests(&config.Config{MCPServers: map[string]config.MCPServer{
		"xcode": {Type: config.MCPStdio, Command: "xcrun", Args: []string{"mcpbridge"}},
		"other": {Type: config.MCPStdio, Command: "other-server"},
	}})
	t.Cleanup(func() { config.SetForTests(prevCfg) })
	SetSessionXcodeBridgeHook(func(sessionID string) bool { return sessionID == "xcode-session" })
	t.Cleanup(func() { SetSessionXcodeBridgeHook(nil) })

	gw := &Gateway{registry: NewRegistry(db), pool: NewClientPool()}
	catalog := &CatalogTool{gateway: gw}
	proxy := &CallToolProxy{gateway: gw}

	xcodeCtx := context.WithValue(context.Background(), llmtools.SessionIDContextKey, "xcode-session")
	resp, err := catalog.Run(xcodeCtx, llmtools.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.Equal(t, []string{"other/search_docs"}, toolIDs(t, decodeCatalog(t, resp.Content)))

	resp, err = proxy.Run(xcodeCtx, llmtools.ToolCall{Input: `{"tool_name":"BuildProject"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "attached its own Xcode bridge")

	// Other sessions still see and reach the global bridge.
	plainCtx := context.WithValue(context.Background(), llmtools.SessionIDContextKey, "plain-session")
	resp, err = catalog.Run(plainCtx, llmtools.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.Equal(t, []string{"other/search_docs", "xcode/BuildProject"}, toolIDs(t, decodeCatalog(t, resp.Content)))
}
