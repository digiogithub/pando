package luaengine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	lua "github.com/yuin/gopher-lua"
)

// FilterManager manages Lua filter and hook execution.
type FilterManager struct {
	scriptPath     string
	L              *lua.LState
	enabled        bool
	timeout        time.Duration
	strictMode     bool
	toolsEnabled   bool
	allowedModules []string
	mu             sync.RWMutex
	scriptLoaded   bool
	luaTools       []LuaToolDef
}

// NewFilterManager creates a new FilterManager instance.
// If scriptPath is empty or the file doesn't exist, filters are disabled.
// In non-strict mode, script load errors are logged but do not return an error.
func NewFilterManager(scriptPath string, timeout time.Duration, strictMode bool) (*FilterManager, error) {
	fm := &FilterManager{
		scriptPath:   scriptPath,
		enabled:      true,
		timeout:      timeout,
		strictMode:   strictMode,
		toolsEnabled: true, // enabled by default for backwards compatibility
	}

	fm.L = NewLuaState()

	if scriptPath != "" {
		if err := fm.LoadScript(); err != nil {
			if strictMode {
				fm.L.Close()
				return nil, fmt.Errorf("failed to load filter script: %w", err)
			}
			logging.Warn("Failed to load Lua filter script, continuing without filters",
				"script_path", scriptPath,
				"error", err)
		}
	}

	return fm, nil
}

// NewFilterManagerFromConfig creates a FilterManager from a LuaConfig.
// Returns nil, nil when lua is disabled or cfg is nil.
func NewFilterManagerFromConfig(cfg *config.LuaConfig) (*FilterManager, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	timeout := 5 * time.Second
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil && d > 0 {
			timeout = d
		}
	}

	fm := &FilterManager{
		scriptPath:     cfg.ScriptPath,
		enabled:        true,
		timeout:        timeout,
		strictMode:     cfg.StrictMode,
		toolsEnabled:   cfg.ToolsEnabled,
		allowedModules: cfg.AllowedModules,
	}
	fm.L = NewLuaStateWithModules(cfg.AllowedModules)

	if cfg.ScriptPath != "" {
		if err := fm.LoadScript(); err != nil {
			if cfg.StrictMode {
				fm.L.Close()
				return nil, fmt.Errorf("failed to load filter script: %w", err)
			}
			logging.Warn("Failed to load Lua filter script, continuing without filters",
				"script_path", cfg.ScriptPath,
				"error", err)
		}
	}

	return fm, nil
}

// IsEnabled returns whether the filter manager is active.
func (fm *FilterManager) IsEnabled() bool {
	return fm.enabled
}

// LoadScript loads the Lua filter script from the configured path.
func (fm *FilterManager) LoadScript() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.scriptPath == "" {
		return fmt.Errorf("no script path configured")
	}

	if _, err := os.Stat(fm.scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("script file does not exist: %s", fm.scriptPath)
	}

	if err := fm.L.DoFile(fm.scriptPath); err != nil {
		return fmt.Errorf("failed to execute script %s: %w", fm.scriptPath, err)
	}

	fm.scriptLoaded = true
	fm.luaTools = DiscoverLuaTools(fm.L)
	logging.Info("Lua filter script loaded", "script_path", fm.scriptPath)
	return nil
}

// ReloadScript closes the current Lua state, creates a fresh one, and reloads the script.
func (fm *FilterManager) ReloadScript() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.L != nil {
		CloseLuaState(fm.L)
		fm.L = nil
	}

	fm.L = NewLuaState()
	fm.scriptLoaded = false

	if err := fm.L.DoFile(fm.scriptPath); err != nil {
		return fmt.Errorf("failed to reload script %s: %w", fm.scriptPath, err)
	}

	fm.scriptLoaded = true
	fm.luaTools = DiscoverLuaTools(fm.L)
	logging.Info("Lua filter script reloaded", "script_path", fm.scriptPath)
	return nil
}

// Close releases all resources held by the FilterManager.
func (fm *FilterManager) Close() {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fm.L != nil {
		CloseLuaState(fm.L)
		fm.L = nil
	}
}

// ApplyInputFilter applies the input filter for the given MCP tool invocation.
// It looks for function `<serverName>-input`, with fallback to `global-input`.
func (fm *FilterManager) ApplyInputFilter(ctx context.Context, hookCtx *HookContext) (*HookResult, error) {
	if !fm.enabled || !fm.scriptLoaded {
		result := NewHookResult()
		result.Data = hookCtx.Parameters
		return result, nil
	}

	functionName := buildFilterFunctionName(hookCtx.ServerName, FilterInput)
	return fm.executeFilter(ctx, functionName, hookCtx)
}

// ApplyOutputFilter applies the output filter for the given MCP tool result.
// It looks for function `<serverName>-output`, with fallback to `global-output`.
func (fm *FilterManager) ApplyOutputFilter(ctx context.Context, hookCtx *HookContext) (*HookResult, error) {
	if !fm.enabled || !fm.scriptLoaded {
		result := NewHookResult()
		result.Data = hookCtx.Result
		return result, nil
	}

	functionName := buildFilterFunctionName(hookCtx.ServerName, FilterOutput)
	return fm.executeFilter(ctx, functionName, hookCtx)
}

