package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/digiogithub/pando/internal/luaengine"
)

var nonAlphanumericRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// sanitizeLuaToolName converts a Lua tool name to a safe LLM-facing name.
// All non-alphanumeric characters (except underscore) are replaced with "_".
// The result is prefixed with "lua_".
func sanitizeLuaToolName(name string) string {
	safe := nonAlphanumericRe.ReplaceAllString(name, "_")
	return "lua_" + safe
}

// LuaTool wraps a Lua-defined function as a BaseTool.
// The tool name exposed to the LLM is prefixed with "lua_" to avoid name collisions.
type LuaTool struct {
	def       luaengine.LuaToolDef
	filterMgr *luaengine.FilterManager
}

// NewLuaTool creates a LuaTool from a LuaToolDef and its owning FilterManager.
func NewLuaTool(def luaengine.LuaToolDef, fm *luaengine.FilterManager) *LuaTool {
	return &LuaTool{def: def, filterMgr: fm}
}

// Info returns the ToolInfo for this Lua-defined tool.
func (t *LuaTool) Info() ToolInfo {
	params := make(map[string]any, len(t.def.Parameters))
	for name, p := range t.def.Parameters {
		params[name] = map[string]any{
			"type":        p.Type,
			"description": p.Description,
		}
	}
	return ToolInfo{
		Name:        sanitizeLuaToolName(t.def.Name),
		Description: t.def.Description,
		Parameters:  params,
		Required:    t.def.Required,
	}
}

// Run executes the Lua tool with the parameters supplied in call.Input.
func (t *LuaTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params map[string]interface{}
	if call.Input != "" {
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %s", err)), nil
		}
	}
	if params == nil {
		params = make(map[string]interface{})
	}

	// Validate required parameters are present.
	for _, req := range t.def.Required {
		if _, ok := params[req]; !ok {
			return NewTextErrorResponse(fmt.Sprintf("missing required parameter: %s", req)), nil
		}
	}

	result, err := t.filterMgr.ExecuteLuaTool(ctx, t.def.Name, params)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	return NewTextResponse(result), nil
}

// NewLuaToolsFromManager creates BaseTool instances for all Lua tools
// discovered in the FilterManager's loaded script.
// Returns nil if fm is nil, disabled, or has no tools defined.
func NewLuaToolsFromManager(fm *luaengine.FilterManager) []BaseTool {
	if fm == nil || !fm.IsEnabled() {
		return nil
	}
	defs := fm.GetLuaTools()
	result := make([]BaseTool, 0, len(defs))
	for _, def := range defs {
		result = append(result, NewLuaTool(def, fm))
	}
	return result
}
