package prompt

import (
	"os"
	"path/filepath"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/luaengine"
	lua "github.com/yuin/gopher-lua"
)

// NewPromptLuaFunctions creates PromptFunctionOptions with implementations
// that bridge the prompt system to Lua scripts.
func NewPromptLuaFunctions() *luaengine.PromptFunctionOptions {
	return &luaengine.PromptFunctionOptions{
		GetConfig:    luaGetConfig,
		GetGitStatus: luaGetGitStatus,
		LoadFile:     luaLoadFile,
		// ListMCPServers and ListTools are set externally since they
		// depend on runtime state not available at init time.
	}
}

// luaGetConfig returns a config value by key.
// Usage in Lua: local value = pando_get_config("working_dir")
// Supported keys: "working_dir", "data_dir", "debug"
func luaGetConfig(L *lua.LState) int {
	key := L.CheckString(1)
	cfg := config.Get()

	switch key {
	case "working_dir":
		L.Push(lua.LString(cfg.WorkingDir))
	case "data_dir":
		L.Push(lua.LString(cfg.Data.Directory))
	case "debug":
		L.Push(lua.LBool(cfg.Debug))
	default:
		L.Push(lua.LNil)
	}
	return 1
}

// luaGetGitStatus returns git status info as a table.
// Usage in Lua: local git = pando_get_git_status()
// Returns: {is_repo=true/false, working_dir="..."}
func luaGetGitStatus(L *lua.LState) int {
	cwd := config.WorkingDirectory()
	table := L.NewTable()

	isRepo := isGitRepo(cwd)
	L.SetField(table, "is_repo", lua.LBool(isRepo))

	if isRepo {
		L.SetField(table, "working_dir", lua.LString(cwd))
	}

	L.Push(table)
	return 1
}

// luaLoadFile reads a file and returns its content.
// Usage in Lua: local content = pando_load_file("path/to/file")
// Returns file content as string, or nil if file doesn't exist.
func luaLoadFile(L *lua.LState) int {
	path := L.CheckString(1)

	// Resolve relative paths against working directory
	if !filepath.IsAbs(path) {
		path = filepath.Join(config.WorkingDirectory(), path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		L.Push(lua.LNil)
		return 1
	}

	L.Push(lua.LString(string(content)))
	return 1
}
