package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/luaengine"
)

func TestSanitizeLuaToolName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"git-status", "lua_git_status"},
		{"count lines", "lua_count_lines"},
		{"simple", "lua_simple"},
		{"my.tool.name", "lua_my_tool_name"},
		{"tool__name", "lua_tool__name"},
		{"UPPER-case", "lua_UPPER_case"},
	}
	for _, c := range cases {
		got := sanitizeLuaToolName(c.input)
		if got != c.want {
			t.Errorf("sanitizeLuaToolName(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestNewLuaToolsFromManager_Nil(t *testing.T) {
	result := NewLuaToolsFromManager(nil)
	if result != nil {
		t.Errorf("expected nil for nil manager, got %v", result)
	}
}

func newTestFilterManager(t *testing.T, script string) *luaengine.FilterManager {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "test.lua")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatalf("write lua script: %v", err)
	}
	fm, err := luaengine.NewFilterManager(scriptPath, 5*time.Second, true)
	if err != nil {
		t.Fatalf("NewFilterManager: %v", err)
	}
	t.Cleanup(fm.Close)
	return fm
}

const testLuaScript = `
pando_tools = {
  greet = {
    description = "Greets a person by name",
    parameters = {
      name = {
        type = "string",
        description = "The name to greet",
        required = true,
      },
    },
    run = function(params)
      return "Hello, " .. params.name .. "!", nil
    end,
  },
  add = {
    description = "Adds two numbers",
    parameters = {
      a = { type = "number", description = "First number", required = true },
      b = { type = "number", description = "Second number", required = true },
    },
    run = function(params)
      return tostring(params.a + params.b), nil
    end,
  },
  fail_tool = {
    description = "Always returns an error",
    parameters = {},
    run = function(params)
      return nil, "intentional error"
    end,
  },
}
`

func TestLuaTool_Info(t *testing.T) {
	fm := newTestFilterManager(t, testLuaScript)
	defs := fm.GetLuaTools()
	if len(defs) == 0 {
		t.Fatal("expected Lua tools to be discovered")
	}

	var greetDef luaengine.LuaToolDef
	for _, d := range defs {
		if d.Name == "greet" {
			greetDef = d
			break
		}
	}
	if greetDef.Name == "" {
		t.Fatal("greet tool not found")
	}

	tool := NewLuaTool(greetDef, fm)
	info := tool.Info()

	if info.Name != "lua_greet" {
		t.Errorf("Info().Name = %q, want %q", info.Name, "lua_greet")
	}
	if info.Description != "Greets a person by name" {
		t.Errorf("Info().Description = %q", info.Description)
	}
	if _, ok := info.Parameters["name"]; !ok {
		t.Error("expected 'name' parameter in Info().Parameters")
	}
	if len(info.Required) == 0 {
		t.Error("expected Required to contain 'name'")
	}
}

func TestLuaTool_Info_ParametersJSONSchema(t *testing.T) {
	fm := newTestFilterManager(t, testLuaScript)
	defs := fm.GetLuaTools()

	var addDef luaengine.LuaToolDef
	for _, d := range defs {
		if d.Name == "add" {
			addDef = d
			break
		}
	}
	if addDef.Name == "" {
		t.Fatal("add tool not found")
	}

	tool := NewLuaTool(addDef, fm)
	info := tool.Info()

	for _, paramName := range []string{"a", "b"} {
		p, ok := info.Parameters[paramName]
		if !ok {
			t.Fatalf("parameter %q not found in Info().Parameters", paramName)
		}
		pMap, ok := p.(map[string]any)
		if !ok {
			t.Fatalf("parameter %q is not map[string]any", paramName)
		}
		if pMap["type"] != "number" {
			t.Errorf("parameter %q type = %v, want number", paramName, pMap["type"])
		}
	}
}

func TestLuaTool_Run_Success(t *testing.T) {
	fm := newTestFilterManager(t, testLuaScript)
	defs := fm.GetLuaTools()

	var greetDef luaengine.LuaToolDef
	for _, d := range defs {
		if d.Name == "greet" {
			greetDef = d
			break
		}
	}

	tool := NewLuaTool(greetDef, fm)
	resp, err := tool.Run(context.Background(), ToolCall{
		Input: `{"name": "World"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Errorf("unexpected error response: %s", resp.Content)
	}
	if resp.Content != "Hello, World!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello, World!")
	}
}

func TestLuaTool_Run_MissingRequired(t *testing.T) {
	fm := newTestFilterManager(t, testLuaScript)
	defs := fm.GetLuaTools()

	var greetDef luaengine.LuaToolDef
	for _, d := range defs {
		if d.Name == "greet" {
			greetDef = d
			break
		}
	}

	tool := NewLuaTool(greetDef, fm)
	resp, err := tool.Run(context.Background(), ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Error("expected IsError=true for missing required param")
	}
}

func TestLuaTool_Run_LuaError(t *testing.T) {
	fm := newTestFilterManager(t, testLuaScript)
	defs := fm.GetLuaTools()

	var failDef luaengine.LuaToolDef
	for _, d := range defs {
		if d.Name == "fail_tool" {
			failDef = d
			break
		}
	}
	if failDef.Name == "" {
		t.Fatal("fail_tool not found")
	}

	tool := NewLuaTool(failDef, fm)
	resp, err := tool.Run(context.Background(), ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Error("expected IsError=true when Lua tool returns error")
	}
}

func TestLuaTool_Run_InvalidJSON(t *testing.T) {
	fm := newTestFilterManager(t, testLuaScript)
	defs := fm.GetLuaTools()

	tool := NewLuaTool(defs[0], fm)
	resp, err := tool.Run(context.Background(), ToolCall{Input: `not json`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Error("expected IsError=true for invalid JSON input")
	}
}

func TestNewLuaToolsFromManager_WithTools(t *testing.T) {
	fm := newTestFilterManager(t, testLuaScript)
	tools := NewLuaToolsFromManager(fm)
	if len(tools) == 0 {
		t.Error("expected tools to be returned")
	}
	for _, tool := range tools {
		info := tool.Info()
		if len(info.Name) < len("lua_") || info.Name[:4] != "lua_" {
			t.Errorf("tool name %q does not start with lua_", info.Name)
		}
	}
}

func TestNewLuaToolsFromManager_EmptyScript(t *testing.T) {
	fm := newTestFilterManager(t, `-- no tools defined`)
	tools := NewLuaToolsFromManager(fm)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for script with no pando_tools, got %d", len(tools))
	}
}
