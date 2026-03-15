package luaengine

import (
	"sync"
	"testing"
	"time"
)

const luaToolsScript = `
pando_tools = {
    ["git-status"] = {
        description = "Get current git status",
        parameters = {
            verbose = { type = "boolean", description = "Show verbose output", required = false },
            path = { type = "string", description = "Repository path", required = true }
        },
        run = function(params)
            return "git status output"
        end
    },
    ["echo-tool"] = {
        description = "Echo a message",
        parameters = {
            message = { type = "string", description = "Message to echo", required = true }
        },
        run = function(params)
            return params.message
        end
    },
    ["error-tool"] = {
        description = "Tool that always errors",
        parameters = {},
        run = function(params)
            return nil, "something went wrong"
        end
    }
}
`

func TestDiscoverLuaTools(t *testing.T) {
	L := NewLuaState()
	defer CloseLuaState(L)

	if err := L.DoString(luaToolsScript); err != nil {
		t.Fatalf("failed to load test script: %v", err)
	}

	tools := DiscoverLuaTools(L)

	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	byName := make(map[string]LuaToolDef)
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	gitStatus, ok := byName["git-status"]
	if !ok {
		t.Fatal("expected tool 'git-status' not found")
	}

	if gitStatus.Description != "Get current git status" {
		t.Errorf("unexpected description: %q", gitStatus.Description)
	}

	if len(gitStatus.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(gitStatus.Parameters))
	}

	pathParam, ok := gitStatus.Parameters["path"]
	if !ok {
		t.Fatal("expected parameter 'path' not found")
	}
	if !pathParam.Required {
		t.Error("expected 'path' parameter to be required")
	}
	if pathParam.Type != "string" {
		t.Errorf("expected 'path' type 'string', got %q", pathParam.Type)
	}

	verboseParam, ok := gitStatus.Parameters["verbose"]
	if !ok {
		t.Fatal("expected parameter 'verbose' not found")
	}
	if verboseParam.Required {
		t.Error("expected 'verbose' parameter to be optional")
	}
	if verboseParam.Type != "boolean" {
		t.Errorf("expected 'verbose' type 'boolean', got %q", verboseParam.Type)
	}

	if len(gitStatus.Required) != 1 || gitStatus.Required[0] != "path" {
		t.Errorf("expected Required=[\"path\"], got %v", gitStatus.Required)
	}
}

func TestExecuteLuaToolSuccess(t *testing.T) {
	L := NewLuaState()
	defer CloseLuaState(L)

	if err := L.DoString(luaToolsScript); err != nil {
		t.Fatalf("failed to load test script: %v", err)
	}

	var mu sync.RWMutex
	result, err := ExecuteLuaTool(L, &mu, "git-status", map[string]interface{}{}, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "git status output" {
		t.Errorf("expected 'git status output', got %q", result)
	}
}

func TestExecuteLuaToolWithParams(t *testing.T) {
	L := NewLuaState()
	defer CloseLuaState(L)

	if err := L.DoString(luaToolsScript); err != nil {
		t.Fatalf("failed to load test script: %v", err)
	}

	var mu sync.RWMutex
	result, err := ExecuteLuaTool(L, &mu, "echo-tool", map[string]interface{}{"message": "hello world"}, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExecuteLuaToolReturnsError(t *testing.T) {
	L := NewLuaState()
	defer CloseLuaState(L)

	if err := L.DoString(luaToolsScript); err != nil {
		t.Fatalf("failed to load test script: %v", err)
	}

	var mu sync.RWMutex
	_, err := ExecuteLuaTool(L, &mu, "error-tool", map[string]interface{}{}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error from error-tool, got nil")
	}
}

func TestExecuteLuaToolNotFound(t *testing.T) {
	L := NewLuaState()
	defer CloseLuaState(L)

	if err := L.DoString(luaToolsScript); err != nil {
		t.Fatalf("failed to load test script: %v", err)
	}

	var mu sync.RWMutex
	_, err := ExecuteLuaTool(L, &mu, "nonexistent-tool", map[string]interface{}{}, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for nonexistent tool, got nil")
	}
}

func TestDiscoverLuaToolsNoPandoTools(t *testing.T) {
	L := NewLuaState()
	defer CloseLuaState(L)

	// Don't load any script — pando_tools should be absent
	tools := DiscoverLuaTools(L)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools when pando_tools is not defined, got %d", len(tools))
	}
}
