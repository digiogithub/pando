package tooldiscovery_test

import (
	"testing"

	"github.com/digiogithub/pando/internal/llm/tooldiscovery"
	"github.com/digiogithub/pando/internal/llm/tools"
)

func buildLargeRegistry(n int) (*tooldiscovery.Registry, []tools.BaseTool) {
	reg := tooldiscovery.NewRegistry()
	var all []tools.BaseTool
	// Core tools (non-deferred)
	core := []string{"bash", "edit", "view", "glob", "grep", "write", "patch", "ls", "cache_read", "cache_stats", "todo_write"}
	for _, name := range core {
		t := &stubTool{name: name, desc: "core tool"}
		all = append(all, t)
		_ = reg.Register(t, tooldiscovery.SourceCore, true)
	}
	// MCP tools (deferred by default)
	for i := 0; i < n; i++ {
		name := "mcp_server_tool_" + string(rune('a'+i%26))
		t := &stubTool{name: name, desc: "mcp tool"}
		all = append(all, t)
		_ = reg.Register(t, tooldiscovery.SourceMCP, false)
	}
	return reg, all
}

func TestPolicy_ModeOff_AllToolsVisible(t *testing.T) {
	reg, _ := buildLargeRegistry(80)
	cfg := tooldiscovery.PolicyConfig{
		Mode:           tooldiscovery.ModeOff,
		MaxDirectTools: 10,
		NonDeferredTools: []string{"bash", "edit", "view", "glob", "grep", "write", "patch", "ls",
			"cache_read", "cache_stats", "todo_write"},
		DeferredSources: []string{"mcp"},
	}
	policy := tooldiscovery.NewSelectionPolicy(cfg)
	visible := policy.Apply(reg, nil)
	// All 91 tools should be visible (11 core + 80 mcp).
	if len(visible) != 91 {
		t.Errorf("expected 91 tools, got %d", len(visible))
	}
}

func TestPolicy_ModeAuto_BelowThreshold(t *testing.T) {
	reg, _ := buildLargeRegistry(5) // 11 core + 5 mcp = 16 total, below MaxDirectTools=64
	cfg := tooldiscovery.DefaultPolicyConfig()
	policy := tooldiscovery.NewSelectionPolicy(cfg)
	visible := policy.Apply(reg, nil)
	// All 16 tools should be visible.
	if len(visible) != 16 {
		t.Errorf("expected 16 tools, got %d", len(visible))
	}
}

func TestPolicy_ModeAuto_AboveThreshold(t *testing.T) {
	reg, _ := buildLargeRegistry(60) // 11 core + 60 mcp = 71 total, above MaxDirectTools=64
	cfg := tooldiscovery.DefaultPolicyConfig()
	policy := tooldiscovery.NewSelectionPolicy(cfg)

	searchTool := &stubTool{name: "tool_search", desc: "search tools"}
	visible := policy.Apply(reg, searchTool)

	// Only core (11) + tool_search (1) should be visible; MCP tools are deferred.
	names := make(map[string]bool)
	for _, v := range visible {
		names[v.Info().Name] = true
	}
	if !names["tool_search"] {
		t.Error("tool_search must be in visible set")
	}
	if !names["bash"] {
		t.Error("bash must be in visible set")
	}
	// No MCP tool should be visible before discovery.
	for _, v := range visible {
		name := v.Info().Name
		if len(name) > 4 && name[:4] == "mcp_" {
			t.Errorf("MCP tool %q should be deferred", name)
		}
	}
}

func TestPolicy_ModeAlways_ForcesDeferred(t *testing.T) {
	reg, _ := buildLargeRegistry(2) // only 13 tools, below default threshold
	cfg := tooldiscovery.PolicyConfig{
		Mode:             tooldiscovery.ModeAlways,
		MaxDirectTools:   64,
		NonDeferredTools: []string{"bash", "edit"},
		DeferredSources:  []string{"mcp"},
	}
	policy := tooldiscovery.NewSelectionPolicy(cfg)
	searchTool := &stubTool{name: "tool_search", desc: "search tools"}
	visible := policy.Apply(reg, searchTool)

	names := make(map[string]bool)
	for _, v := range visible {
		names[v.Info().Name] = true
	}
	if !names["tool_search"] || !names["bash"] || !names["edit"] {
		t.Error("expected tool_search, bash, edit in visible set")
	}
	for _, v := range visible {
		name := v.Info().Name
		if len(name) > 4 && name[:4] == "mcp_" {
			t.Errorf("MCP tool %q should be deferred in ModeAlways", name)
		}
	}
}

func TestPolicy_DiscoveredToolsBecomVisible(t *testing.T) {
	reg, _ := buildLargeRegistry(60)
	cfg := tooldiscovery.DefaultPolicyConfig()
	policy := tooldiscovery.NewSelectionPolicy(cfg)

	// Simulate a tool_search result marking a deferred tool as discovered.
	reg.MarkDiscovered("mcp_server_tool_a")

	searchTool := &stubTool{name: "tool_search"}
	visible := policy.Apply(reg, searchTool)

	names := make(map[string]bool)
	for _, v := range visible {
		names[v.Info().Name] = true
	}
	if !names["mcp_server_tool_a"] {
		t.Error("discovered tool mcp_server_tool_a should be visible")
	}
}
