package luaengine

import (
	gopherjson "github.com/layeh/gopher-json"
	gluash "github.com/otm/gluash"
	lua "github.com/yuin/gopher-lua"

	luabase64 "github.com/vadv/gopher-lua-libs/base64"
	luabit "github.com/vadv/gopher-lua-libs/bit"
	luacmd "github.com/vadv/gopher-lua-libs/cmd"
	luacrypto "github.com/vadv/gopher-lua-libs/crypto"
	luadb "github.com/vadv/gopher-lua-libs/db"
	luafilepath "github.com/vadv/gopher-lua-libs/filepath"
	luagoos "github.com/vadv/gopher-lua-libs/goos"
	luahex "github.com/vadv/gopher-lua-libs/hex"
	luahttp "github.com/vadv/gopher-lua-libs/http"
	luahumanize "github.com/vadv/gopher-lua-libs/humanize"
	luainspect "github.com/vadv/gopher-lua-libs/inspect"
	luaioutil "github.com/vadv/gopher-lua-libs/ioutil"
	luare "github.com/vadv/gopher-lua-libs/regexp"
	luaruntime "github.com/vadv/gopher-lua-libs/runtime"
	luashellescape "github.com/vadv/gopher-lua-libs/shellescape"
	luastats "github.com/vadv/gopher-lua-libs/stats"
	luastorage "github.com/vadv/gopher-lua-libs/storage"
	luastrings "github.com/vadv/gopher-lua-libs/strings"
	luatac "github.com/vadv/gopher-lua-libs/tac"
	luatcp "github.com/vadv/gopher-lua-libs/tcp"
	luatemplate "github.com/vadv/gopher-lua-libs/template"
	luatime "github.com/vadv/gopher-lua-libs/time"
	luaxmlpath "github.com/vadv/gopher-lua-libs/xmlpath"
	luayaml "github.com/vadv/gopher-lua-libs/yaml"
)

// NewLuaState initializes a new Lua state with the supported modules preloaded.
func NewLuaState() *lua.LState {
	L := lua.NewState(lua.Options{
		CallStackSize:       120,
		RegistrySize:        1024,
		SkipOpenLibs:        false,
		IncludeGoStackTrace: true,
	})

	luastrings.Preload(L)
	luatime.Preload(L)
	luare.Preload(L)
	luabase64.Preload(L)
	luabit.Preload(L)
	luacmd.Preload(L)
	luacrypto.Preload(L)
	luadb.Preload(L)
	luafilepath.Preload(L)
	luagoos.Preload(L)
	luahex.Preload(L)
	luahttp.Preload(L)
	luahumanize.Preload(L)
	luainspect.Preload(L)
	luaioutil.Preload(L)
	luaruntime.Preload(L)
	luashellescape.Preload(L)
	luastats.Preload(L)
	luastorage.Preload(L)
	luatac.Preload(L)
	luatcp.Preload(L)
	luatemplate.Preload(L)
	luaxmlpath.Preload(L)
	luayaml.Preload(L)

	L.PreloadModule("json", gopherjson.Loader)
	L.PreloadModule("sh", gluash.Loader)

	return L
}

// CloseLuaState properly closes and cleans up a Lua state.
func CloseLuaState(L *lua.LState) {
	if L != nil {
		L.Close()
	}
}
