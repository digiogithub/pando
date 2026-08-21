package agent

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/pkg/extension"
)

type stubTool struct{ name string }

func (t stubTool) Info() extension.ToolInfo { return extension.ToolInfo{Name: t.name} }
func (t stubTool) Run(context.Context, extension.ToolCall) (extension.ToolResponse, error) {
	return extension.NewTextResponse("ok"), nil
}

type stubProvider struct{}

func (stubProvider) ExtensionInfo() extension.Info {
	return extension.Info{
		ID:      "tools.acme",
		Version: "1.0.0",
		New:     func() extension.Extension { return stubProvider{} },
	}
}
func (stubProvider) Tools() []extension.Tool { return []extension.Tool{stubTool{name: "acme_tool"}} }

type coreStub struct{}

func (coreStub) Info() tools.ToolInfo { return tools.ToolInfo{Name: "bash"} }
func (coreStub) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	return tools.NewTextResponse(""), nil
}

// The agent must pick up extension tools through the same choke point every
// coder tool set goes through, whether or not tool discovery is enabled.
func TestApplyToolDiscoveryIncludesExtensionTools(t *testing.T) {
	reg := extension.NewRegistry()
	reg.Register(stubProvider{})
	mgr := extension.NewManager(extension.Options{Registry: reg})
	if err := mgr.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	SetExtensionManager(mgr)
	t.Cleanup(func() {
		SetExtensionManager(nil)
		mgr.Cleanup()
	})

	got := ApplyToolDiscovery([]tools.BaseTool{coreStub{}}, nil)

	names := make(map[string]bool, len(got))
	for _, tool := range got {
		names[tool.Info().Name] = true
	}
	if !names["acme_tool"] {
		t.Errorf("extension tool missing from the agent tool set: %v", names)
	}
	if !names["bash"] {
		t.Errorf("core tool was dropped: %v", names)
	}
}

func TestApplyToolDiscoveryWithoutManagerIsUnchanged(t *testing.T) {
	SetExtensionManager(nil)
	got := ApplyToolDiscovery([]tools.BaseTool{coreStub{}}, nil)
	if len(got) != 1 || got[0].Info().Name != "bash" {
		t.Errorf("got %d tools", len(got))
	}
}
