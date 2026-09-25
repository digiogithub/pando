package agent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// countingMCPClient wraps an in-process mcp-go client and counts the
// lifecycle calls the session registry makes on it.
type countingMCPClient struct {
	inner   *client.Client
	stats   *sessionMCPStats
	failOne *atomic.Bool // when set, the next CallTool fails as a dead transport
}

type sessionMCPStats struct {
	clients     atomic.Int32
	initializes atomic.Int32
	calls       atomic.Int32
	closes      atomic.Int32
}

func (c *countingMCPClient) Start(ctx context.Context) error { return c.inner.Start(ctx) }

func (c *countingMCPClient) Initialize(ctx context.Context, req mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	c.stats.initializes.Add(1)
	return c.inner.Initialize(ctx, req)
}

func (c *countingMCPClient) ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return c.inner.ListTools(ctx, req)
}

func (c *countingMCPClient) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if c.failOne != nil && c.failOne.CompareAndSwap(true, false) {
		return nil, transport.ErrTransportClosed
	}
	c.stats.calls.Add(1)
	return c.inner.CallTool(ctx, req)
}

func (c *countingMCPClient) Close() error {
	c.stats.closes.Add(1)
	return c.inner.Close()
}

// installInProcessSessionMCP swaps the client factory for one that serves an
// in-process MCP server with an "echo" and a "view" tool.
func installInProcessSessionMCP(t *testing.T) (*sessionMCPStats, *atomic.Bool) {
	t.Helper()
	srv := server.NewMCPServer("fixture", "1.0.0")
	srv.AddTool(mcp.NewTool("echo", mcp.WithDescription("echo text"), mcp.WithString("text", mcp.Required())),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("echo:" + req.GetString("text", "")), nil
		})
	srv.AddTool(mcp.NewTool("view", mcp.WithDescription("fixture view")),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("fixture view"), nil
		})

	prevCfg := config.Get()
	config.SetForTests(&config.Config{})
	t.Cleanup(func() { config.SetForTests(prevCfg) })

	stats := &sessionMCPStats{}
	failOne := &atomic.Bool{}
	prev := newSessionMCPClient
	newSessionMCPClient = func(_ context.Context, _ string, _ config.MCPServer) (MCPClient, error) {
		inner, err := client.NewInProcessClient(srv)
		if err != nil {
			return nil, err
		}
		stats.clients.Add(1)
		return &countingMCPClient{inner: inner, stats: stats, failOne: failOne}, nil
	}
	t.Cleanup(func() { newSessionMCPClient = prev })
	return stats, failOne
}

type namedTool struct{ name string }

func (n namedTool) Info() tools.ToolInfo { return tools.ToolInfo{Name: n.name} }
func (n namedTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	return tools.NewTextResponse(n.name), nil
}

func sessionToolCallContext(sessionID string) context.Context {
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, sessionID)
	return context.WithValue(ctx, tools.MessageIDContextKey, "message-1")
}

func findTool(list []tools.BaseTool, name string) tools.BaseTool {
	for _, t := range list {
		if t.Info().Name == name {
			return t
		}
	}
	return nil
}

func TestAttachSessionMCPServersExposesToolsToThatSessionOnly(t *testing.T) {
	installInProcessSessionMCP(t)
	perms := &recordingPermissions{approve: true}

	if err := AttachSessionMCPServers(context.Background(), "s-mcp-1", map[string]config.MCPServer{
		"xcode tools": {Type: config.MCPStdio, Command: "unused"},
	}, perms); err != nil {
		t.Fatalf("attach: %v", err)
	}
	t.Cleanup(func() { DetachSessionMCPServers("s-mcp-1") })

	names := sessionMCPToolNames("s-mcp-1")
	if strings.Join(names, ",") != "xcode_tools_echo,xcode_tools_view" {
		t.Fatalf("session tool names = %v", names)
	}
	if got := SessionTools("s-mcp-other"); len(got) != 0 {
		t.Fatalf("another session sees %d session tools", len(got))
	}

	a := &agent{agentName: config.AgentCoder, tools: []tools.BaseTool{namedTool{"bash"}}}
	if got := a.toolsForSession("s-mcp-other"); len(got) != 1 {
		t.Fatalf("other session tool count = %d, want 1", len(got))
	}
	if got := a.toolsForSession("s-mcp-1"); len(got) != 3 || findTool(got, "xcode_tools_echo") == nil {
		t.Fatalf("session tool set = %v", toolNames(got))
	}
	if listing := promptMcpCatalogListing(context.WithValue(context.Background(), tools.SessionIDContextKey, "s-mcp-1")); !strings.Contains(listing, "xcode_tools_echo") {
		t.Fatalf("prompt catalog misses session tools: %q", listing)
	}

	// Agents configured without tools stay tool-less.
	bare := &agent{agentName: config.AgentTitle}
	if got := bare.toolsForSession("s-mcp-1"); len(got) != 0 {
		t.Fatalf("tool-less agent got %d tools", len(got))
	}
}

func TestToolsForSessionOverridesByName(t *testing.T) {
	base := []tools.BaseTool{namedTool{"bash"}, namedTool{"srv_echo"}}
	session := []tools.BaseTool{namedTool{"srv_echo"}, namedTool{"srv_view"}}
	merged := mergeSessionTools(base, session)
	if got := strings.Join(toolNames(merged), ","); got != "bash,srv_echo,srv_view" {
		t.Fatalf("merged = %s", got)
	}
	if merged[1] != session[0] {
		t.Fatal("the session tool must replace the global tool with the same name")
	}
	if len(base) != 2 || base[1].Info().Name != "srv_echo" {
		t.Fatal("base slice mutated")
	}
}

