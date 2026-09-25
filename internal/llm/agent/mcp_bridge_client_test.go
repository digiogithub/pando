package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"

	"github.com/mark3labs/mcp-go/mcp"
)

var xcodeBridgeConfig = config.MCPServer{Type: config.MCPStdio, Command: "/usr/bin/xcrun", Args: []string{"mcpbridge"}}

func TestGlobalXcodeBridgeToolReusesPersistentClient(t *testing.T) {
	stats, _ := installInProcessSessionMCP(t)
	t.Cleanup(closeGlobalBridgeClients)
	perms := &recordingPermissions{approve: true}

	echo := NewMcpTool("xcode", mcp.NewTool("echo", mcp.WithString("text")), perms, xcodeBridgeConfig)
	ctx := sessionToolCallContext("s-direct-bridge")
	for i, text := range []string{"one", "two"} {
		resp, err := echo.Run(ctx, tools.ToolCall{ID: "c", Name: "xcode_echo", Input: `{"text":"` + text + `"}`})
		if err != nil || resp.IsError || !strings.Contains(resp.Content, "echo:"+text) {
			t.Fatalf("call %d: resp=%+v err=%v", i, resp, err)
		}
	}
	if got := stats.clients.Load(); got != 1 {
		t.Fatalf("clients created across two direct calls = %d, want 1", got)
	}
	if got := stats.initializes.Load(); got != 1 {
		t.Fatalf("Initialize ran %d times, want 1", got)
	}
	if got := stats.closes.Load(); got != 0 {
		t.Fatalf("persistent client closed %d times between calls", got)
	}

	// A configuration change replaces the client.
	changed := xcodeBridgeConfig
	changed.Env = []string{"MCP_XCODE_PID=7"}
	echo2 := NewMcpTool("xcode", mcp.NewTool("echo", mcp.WithString("text")), perms, changed)
	if resp, err := echo2.Run(ctx, tools.ToolCall{ID: "c3", Name: "xcode_echo", Input: `{"text":"three"}`}); err != nil || resp.IsError {
		t.Fatalf("call after config change: resp=%+v err=%v", resp, err)
	}
	if got := stats.clients.Load(); got != 2 {
		t.Fatalf("clients after config change = %d, want 2", got)
	}
	if got := stats.closes.Load(); got != 1 {
		t.Fatalf("stale client closes = %d, want 1", got)
	}

	// Resetting the MCP tool cache (config reload) closes the client.
	ResetMcpToolsCache()
	if got := stats.closes.Load(); got != 2 {
		t.Fatalf("closes after ResetMcpToolsCache = %d, want 2", got)
	}
}

func TestToolsForSessionDropsGlobalBridgeWhenSessionHasBridge(t *testing.T) {
	installInProcessSessionMCP(t)
	perms := &recordingPermissions{approve: true}

	globalBridge := NewMcpTool("xcode", mcp.NewTool("BuildProject"), perms, xcodeBridgeConfig)
	globalOther := NewMcpTool("other", mcp.NewTool("search"), perms, config.MCPServer{Type: config.MCPStdio, Command: "other-server"})
	a := &agent{tools: []tools.BaseTool{namedTool{"bash"}, globalBridge, globalOther}}

	if err := AttachSessionMCPServers(context.Background(), "s-bridge", map[string]config.MCPServer{
		"xcode-tools": {Type: config.MCPStdio, Command: "/Applications/Xcode.app/Contents/Developer/usr/bin/mcpbridge"},
	}, perms); err != nil {
		t.Fatalf("attach: %v", err)
	}
	t.Cleanup(func() { DetachSessionMCPServers("s-bridge") })
	if err := AttachSessionMCPServers(context.Background(), "s-plain", map[string]config.MCPServer{
		"zed": {Type: config.MCPStdio, Command: "context-server"},
	}, perms); err != nil {
		t.Fatalf("attach plain: %v", err)
	}
	t.Cleanup(func() { DetachSessionMCPServers("s-plain") })

	got := strings.Join(toolNames(a.toolsForSession("s-bridge")), ",")
	if strings.Contains(got, "xcode_BuildProject") {
		t.Fatalf("global bridge tool leaked into a session with its own bridge: %s", got)
	}
	if !strings.Contains(got, "other_search") || !strings.Contains(got, "xcode-tools_echo") {
		t.Fatalf("expected other_search and the session bridge tools, got %s", got)
	}

	for _, sid := range []string{"s-plain", "no-session-mcp"} {
		if names := strings.Join(toolNames(a.toolsForSession(sid)), ","); !strings.Contains(names, "xcode_BuildProject") {
			t.Fatalf("session %s lost the global bridge tool: %s", sid, names)
		}
	}

	// A deferred/direct call from the bridge session is refused before any
	// connection is made.
	resp, err := globalBridge.Run(sessionToolCallContext("s-bridge"), tools.ToolCall{ID: "c", Name: "xcode_BuildProject", Input: `{}`})
	if err != nil || !resp.IsError || !strings.Contains(resp.Content, "attached its own Xcode bridge") {
		t.Fatalf("expected refusal, got resp=%+v err=%v", resp, err)
	}
}
