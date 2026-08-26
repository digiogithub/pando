package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/design"
)

func designToolSet() []BaseTool {
	return []BaseTool{
		NewDesignCreateTool(nil),
		NewDesignRenderTool(),
		NewDesignInspectTool(),
		NewDesignPatchTool(nil),
		NewDesignScreenshotTool(),
		NewDesignVersionsTool(nil),
		NewDesignExportTool(nil),
		NewDesignCanvasTool(nil),
		NewDesignSystemTool(nil),
		NewDesignCritiqueTool(),
		NewDesignSkillsTool(nil),
		NewDesignPresentTool(),
	}
}

func TestDesignToolsAreRegisteredAsBuiltin(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range designToolSet() {
		name := tool.Info().Name
		seen[name] = true
		if !IsBuiltinTool(name) {
			t.Errorf("%s is not registered as a builtin tool, so the MCP gateway would try to route it", name)
		}
	}
	for _, name := range DesignToolNames {
		if !seen[name] {
			t.Errorf("%s is listed in DesignToolNames but no constructor produces it", name)
		}
	}
	if len(seen) != len(DesignToolNames) {
		t.Fatalf("DesignToolNames lists %d tools, %d were constructed", len(DesignToolNames), len(seen))
	}
}

// Every tool schema is sent to the model on each request, so a malformed one is
// a per-request cost as well as a correctness problem.
func TestDesignToolSchemasAreWellFormed(t *testing.T) {
	for _, tool := range designToolSet() {
		info := tool.Info()
		if strings.TrimSpace(info.Description) == "" {
			t.Errorf("%s has no description", info.Name)
		}
		if info.Parameters == nil {
			t.Errorf("%s has no parameters", info.Name)
			continue
		}
		for _, required := range info.Required {
			if _, ok := info.Parameters[required]; !ok {
				t.Errorf("%s requires %q but does not declare it", info.Name, required)
			}
		}
		if _, err := json.Marshal(info.Parameters); err != nil {
			t.Errorf("%s parameters are not serialisable: %v", info.Name, err)
		}
	}
}

func TestDesignToolsReportMissingSubsystem(t *testing.T) {
	// With no provider installed the tools must explain themselves rather than
	// surfacing a bare error: this is the shape a stripped-down entry point hits.
	call := ToolCall{Name: DesignRenderToolName, Input: `{"artifact_id":"dsg_x"}`}
	resp, err := NewDesignRenderTool().Run(context.Background(), call)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content, "not available") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestDesignToolsValidateInput(t *testing.T) {
	cases := []struct {
		tool BaseTool
		in   string
		want string
	}{
		{NewDesignCreateTool(nil), `{}`, "title is required"},
		{NewDesignRenderTool(), `{}`, "artifact_id is required"},
		{NewDesignPatchTool(nil), `{"artifact_id":"dsg_x"}`, "ops is required"},
		{NewDesignCanvasTool(nil), `{"dest":"a.png"}`, "html is required"},
		{NewDesignCanvasTool(nil), `{"html":"<html></html>"}`, "dest is required"},
	}
	for _, tc := range cases {
		resp, err := tc.tool.Run(context.Background(), ToolCall{Input: tc.in})
		if err != nil {
			t.Fatalf("%s: %v", tc.tool.Info().Name, err)
		}
		if !resp.IsError || !strings.Contains(resp.Content, tc.want) {
			t.Errorf("%s with %s: want %q, got %q", tc.tool.Info().Name, tc.in, tc.want, resp.Content)
		}
	}
}

// The design_system tool grew from a token reader into the surface that builds
// and applies a system, so its advertised actions are a contract: an agent that
// reads the description and calls an action the switch does not handle gets an
// "unknown action" instead of the work it asked for.
func TestDesignSystemToolAdvertisesEveryAction(t *testing.T) {
	info := NewDesignSystemTool(nil).Info()
	action, ok := info.Parameters["action"].(map[string]any)
	if !ok {
		t.Fatal("design_system has no action parameter")
	}
	enum, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("action enum is %T, want []string", action["enum"])
	}
	want := map[string]bool{"get": false, "init": false, "set": false, "extract": false, "apply": false, "examples": false}
	for _, value := range enum {
		if _, known := want[value]; !known {
			t.Errorf("action enum advertises %q, which nothing handles", value)
		}
		want[value] = true
	}
	for value, found := range want {
		if !found {
			t.Errorf("action %q is implemented but not advertised", value)
		}
	}
	for _, param := range []string{"source", "target", "artifact_id"} {
		if _, ok := info.Parameters[param]; !ok {
			t.Errorf("design_system is missing the %q parameter", param)
		}
	}
	for _, mention := range []string{"DESIGN.md", "extract", "apply"} {
		if !strings.Contains(info.Description, mention) {
			t.Errorf("description never mentions %q", mention)
		}
	}
}

// The examples listing is answered without a design service, so it works in a
// process where the subsystem was never wired.
func TestDesignSystemExamplesNeedNoSubsystem(t *testing.T) {
	resp, err := NewDesignSystemTool(nil).Run(context.Background(), ToolCall{
		Input: `{"action":"examples"}`,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if resp.IsError {
		t.Fatalf("listing examples must not need the subsystem: %s", resp.Content)
	}
	for _, name := range design.ExampleSystemNames() {
		if !strings.Contains(resp.Content, name) {
			t.Errorf("listing omits the bundled guide %q", name)
		}
	}
}
