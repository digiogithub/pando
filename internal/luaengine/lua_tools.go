package luaengine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	lua "github.com/yuin/gopher-lua"
)

// DiscoverLuaTools reads the global pando_tools table from the given LState
// and returns a slice of LuaToolDef. If pando_tools is not defined, returns empty slice.
func DiscoverLuaTools(L *lua.LState) []LuaToolDef {
	global := L.GetGlobal("pando_tools")
	if global == lua.LNil {
		return []LuaToolDef{}
	}

	toolsTable, ok := global.(*lua.LTable)
	if !ok {
		logging.Warn("pando_tools is not a table, skipping Lua tool discovery")
		return []LuaToolDef{}
	}

	var tools []LuaToolDef

	toolsTable.ForEach(func(k, v lua.LValue) {
		name, ok := k.(lua.LString)
		if !ok {
			return
		}

		defTable, ok := v.(*lua.LTable)
		if !ok {
			return
		}

		def := LuaToolDef{
			Name:       string(name),
			Parameters: make(map[string]LuaToolParam),
		}

		// Extract description
		if descVal := defTable.RawGetString("description"); descVal != lua.LNil {
			if descStr, ok := descVal.(lua.LString); ok {
				def.Description = string(descStr)
			}
		}

		// Extract parameters
		paramsVal := defTable.RawGetString("parameters")
		if paramsVal != lua.LNil {
			if paramsTable, ok := paramsVal.(*lua.LTable); ok {
				paramsTable.ForEach(func(pk, pv lua.LValue) {
					paramName, ok := pk.(lua.LString)
					if !ok {
						return
					}

					paramTable, ok := pv.(*lua.LTable)
					if !ok {
						return
					}

					param := LuaToolParam{
						Type: "string", // default type
					}

					if typeVal := paramTable.RawGetString("type"); typeVal != lua.LNil {
						if typeStr, ok := typeVal.(lua.LString); ok {
							param.Type = string(typeStr)
						}
					}

					if descVal := paramTable.RawGetString("description"); descVal != lua.LNil {
						if descStr, ok := descVal.(lua.LString); ok {
							param.Description = string(descStr)
						}
					}

					if reqVal := paramTable.RawGetString("required"); reqVal != lua.LNil {
						if reqBool, ok := reqVal.(lua.LBool); ok {
							param.Required = bool(reqBool)
						}
					}

					if defVal := paramTable.RawGetString("default"); defVal != lua.LNil {
						param.Default = LuaToGo(defVal)
					}

					def.Parameters[string(paramName)] = param

					if param.Required {
						def.Required = append(def.Required, string(paramName))
					}
				})
			}
		}

		tools = append(tools, def)
	})

	logging.Info("Lua tools discovered", "count", len(tools))
	return tools
}

// ExecuteLuaTool executes a named Lua tool from the pando_tools table.
// params is passed as a Lua table to the run function.
// Returns the string result or an error.
func ExecuteLuaTool(L *lua.LState, mu *sync.RWMutex, name string, params map[string]interface{}, timeout time.Duration) (string, error) {
	mu.RLock()
	defer mu.RUnlock()

	pandoTools := L.GetGlobal("pando_tools")
	if pandoTools == lua.LNil {
		return "", fmt.Errorf("pando_tools not defined in Lua script")
	}

	toolsTable, ok := pandoTools.(*lua.LTable)
	if !ok {
		return "", fmt.Errorf("pando_tools is not a table")
	}

	toolDef := toolsTable.RawGetString(name)
	if toolDef == lua.LNil {
		return "", fmt.Errorf("lua tool %q not found", name)
	}

	defTable, ok := toolDef.(*lua.LTable)
	if !ok {
		return "", fmt.Errorf("tool definition for %q is not a table", name)
	}

	runFn := defTable.RawGetString("run")
	if runFn == lua.LNil {
		return "", fmt.Errorf("tool %q has no run function", name)
	}

	fn, ok := runFn.(*lua.LFunction)
	if !ok {
		return "", fmt.Errorf("tool %q run field is not a function", name)
	}

	paramsTable := MapToLuaTable(L, params)

	type execResult struct {
		output string
		err    error
	}

	done := make(chan execResult, 1)

	go func() {
		if err := L.CallByParam(lua.P{
			Fn:      fn,
			NRet:    2,
			Protect: true,
		}, paramsTable); err != nil {
			done <- execResult{err: fmt.Errorf("lua tool %q execution failed: %w", name, err)}
			return
		}

		// Pop in reverse order: second return value first, then first
		errVal := L.Get(-1)
		L.Pop(1)
		retVal := L.Get(-1)
		L.Pop(1)

		var errStr string
		if errVal != lua.LNil {
			if es, ok := errVal.(lua.LString); ok {
				errStr = string(es)
			}
		}

		if errStr != "" {
			done <- execResult{err: fmt.Errorf("lua tool %q returned error: %s", name, errStr)}
			return
		}

		if retVal == lua.LNil {
			done <- execResult{err: fmt.Errorf("tool %q returned no result", name)}
			return
		}

		output := ""
		if s, ok := retVal.(lua.LString); ok {
			output = string(s)
		} else {
			output = retVal.String()
		}

		done <- execResult{output: output}
	}()

	ctx := context.Background()
	select {
	case res := <-done:
		return res.output, res.err
	case <-time.After(timeout):
		return "", fmt.Errorf("lua tool %q timed out after %v", name, timeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
