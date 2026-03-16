package luaengine

import (
	lua "github.com/yuin/gopher-lua"
)

// PromptFunctionOptions holds callbacks for Lua prompt functions.
// These are injected from the calling package to avoid circular dependencies.
type PromptFunctionOptions struct {
	GetConfig      lua.LGFunction
	GetGitStatus   lua.LGFunction
	ListMCPServers lua.LGFunction
	ListTools      lua.LGFunction
	LoadFile       lua.LGFunction
}

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
}
