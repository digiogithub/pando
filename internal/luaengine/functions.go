package luaengine

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// LuaToolDefinition describes a tool registered from Lua.
type LuaToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
}

// PromptFunctionOptions holds callbacks for Lua prompt functions.
// These are injected from the calling package to avoid circular dependencies.
type PromptFunctionOptions struct {
	GetConfig      lua.LGFunction
	GetGitStatus   lua.LGFunction
	ListMCPServers lua.LGFunction
	ListTools      lua.LGFunction
	LoadFile       lua.LGFunction
}

const luaRegisteredToolsGlobal = "__pando_registered_tools"

// RegisterPromptFunctions registers Pando-specific functions in the Lua state.
// These are available to Lua scripts for inspecting and manipulating the prompt system.
func RegisterPromptFunctions(L *lua.LState, opts *PromptFunctionOptions) {
	if opts == nil {
		return
	}

	if opts.GetConfig != nil {
		L.SetGlobal("pando_get_config", L.NewFunction(opts.GetConfig))
	}
	if opts.GetGitStatus != nil {
		L.SetGlobal("pando_get_git_status", L.NewFunction(opts.GetGitStatus))
	}
	if opts.ListMCPServers != nil {
		L.SetGlobal("pando_list_mcp_servers", L.NewFunction(opts.ListMCPServers))
	}
	if opts.ListTools != nil {
		L.SetGlobal("pando_list_tools", L.NewFunction(opts.ListTools))
	}
	if opts.LoadFile != nil {
		L.SetGlobal("pando_load_file", L.NewFunction(opts.LoadFile))
	}
	L.SetGlobal("pando_register_tool", L.NewFunction(registerLuaTool))
}

func registerLuaTool(L *lua.LState) int {
	tbl := L.CheckTable(1)
	registry := ensureLuaToolRegistry(L)
	registry.Append(tbl)
	return 0
}

func ensureLuaToolRegistry(L *lua.LState) *lua.LTable {
	registry := L.GetGlobal(luaRegisteredToolsGlobal)
	if tbl, ok := registry.(*lua.LTable); ok {
		return tbl
	}
	tbl := L.NewTable()
	L.SetGlobal(luaRegisteredToolsGlobal, tbl)
	return tbl
}

// CollectRegisteredTools validates and converts Lua-declared tool definitions.
func CollectRegisteredTools(L *lua.LState) ([]LuaToolDefinition, error) {
	registry := L.GetGlobal(luaRegisteredToolsGlobal)
	tbl, ok := registry.(*lua.LTable)
	if !ok {
		return nil, nil
	}

	tools := make([]LuaToolDefinition, 0)
	seen := make(map[string]struct{})
	var firstErr error
	tbl.ForEach(func(_ lua.LValue, value lua.LValue) {
		if firstErr != nil {
			return
		}
		entry, ok := value.(*lua.LTable)
		if !ok {
			firstErr = fmt.Errorf("pando_register_tool expects table entries")
			return
		}
		tool, err := parseLuaToolDefinition(entry)
		if err != nil {
			firstErr = err
			return
		}
		if _, exists := seen[tool.Name]; exists {
			firstErr = fmt.Errorf("duplicate lua tool name %q", tool.Name)
			return
		}
		seen[tool.Name] = struct{}{}
		tools = append(tools, tool)
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return tools, nil
}

func parseLuaToolDefinition(tbl *lua.LTable) (LuaToolDefinition, error) {
	tool := LuaToolDefinition{
		Parameters: map[string]any{},
		Required:   []string{},
	}
	name := strings.TrimSpace(lua.LVAsString(tbl.RawGetString("name")))
	if name == "" {
		return tool, fmt.Errorf("lua tool name is required")
	}
	if !strings.HasPrefix(name, "lua_") {
		return tool, fmt.Errorf("lua tool %q must use the lua_ prefix", name)
	}
	tool.Name = name
	tool.Description = strings.TrimSpace(lua.LVAsString(tbl.RawGetString("description")))
	if tool.Description == "" {
		return tool, fmt.Errorf("lua tool %q description is required", name)
	}
	if params, ok := tbl.RawGetString("parameters").(*lua.LTable); ok {
		tool.Parameters = LuaTableToMap(params)
	}
	if req, ok := tbl.RawGetString("required").(*lua.LTable); ok {
		required := make([]string, 0)
		for i := 1; i <= req.Len(); i++ {
			value := strings.TrimSpace(lua.LVAsString(req.RawGetInt(i)))
			if value != "" {
				required = append(required, value)
			}
		}
		tool.Required = required
	}
	return tool, nil
}
