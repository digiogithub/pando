package luaengine

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// GoToLua converts a Go value to a Lua value.
// Handles: nil, bool, string, int, int64, float64, map[string]interface{}, []interface{}
func GoToLua(L *lua.LState, val interface{}) lua.LValue {
	switch v := val.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(v)
	case string:
		return lua.LString(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case float32:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case map[string]interface{}:
		return MapToLuaTable(L, v)
	case []interface{}:
		return sliceToLuaTable(L, v)
	default:
		return lua.LNil
	}
}

// LuaToGo converts a Lua value to a Go value.
// Handles: LNil, LBool, LNumber, LString, *LTable (auto-detects array vs map)
func LuaToGo(lv lua.LValue) interface{} {
	switch v := lv.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(v)
	case lua.LString:
		return string(v)
	case lua.LNumber:
		if float64(v) == float64(int64(v)) {
			return int64(v)
		}
		return float64(v)
	case *lua.LTable:
		return LuaTableToMap(v)
	default:
		return nil
	}
}

// MapToLuaTable converts a Go map[string]interface{} to a Lua table.
func MapToLuaTable(L *lua.LState, m map[string]interface{}) *lua.LTable {
	table := L.NewTable()
	for k, v := range m {
		table.RawSetString(k, GoToLua(L, v))
	}
	return table
}

// LuaTableToMap converts a Lua table to a Go map[string]interface{}.
// If the table is an array, keys are converted to string indices.
func LuaTableToMap(lt *lua.LTable) map[string]interface{} {
	result := make(map[string]interface{})
	lt.ForEach(func(k, v lua.LValue) {
		var key string
		switch kt := k.(type) {
		case lua.LString:
			key = string(kt)
		case lua.LNumber:
			key = fmt.Sprintf("%v", kt)
		default:
			key = k.String()
		}
		result[key] = LuaToGo(v)
	})
	return result
}

// sliceToLuaTable converts a Go []interface{} to a Lua array table (1-indexed).
func sliceToLuaTable(L *lua.LState, s []interface{}) *lua.LTable {
	table := L.NewTable()
	for i, v := range s {
		table.RawSetInt(i+1, GoToLua(L, v))
	}
	return table
}

// setTableField sets a field in a Lua table from a Go value.
func setTableField(L *lua.LState, table *lua.LTable, key string, value interface{}) {
	table.RawSetString(key, GoToLua(L, value))
}
