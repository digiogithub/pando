package luaengine

import (
	gopherjson "github.com/layeh/gopher-json"
	lua "github.com/yuin/gopher-lua"

	// gopher-lua-libs modules
	luastrings "github.com/vadv/gopher-lua-libs/strings"
	luatime    "github.com/vadv/gopher-lua-libs/time"
	luare      "github.com/vadv/gopher-lua-libs/regexp"
)

// NewLuaState initializes a new sandboxed Lua state with required modules preloaded.
// The shell/os execution modules are intentionally excluded for security.
func NewLuaState() *lua.LState {
	L := lua.NewState(lua.Options{
		CallStackSize:       120,
		RegistrySize:        1024,
		SkipOpenLibs:        false,
		IncludeGoStackTrace: true,
	})

	// Preload string manipulation module
	luastrings.Preload(L)

	// Preload time/date module
	luatime.Preload(L)

	// Preload regular expression module
	luare.Preload(L)

	// Preload JSON encode/decode module
	L.PreloadModule("json", gopherjson.Loader)

	// Note: sh, os.exec, and argparse modules are intentionally NOT loaded
	// to prevent arbitrary shell command execution from Lua filters.

	return L
}

// NewLuaStateWithModules creates a sandboxed Lua state with optional extra modules.
// allowedModules can include: "io" to enable file I/O (io.popen, io.open),
// "os" to enable os.execute (use with caution — grants arbitrary shell access).
func NewLuaStateWithModules(allowedModules []string) *lua.LState {
	L := lua.NewState(lua.Options{
		CallStackSize:       120,
		RegistrySize:        1024,
		SkipOpenLibs:        false,
		IncludeGoStackTrace: true,
	})

	luastrings.Preload(L)
	luatime.Preload(L)
	luare.Preload(L)
	L.PreloadModule("json", gopherjson.Loader)

	for _, mod := range allowedModules {
		switch mod {
		case "io":
			lua.OpenIo(L)
		case "os":
			lua.OpenOs(L)
		}
	}

	return L
}

// CloseLuaState properly closes and cleans up a Lua state.
func CloseLuaState(L *lua.LState) {
	if L != nil {
		L.Close()
	}
}