// ExecuteHook executes a lifecycle hook function.
// It looks for function `hook_<hookType>`, with fallback to `hook_global`.
func (fm *FilterManager) ExecuteHook(ctx context.Context, hookType HookType, data map[string]interface{}) (*HookResult, error) {
	if !fm.enabled || !fm.scriptLoaded {
		result := NewHookResult()
		result.Data = data
		return result, nil
	}

	hookCtx := &HookContext{
		HookType:   hookType,
		Parameters: data,
		Timestamp:  time.Now().Unix(),
		FilterType: FilterInput,
	}

	functionName := fmt.Sprintf("hook_%s", hookType)
	return fm.executeFilter(ctx, functionName, hookCtx)
}

// buildFilterFunctionName builds the Lua function name for a filter.
// Format: <server-name>-input or <server-name>-output
func buildFilterFunctionName(serverName string, filterType FilterType) string {
	normalized := strings.ReplaceAll(serverName, "_", "-")
	normalized = strings.ReplaceAll(normalized, ".", "-")
	return fmt.Sprintf("%s-%s", normalized, filterType)
}

// executeFilter executes a named Lua function with timeout.
// Falls back to global-<type> for filters, hook_global for hooks.
func (fm *FilterManager) executeFilter(ctx context.Context, functionName string, hookCtx *HookContext) (*HookResult, error) {
	startTime := time.Now()
	result := NewHookResult()

	fm.mu.RLock()
	defer fm.mu.RUnlock()

	// Resolve the function, trying fallback if not found
	fn := fm.L.GetGlobal(functionName)
	if fn == lua.LNil {
		var fallback string
		if strings.HasPrefix(functionName, "hook_") {
			fallback = "hook_global"
		} else if strings.HasSuffix(functionName, string("-"+FilterInput)) {
			fallback = fmt.Sprintf("global-%s", FilterInput)
		} else {
			fallback = fmt.Sprintf("global-%s", FilterOutput)
		}
		fn = fm.L.GetGlobal(fallback)
		if fn == lua.LNil {
			// No filter found — return original data unchanged
			if hookCtx.FilterType == FilterInput {
				result.Data = hookCtx.Parameters
			} else {
				result.Data = hookCtx.Result
			}
			return result, nil
		}
		functionName = fallback
	}

	contextTable := fm.buildContextTable(hookCtx)

	done := make(chan struct{}, 1)
	var execErr error

	go func() {
		defer func() { done <- struct{}{} }()

		if err := fm.L.CallByParam(lua.P{
			Fn:      fn,
			NRet:    1,
			Protect: true,
		}, contextTable); err != nil {
			execErr = err
			return
		}

		ret := fm.L.Get(-1)
		fm.L.Pop(1)

		if ret == lua.LNil {
			execErr = fmt.Errorf("filter function %s returned nil", functionName)
			return
		}

		if retTable, ok := ret.(*lua.LTable); ok {
			result.Data = LuaTableToMap(retTable)
			result.Modified = true
		} else {
			execErr = fmt.Errorf("filter function %s did not return a table", functionName)
		}
	}()

	select {
	case <-done:
		if execErr != nil {
			logging.Error("Lua filter execution failed", "function", functionName, "error", execErr)
			if fm.strictMode {
				return result, execErr
			}
			// Non-strict: log and return original data
			if hookCtx.FilterType == FilterInput {
				result.Data = hookCtx.Parameters
			} else {
				result.Data = hookCtx.Result
			}
			result.Modified = false
		}

	case <-time.After(fm.timeout):
		err := fmt.Errorf("filter function %s timed out after %v", functionName, fm.timeout)
		logging.Error("Lua filter timeout", "function", functionName, "timeout", fm.timeout)
		if fm.strictMode {
			return result, err
		}
		if hookCtx.FilterType == FilterInput {
			result.Data = hookCtx.Parameters
		} else {
			result.Data = hookCtx.Result
		}
		result.Modified = false

	case <-ctx.Done():
		return result, ctx.Err()
	}

	result.ExecutionTime = time.Since(startTime)
	return result, nil
}

// GetLuaTools returns the list of Lua-defined tools discovered in the loaded script.
// Returns nil when tools_enabled is false.
func (fm *FilterManager) GetLuaTools() []LuaToolDef {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	if !fm.toolsEnabled {
		return nil
	}
	return fm.luaTools
}

// ExecuteLuaTool executes a named Lua tool from the pando_tools table.
// Returns the string output or an error.
func (fm *FilterManager) ExecuteLuaTool(ctx context.Context, name string, params map[string]interface{}) (string, error) {
	if !fm.enabled {
		return "", fmt.Errorf("lua engine is disabled")
	}
	if !fm.scriptLoaded {
		return "", fmt.Errorf("no Lua script loaded")
	}
	return ExecuteLuaTool(fm.L, &fm.mu, name, params, fm.timeout)
}

// buildContextTable creates a Lua table from a HookContext.
func (fm *FilterManager) buildContextTable(hookCtx *HookContext) *lua.LTable {
	table := fm.L.NewTable()

	setTableField(fm.L, table, "server_name", hookCtx.ServerName)
	setTableField(fm.L, table, "tool_name", hookCtx.ToolName)
	setTableField(fm.L, table, "request_id", hookCtx.RequestID)
	setTableField(fm.L, table, "session_id", hookCtx.SessionID)
	setTableField(fm.L, table, "timestamp", hookCtx.Timestamp)

	if hookCtx.FilterType == FilterInput && hookCtx.Parameters != nil {
		setTableField(fm.L, table, "parameters", hookCtx.Parameters)
	}

	if hookCtx.FilterType == FilterOutput && hookCtx.Result != nil {
		setTableField(fm.L, table, "result", hookCtx.Result)
		setTableField(fm.L, table, "duration", hookCtx.Duration)
	}

	return table
}
