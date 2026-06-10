package tooldiscovery_test

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/llm/tooldiscovery"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// stubTool is a minimal BaseTool for tests.
type stubTool struct {
	name string
	desc string
}

func (s *stubTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: s.name, Description: s.desc}
}
func (s *stubTool) Run(_ context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	return tools.NewTextResponse("ok"), nil
}

// aliasedTool implements ToolMetadataProvider.
type aliasedTool struct {
	stubTool
	meta tooldiscovery.ToolMetadata
}

func (a *aliasedTool) ToolMetadata() tooldiscovery.ToolMetadata { return a.meta }

func TestRegistry_RegisterAndResolve(t *testing.T) {
	reg := tooldiscovery.NewRegistry()

	bash := &stubTool{name: "bash", desc: "run shell commands"}
	if err := reg.Register(bash, tooldiscovery.SourceCore, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := reg.Resolve("bash"); got == nil {
		t.Fatal("expected to resolve 'bash'")
	}
	if got := reg.Resolve("nonexistent"); got != nil {
		t.Fatal("expected nil for unknown tool")
	}
}

func TestRegistry_AliasResolution(t *testing.T) {
	reg := tooldiscovery.NewRegistry()

	tool := &aliasedTool{
		stubTool: stubTool{name: "edit", desc: "edit files"},
		meta: tooldiscovery.ToolMetadata{
			CanonicalName: "edit",
			Aliases:       []string{"file_edit", "modify"},
			Source:        tooldiscovery.SourceCore,
			NonDeferred:   true,
		},
	}
	if err := reg.Register(tool, tooldiscovery.SourceCore, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, alias := range []string{"edit", "file_edit", "modify"} {
		if got := reg.Resolve(alias); got == nil {
			t.Errorf("expected to resolve alias %q", alias)
		}
	}
}

func TestRegistry_AliasCollision(t *testing.T) {
	reg := tooldiscovery.NewRegistry()

	t1 := &aliasedTool{
		stubTool: stubTool{name: "tool_a"},
		meta:     tooldiscovery.ToolMetadata{CanonicalName: "tool_a", Aliases: []string{"shared"}},
	}
	t2 := &aliasedTool{
		stubTool: stubTool{name: "tool_b"},
		meta:     tooldiscovery.ToolMetadata{CanonicalName: "tool_b", Aliases: []string{"shared"}},
	}

	if err := reg.Register(t1, tooldiscovery.SourceInternal, false); err != nil {
		t.Fatalf("unexpected error registering t1: %v", err)
	}
	if err := reg.Register(t2, tooldiscovery.SourceInternal, false); err == nil {
		t.Fatal("expected collision error for duplicate alias 'shared'")
	}
}

func TestRegistry_Discovery(t *testing.T) {
	reg := tooldiscovery.NewRegistry()
	_ = reg.Register(&stubTool{name: "tool_x"}, tooldiscovery.SourceMCP, false)
	_ = reg.Register(&stubTool{name: "tool_y"}, tooldiscovery.SourceMCP, false)

	if reg.IsDiscovered("tool_x") {
		t.Fatal("tool_x should not be discovered yet")
	}
	reg.MarkDiscovered("tool_x")
	if !reg.IsDiscovered("tool_x") {
		t.Fatal("tool_x should be discovered")
	}
	if reg.IsDiscovered("tool_y") {
		t.Fatal("tool_y should not be discovered")
	}
	discovered := reg.DiscoveredTools()
	if len(discovered) != 1 || discovered[0].Info().Name != "tool_x" {
		t.Fatalf("unexpected discovered tools: %v", discovered)
	}
}