func TestSessionMCPToolUsesPersistentClient(t *testing.T) {
	stats, failOne := installInProcessSessionMCP(t)
	perms := &recordingPermissions{approve: true}
	if err := AttachSessionMCPServers(context.Background(), "s-mcp-2", map[string]config.MCPServer{
		"srv": {Type: config.MCPStdio, Command: "unused"},
	}, perms); err != nil {
		t.Fatalf("attach: %v", err)
	}
	t.Cleanup(func() { DetachSessionMCPServers("s-mcp-2") })

	echo := findTool(SessionTools("s-mcp-2"), "srv_echo")
	if echo == nil {
		t.Fatal("srv_echo not attached")
	}
	ctx := sessionToolCallContext("s-mcp-2")
	for i, text := range []string{"one", "two"} {
		resp, err := echo.Run(ctx, tools.ToolCall{ID: "c", Name: "srv_echo", Input: `{"text":"` + text + `"}`})
		if err != nil || resp.IsError || !strings.Contains(resp.Content, "echo:"+text) {
			t.Fatalf("call %d: resp=%+v err=%v", i, resp, err)
		}
	}
	if got := stats.initializes.Load(); got != 1 {
		t.Fatalf("Initialize ran %d times across two calls, want 1", got)
	}
	if got := stats.clients.Load(); got != 1 {
		t.Fatalf("clients created = %d, want 1", got)
	}
	if len(perms.seen) != 2 || perms.seen[0] != "srv_echo" {
		t.Fatalf("permission requests = %v", perms.seen)
	}

	// A dead transport reconnects once and retries transparently.
	failOne.Store(true)
	resp, err := echo.Run(ctx, tools.ToolCall{ID: "c3", Name: "srv_echo", Input: `{"text":"three"}`})
	if err != nil || resp.IsError || !strings.Contains(resp.Content, "echo:three") {
		t.Fatalf("retry call: resp=%+v err=%v", resp, err)
	}
	if got := stats.clients.Load(); got != 2 {
		t.Fatalf("clients after reconnect = %d, want 2", got)
	}

	// Permission denial never reaches the server.
	perms.approve = false
	calls := stats.calls.Load()
	resp, _ = echo.Run(ctx, tools.ToolCall{ID: "c4", Name: "srv_echo", Input: `{"text":"x"}`})
	if !resp.IsError || stats.calls.Load() != calls {
		t.Fatalf("denied call reached the server: %+v", resp)
	}
}

func TestDetachSessionMCPServersClosesClient(t *testing.T) {
	stats, _ := installInProcessSessionMCP(t)
	if err := AttachSessionMCPServers(context.Background(), "s-mcp-3", map[string]config.MCPServer{
		"srv": {Type: config.MCPStdio, Command: "unused"},
	}, &recordingPermissions{approve: true}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	echo := findTool(SessionTools("s-mcp-3"), "srv_echo")

	DetachSessionMCPServers("s-mcp-3")
	if got := stats.closes.Load(); got != 1 {
		t.Fatalf("closes = %d, want 1", got)
	}
	if got := SessionTools("s-mcp-3"); len(got) != 0 {
		t.Fatalf("tools survive detach: %d", len(got))
	}
	// A stale tool reference must not resurrect the connection.
	resp, _ := echo.Run(sessionToolCallContext("s-mcp-3"), tools.ToolCall{ID: "c", Name: "srv_echo", Input: `{"text":"x"}`})
	if !resp.IsError || stats.clients.Load() != 1 {
		t.Fatalf("detached tool reconnected: resp=%+v clients=%d", resp, stats.clients.Load())
	}
}

func TestAttachSessionMCPServersReplacesAndKeepsPartialSuccess(t *testing.T) {
	stats, _ := installInProcessSessionMCP(t)
	working := newSessionMCPClient
	var mu sync.Mutex
	newSessionMCPClient = func(ctx context.Context, name string, srv config.MCPServer) (MCPClient, error) {
		mu.Lock()
		defer mu.Unlock()
		if name == "broken" {
			return nil, context.DeadlineExceeded
		}
		return working(ctx, name, srv)
	}

	servers := map[string]config.MCPServer{"srv": {Type: config.MCPStdio}, "broken": {Type: config.MCPStdio}}
	if err := AttachSessionMCPServers(context.Background(), "s-mcp-4", servers, &recordingPermissions{approve: true}); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("expected the broken server's error, got %v", err)
	}
	t.Cleanup(func() { DetachSessionMCPServers("s-mcp-4") })
	if got := len(SessionTools("s-mcp-4")); got != 2 {
		t.Fatalf("working server tools = %d, want 2", got)
	}

	// Re-attaching (session/load) replaces the set and closes the old client.
	if err := AttachSessionMCPServers(context.Background(), "s-mcp-4", map[string]config.MCPServer{"other": {Type: config.MCPStdio}}, nil); err != nil {
		t.Fatalf("re-attach: %v", err)
	}
	if names := sessionMCPToolNames("s-mcp-4"); strings.Join(names, ",") != "other_echo,other_view" {
		t.Fatalf("names after re-attach = %v", names)
	}
	if got := stats.closes.Load(); got != 1 {
		t.Fatalf("old client closes = %d, want 1", got)
	}
}

func TestSessionMCPToolNameIsSanitizedAndCapped(t *testing.T) {
	if got := sessionMCPToolName(sanitizeMCPName("xcode.tools/β"), "run tests"); got != "xcode_tools___run_tests" {
		t.Fatalf("name = %q", got)
	}
	long := sessionMCPToolName(strings.Repeat("s", 60), "tool_name")
	if len(long) != maxMCPToolNameLen {
		t.Fatalf("len = %d", len(long))
	}
}

func toolNames(list []tools.BaseTool) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.Info().Name)
	}
	return out
}
