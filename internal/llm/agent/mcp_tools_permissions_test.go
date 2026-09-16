package agent

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/permission"

	"github.com/mark3labs/mcp-go/mcp"
)

// recordingPermissions is a permission.Service that only records what it was
// asked and answers with a fixed verdict. It embeds the interface so it keeps
// compiling when unrelated methods are added; only the two Request methods are
// ever called here.
type recordingPermissions struct {
	permission.Service
	name    string
	approve bool
	seen    []string
}

func (p *recordingPermissions) Request(opts permission.CreatePermissionRequest) bool {
	return p.RequestWithContext(context.Background(), opts)
}

func (p *recordingPermissions) RequestWithContext(_ context.Context, opts permission.CreatePermissionRequest) bool {
	p.seen = append(p.seen, opts.ToolName)
	return p.approve
}

// seedMcpCatalog installs a one-tool discovery result in the package cache
// without connecting to anything, and restores the previous state afterwards.
// The server config deliberately points at nothing: these tests deny the
// permission, so the tool must never get as far as opening a connection.
func seedMcpCatalog(t *testing.T) {
	t.Helper()
	prevCfg := config.Get()
	config.SetForTests(&config.Config{})
	prev := cachedMcpToolDescriptors()
	mcpToolsMu.Lock()
	mcpToolDescriptors = []mcpToolDescriptor{{
		serverName: "fixture",
		tool:       mcp.Tool{Name: "ping", Description: "ping"},
		mcpConfig:  config.MCPServer{Type: config.MCPStdio, Command: "/nonexistent/mcp-server"},
	}}
	mcpToolsMu.Unlock()
	t.Cleanup(func() {
		mcpToolsMu.Lock()
		mcpToolDescriptors = prev
		mcpToolsMu.Unlock()
		config.SetForTests(prevCfg)
	})
}

func toolCallContext() context.Context {
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "session-1")
	return context.WithValue(ctx, tools.MessageIDContextKey, "message-1")
}

// TestGetMcpToolsBindsTheCallersPermissionService is the PANDO-US-0031 core
// regression: the cache holds discovery results, not permission-bound tools, so
// two callers with two different services each get tools that ask their own.
func TestGetMcpToolsBindsTheCallersPermissionService(t *testing.T) {
	seedMcpCatalog(t)
	ctx := toolCallContext()

	first := &recordingPermissions{name: "first", approve: false}
	second := &recordingPermissions{name: "second", approve: false}

	firstTools := GetMcpTools(ctx, first)
	secondTools := GetMcpTools(ctx, second)
	if len(firstTools) != 1 || len(secondTools) != 1 {
		t.Fatalf("expected one MCP tool per caller, got %d and %d", len(firstTools), len(secondTools))
	}
	if firstTools[0].Info().Name != "fixture_ping" {
		t.Fatalf("unexpected tool name %q", firstTools[0].Info().Name)
	}

	if _, err := firstTools[0].Run(ctx, tools.ToolCall{ID: "c1", Name: "fixture_ping", Input: "{}"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(first.seen) != 1 || len(second.seen) != 0 {
		t.Fatalf("the first caller's tool asked the wrong service: first=%v second=%v", first.seen, second.seen)
	}

	if _, err := secondTools[0].Run(ctx, tools.ToolCall{ID: "c2", Name: "fixture_ping", Input: "{}"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(second.seen) != 1 {
		t.Fatalf("the second caller's tool never reached its own service: %v", second.seen)
	}
	if len(first.seen) != 1 {
		t.Fatalf("the second caller's tool asked the first caller's service: %v", first.seen)
	}
}

// TestPromptCatalogListingDoesNotPoisonTheCache covers PANDO-US-0031 defect 2:
// the name-only prompt path must read the cached catalog without binding any
// permission service, so a real caller that comes after it still gets tools
// bound to its own.
func TestPromptCatalogListingDoesNotPoisonTheCache(t *testing.T) {
	seedMcpCatalog(t)
	ctx := toolCallContext()

	// Name path first -- this is the order that used to deadlock everything.
	if listing := promptMcpCatalogListing(ctx); listing == "" {
		t.Fatal("the prompt listing must still name the discovered MCP tools")
	}
	if names := CachedMcpToolNames(); len(names) != 1 || names[0] != "fixture_ping" {
		t.Fatalf("unexpected cached names: %v", names)
	}

	caller := &recordingPermissions{name: "caller", approve: false}
	callerTools := GetMcpTools(ctx, caller)
	if len(callerTools) != 1 {
		t.Fatalf("expected one MCP tool, got %d", len(callerTools))
	}
	if _, err := callerTools[0].Run(ctx, tools.ToolCall{ID: "c1", Name: "fixture_ping", Input: "{}"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(caller.seen) != 1 {
		t.Fatalf("the tool did not ask the caller's own permission service: %v", caller.seen)
	}
}

// TestPromptCatalogListingDoesNotDiscover pins the other half of defect 2: with
// an empty cache the name path stays silent instead of triggering discovery and
// filling the cache on everyone else's behalf.
func TestPromptCatalogListingDoesNotDiscover(t *testing.T) {
	prevCfg := config.Get()
	config.SetForTests(&config.Config{MCPServers: map[string]config.MCPServer{
		"fixture": {Type: config.MCPStdio, Command: "/nonexistent/mcp-server"},
	}})
	prev := cachedMcpToolDescriptors()
	ResetMcpToolsCache()
	t.Cleanup(func() {
		mcpToolsMu.Lock()
		mcpToolDescriptors = prev
		mcpToolsMu.Unlock()
		config.SetForTests(prevCfg)
	})

	_ = promptMcpCatalogListing(context.Background())
	if got := cachedMcpToolDescriptors(); len(got) != 0 {
		t.Fatalf("the name-only path must not populate the catalog cache, got %d entries", len(got))
	}
}

// TestPermissionDeniedShortCircuitsTheMcpCall proves the denial path still
// returns an error response rather than attempting the call.
func TestPermissionDeniedShortCircuitsTheMcpCall(t *testing.T) {
	seedMcpCatalog(t)
	ctx := toolCallContext()

	denier := &recordingPermissions{name: "denier", approve: false}
	tool := GetMcpTools(ctx, denier)[0]
	resp, err := tool.Run(ctx, tools.ToolCall{ID: "c1", Name: "fixture_ping", Input: "{}"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !resp.IsError || resp.Content != "permission denied" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
