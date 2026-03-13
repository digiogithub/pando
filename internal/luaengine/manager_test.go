package luaengine_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/luaengine"
)

// writeTempScript writes content to a temp file and returns its path.
// The caller is responsible for removing it.
func writeTempScript(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "pando-lua-test-*.lua")
	if err != nil {
		t.Fatalf("create temp script: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		t.Fatalf("write temp script: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestFilterManager_InputFilter(t *testing.T) {
	script := `
_G["test-server-input"] = function(ctx)
    ctx.parameters.modified = true
    ctx.parameters.server = ctx.server_name
    return ctx.parameters
end
`
	path := writeTempScript(t, script)
	defer os.Remove(path)

	fm, err := luaengine.NewFilterManager(path, 5*time.Second, false)
	if err != nil {
		t.Fatalf("NewFilterManager: %v", err)
	}
	defer fm.Close()

	params := map[string]interface{}{"key": "value"}
	hookCtx := luaengine.NewInputContext("test-server", "my-tool", params, "req-1")

	result, err := fm.ApplyInputFilter(context.Background(), hookCtx)
	if err != nil {
		t.Fatalf("ApplyInputFilter: %v", err)
	}
	if !result.Modified {
		t.Error("expected Modified=true")
	}
	if v, ok := result.Data["modified"].(bool); !ok || !v {
		t.Errorf("expected modified=true in result data, got %v", result.Data["modified"])
	}
	if s, ok := result.Data["server"].(string); !ok || s != "test-server" {
		t.Errorf("expected server=test-server, got %v", result.Data["server"])
	}
}

func TestFilterManager_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	// An infinite loop that will trigger the timeout.
	script := `
_G["loop-server-input"] = function(ctx)
    while true do end
    return ctx.parameters
end
`
	path := writeTempScript(t, script)
	defer os.Remove(path)

	fm, err := luaengine.NewFilterManager(path, 300*time.Millisecond, false)
	if err != nil {
		t.Fatalf("NewFilterManager: %v", err)
	}
	defer fm.Close()

	params := map[string]interface{}{"x": 1}
	hookCtx := luaengine.NewInputContext("loop-server", "tool", params, "req-timeout")

	start := time.Now()
	result, err := fm.ApplyInputFilter(context.Background(), hookCtx)
	elapsed := time.Since(start)

	// In non-strict mode, timeout returns original data without error.
	if err != nil {
		t.Errorf("expected no error in non-strict mode on timeout, got: %v", err)
	}
	if result.Modified {
		t.Error("expected Modified=false after timeout")
	}
	// Should complete within ~2 seconds (timeout + small buffer).
	if elapsed > 2*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestFilterManager_NonStrictMode(t *testing.T) {
	// Script that raises an error.
	script := `
_G["error-server-input"] = function(ctx)
    error("intentional error for testing")
end
`
	path := writeTempScript(t, script)
	defer os.Remove(path)

	fm, err := luaengine.NewFilterManager(path, 5*time.Second, false)
	if err != nil {
		t.Fatalf("NewFilterManager: %v", err)
	}
	defer fm.Close()

	params := map[string]interface{}{"original": "data"}
	hookCtx := luaengine.NewInputContext("error-server", "tool", params, "req-err")

	result, err := fm.ApplyInputFilter(context.Background(), hookCtx)
	// Non-strict: error is logged, original data is returned.
	if err != nil {
		t.Errorf("non-strict mode should not propagate error, got: %v", err)
	}
	if result.Modified {
		t.Error("non-strict mode should not modify data on error")
	}
	if v, ok := result.Data["original"].(string); !ok || v != "data" {
		t.Errorf("non-strict mode should return original data, got: %v", result.Data)
	}
}

func TestFilterManager_StrictModeError(t *testing.T) {
	script := `
_G["strict-server-input"] = function(ctx)
    error("strict mode error")
end
`
	path := writeTempScript(t, script)
	defer os.Remove(path)

	fm, err := luaengine.NewFilterManager(path, 5*time.Second, true)
	if err != nil {
		t.Fatalf("NewFilterManager: %v", err)
	}
	defer fm.Close()

	params := map[string]interface{}{"x": 1}
	hookCtx := luaengine.NewInputContext("strict-server", "tool", params, "req-strict")

	_, err = fm.ApplyInputFilter(context.Background(), hookCtx)
	if err == nil {
		t.Error("strict mode should propagate Lua errors")
	}
}

func TestFilterManager_LoadScriptError_NonStrict(t *testing.T) {
	// Non-existent path in non-strict mode — should not error.
	fm, err := luaengine.NewFilterManager("/nonexistent/path/filter.lua", 5*time.Second, false)
	if err != nil {
		t.Fatalf("non-strict mode should not error on missing script, got: %v", err)
	}
	defer fm.Close()

	if !fm.IsEnabled() {
		t.Error("manager should remain enabled even when script is missing (non-strict)")
	}
}

func TestFilterManager_LoadScriptError_Strict(t *testing.T) {
	// Non-existent path in strict mode — should error.
	_, err := luaengine.NewFilterManager("/nonexistent/path/filter.lua", 5*time.Second, true)
	if err == nil {
		t.Error("strict mode should error on missing script")
	}
}

func TestFilterManager_NoFilter_ReturnOriginal(t *testing.T) {
	// Script with no matching function — returns original data unchanged.
	script := `-- no filter functions defined`
	path := writeTempScript(t, script)
	defer os.Remove(path)

	fm, err := luaengine.NewFilterManager(path, 5*time.Second, false)
	if err != nil {
		t.Fatalf("NewFilterManager: %v", err)
	}
	defer fm.Close()

	params := map[string]interface{}{"keep": "me"}
	hookCtx := luaengine.NewInputContext("unknown-server", "tool", params, "req-noop")

	result, err := fm.ApplyInputFilter(context.Background(), hookCtx)
	if err != nil {
		t.Fatalf("ApplyInputFilter: %v", err)
	}
	if result.Modified {
		t.Error("expected Modified=false when no matching filter")
	}
	if v, ok := result.Data["keep"].(string); !ok || v != "me" {
		t.Errorf("expected original data preserved, got: %v", result.Data)
	}
}
