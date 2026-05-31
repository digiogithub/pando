# Pando Lua Engine — Complete Reference

> **Last updated**: 2026-05-31  
> **Source files analyzed**: `internal/luaengine/`, `internal/llm/agent/`, `internal/llm/prompt/`, `internal/llm/tools/`, `internal/session/`, `internal/app/app.go`  
> **Runtime**: [GopherLua](https://github.com/yuin/gopher-lua) (`github.com/yuin/gopher-lua`)

---

## 1. Overview

Pando embeds a GopherLua virtual machine that allows users to customize the agent's behavior at runtime via a single Lua script. Three customization mechanisms are supported:

1. **Lifecycle hooks** — React to session/conversation/prompt events via `hook_<name>(ctx)` functions.
2. **MCP input/output filters** — Intercept tool calls to external MCP servers before/after execution.
3. **MVP Lua tools** — Register new first-class tools via `pando_register_tool({...})` and dispatch with `pando_run_tool(ctx)`.

---

## 2. Package Structure

```
internal/luaengine/
├── lua.go          # Sandboxed LState factory
├── types.go        # FilterType, HookType, HookContext, HookResult
├── helpers.go      # Go↔Lua type conversion utilities
├── functions.go    # pando_* helper functions + tool registration
├── manager.go      # FilterManager — central execution engine
├── helpers_test.go
└── manager_test.go

internal/llm/tools/lua_tools.go   # Wraps LuaToolDefinition as BaseTool
internal/llm/agent/tools.go       # Injects Lua tools into agent tool list
internal/llm/prompt/luafuncs.go   # Concrete implementations of prompt helper functions
```

---

## 3. Lua State Initialization (`lua.go`)

```go
func NewLuaState() *lua.LState
func CloseLuaState(L *lua.LState)
```

Each `FilterManager` owns one `*lua.LState` created with:

| Option | Value |
|---|---|
| `CallStackSize` | 120 |
| `RegistrySize` | 1024 |
| `SkipOpenLibs` | false (standard Lua libs loaded) |
| `IncludeGoStackTrace` | true |

**Preloaded extra modules:**

| Lua name | Go package | Purpose |
|---|---|---|
| `strings` | `vadv/gopher-lua-libs/strings` | String manipulation |
| `time` | `vadv/gopher-lua-libs/time` | Date/time utilities |
| `regexp` | `vadv/gopher-lua-libs/regexp` | Regular expressions |
| `json` | `layeh/gopher-json` | JSON encode/decode |

**Intentionally excluded** for security: `sh`, `os.exec`, `argparse` — no arbitrary shell execution is possible from Lua scripts.

---

## 4. Preloaded Plugin Reference

All four plugins are loaded via `L.PreloadModule(...)` and must be explicitly required before use:

```lua
local strings = require("strings")
local time    = require("time")
local regexp  = require("regexp")
local json    = require("json")
```

---

### 4.1 `strings` — Go `strings` package port

**Source**: `github.com/vadv/gopher-lua-libs@v0.8.0/strings`

#### Module functions

| Function | Signature | Description |
|---|---|---|
| `split` | `strings.split(str, sep) → table` | Go `strings.Split` — splits by separator; if sep is `""`, splits into characters |
| `fields` | `strings.fields(str) → table` | Go `strings.Fields` — splits on whitespace, removing empty parts |
| `has_prefix` | `strings.has_prefix(str, prefix) → bool` | Go `strings.HasPrefix` |
| `has_suffix` | `strings.has_suffix(str, suffix) → bool` | Go `strings.HasSuffix` |
| `contains` | `strings.contains(str, substr) → bool` | Go `strings.Contains` |
| `trim` | `strings.trim(str, cutset) → string` | Go `strings.Trim` — removes any leading/trailing chars in cutset |
| `trim_space` | `strings.trim_space(str) → string` | Go `strings.TrimSpace` — removes leading/trailing whitespace |
| `trim_prefix` | `strings.trim_prefix(str, prefix) → string` | Go `strings.TrimPrefix` |
| `trim_suffix` | `strings.trim_suffix(str, suffix) → string` | Go `strings.TrimSuffix` |
| `new_reader` | `strings.new_reader(str) → Reader` | Creates a `strings.Reader` userdata for stream reading |
| `new_builder` | `strings.new_builder() → Builder` | Creates a `strings.Builder` userdata for incremental construction |

#### `strings.Reader` userdata methods

Created with `strings.new_reader(str)`. Implements the io.Reader interface.

| Method | Signature | Description |
|---|---|---|
| `read` | `reader:read(n) → string` | Read up to n bytes from the reader |
| `close` | `reader:close()` | Close and release the reader |

#### `strings.Builder` userdata methods

Created with `strings.new_builder()`. Implements the io.Writer interface.

| Method | Signature | Description |
|---|---|---|
| `write` | `builder:write(str)` | Append a string to the builder |
| `close` | `builder:close()` | Release the builder |
| `string` | `builder:string() → string` | Return the accumulated string |

**Usage example:**

```lua
local strings = require("strings")

-- Split and iterate
local parts = strings.split("a,b,c", ",")
for i = 1, #parts do
    print(parts[i])  -- "a", "b", "c"
end

-- Trim and check
local s = strings.trim_space("  hello  ")
if strings.has_prefix(s, "hel") then
    print(s)  -- "hello"
end

-- Build a string incrementally
local b = strings.new_builder()
b:write("foo")
b:write("bar")
print(b:string())  -- "foobar"
```

---

### 4.2 `time` — Go `time` package port

**Source**: `github.com/vadv/gopher-lua-libs@v0.8.0/time`

#### Module functions

| Function | Signature | Description |
|---|---|---|
| `unix` | `time.unix() → float` | Current Unix timestamp in seconds (float64, sub-second precision) |
| `unix_nano` | `time.unix_nano() → int` | Current Unix timestamp in nanoseconds |
| `sleep` | `time.sleep(seconds)` | Pause execution for the given number of seconds (float accepted) |
| `parse` | `time.parse(value, layout[, location]) → (float, error)` | Parse a time string using Go reference layout; returns Unix float or `nil, errstr` |
| `format` | `time.format(unixts[, layout[, location]]) → (string, error)` | Format a Unix timestamp as string; returns formatted string or `nil, errstr` |

**Notes on `parse` / `format`:**
- `layout` uses Go's reference time: `"2006-01-02T15:04:05"`, `"Mon Jan 2 15:04:05 -0700 MST 2006"`, etc.
- Default layout for `format`: `"Mon Jan 2 15:04:05 -0700 MST 2006"`.
- `location` is an IANA timezone name, e.g. `"UTC"`, `"Europe/Madrid"`.

**Usage example:**

```lua
local time = require("time")

local now = time.unix()   -- e.g. 1748724000.123456
local formatted = time.format(now, "2006-01-02")  -- "2026-05-31"

local ts, err = time.parse("2026-05-31", "2006-01-02")
if err then
    print("parse error: " .. err)
else
    print(ts)  -- Unix float
end

time.sleep(0.1)  -- sleep 100ms
```

> **Warning**: `time.sleep` inside a hook will block the entire Pando response. The Lua timeout (`Timeout` config) will fire if the sleep exceeds it.

---

### 4.3 `regexp` — Go `regexp` package port

**Source**: `github.com/vadv/gopher-lua-libs@v0.8.0/regexp`

Two usage styles: **one-shot** (module-level, compiles on each call) and **compiled** (reusable `regexp_ud` object).

#### Module-level functions (one-shot)

| Function | Signature | Description |
|---|---|---|
| `compile` | `regexp.compile(expr) → (regexp_ud, error)` | Compile a regex and return a reusable object |
| `match` | `regexp.match(expr, str) → (bool, error)` | Test if `str` matches `expr` |
| `find_all_string_submatch` | `regexp.find_all_string_submatch(expr, str) → (table, error)` | Return all matches with capture groups |

#### `regexp_ud` object methods

Created with `regexp.compile(expr)`.

| Method | Signature | Description |
|---|---|---|
| `match` | `re:match(str) → bool` | Test if `str` matches the compiled pattern |
| `find_all_string_submatch` | `re:find_all_string_submatch(str) → table` | Return all matches as table-of-tables of strings |

**`find_all_string_submatch` return format:**  
Outer table: one entry per match. Inner table: index `[1]` is the full match, `[2..n]` are capture groups.

**Usage example:**

```lua
local regexp = require("regexp")

-- One-shot match
local ok, err = regexp.match(`^\d{3}-\d{4}$`, "123-4567")
if ok then print("matched") end

-- Compiled (reuse across calls)
local re, err = regexp.compile("(\\w+)=(\\w+)")
if err then error(err) end

local matches = re:find_all_string_submatch("foo=bar baz=qux")
for i = 1, #matches do
    local full   = matches[i][1]  -- "foo=bar"
    local key    = matches[i][2]  -- "foo"
    local value  = matches[i][3]  -- "bar"
end
```

---

### 4.4 `json` — JSON encode/decode

**Source**: `github.com/layeh/gopher-json@v0.0.0-20201124131017-552bb3c4c3bf`

#### Module functions

| Function | Signature | Description |
|---|---|---|
| `decode` | `json.decode(str) → (value, error)` | Parse a JSON string into a Lua value |
| `encode` | `json.encode(value) → (string, error)` | Serialize a Lua value to a JSON string |

#### Type mapping

| Lua type | JSON type |
|---|---|
| `nil` | `null` |
| `bool` | `true` / `false` |
| `number` | number |
| `string` | string |
| table with **consecutive integer keys from 1** | array |
| table with **string keys** | object |
| table with mixed keys | error |
| sparse array (gaps in numeric keys) | error |
| recursively nested table | error |

**Usage example:**

```lua
local json = require("json")

-- Decode JSON
local data, err = json.decode('{"name":"pando","version":1}')
if err then error(err) end
print(data.name)    -- "pando"
print(data.version) -- 1 (number)

-- Encode to JSON
local s, err = json.encode({ tool = "lua_echo", args = { "a", "b" } })
-- s = '{"args":["a","b"],"tool":"lua_echo"}'

-- Decode a JSON array
local arr, err = json.decode('[1, 2, 3]')
for i = 1, #arr do print(arr[i]) end  -- 1, 2, 3
```

---

## 5. Types (`types.go`)

### 5.1 FilterType

```go
const (
    FilterInput  FilterType = "input"
    FilterOutput FilterType = "output"
)
```

### 5.2 HookType Constants

| Constant | String value | Category |
|---|---|---|
| `HookSystemPrompt` | `"system_prompt"` | Prompt |
| `HookSessionStart` | `"session_start"` | Session |
| `HookSessionRestore` | `"session_restore"` | Session |
| `HookSessionEnd` | `"session_end"` | Session |
| `HookConversationStart` | `"conversation_start"` | Agent |
| `HookUserPrompt` | `"user_prompt"` | Agent |
| `HookAgentResponseFinish` | `"agent_response_finish"` | Agent |
| `HookTemplateSection` | `"template_section"` | Prompt builder |
| `HookCapabilityCheck` | `"capability_check"` | Prompt builder |
| `HookProviderSelect` | `"provider_select"` | Prompt builder |
| `HookPromptCompose` | `"prompt_compose"` | Prompt builder |
| `HookEvaluationComplete` | `"hook_evaluation_complete"` | Self-improvement |
| `HookCacheStore` | `"hook_cache_store"` | Tool cache |
| `HookCacheEvict` | `"hook_cache_evict"` | Tool cache |
| `HookCacheClear` | `"hook_cache_clear"` | Tool cache |

### 5.3 HookContext (passed as Lua table)

| Field | Type | Present when |
|---|---|---|
| `server_name` | string | MCP filters |
| `tool_name` | string | MCP filters + Lua tools |
| `hook_type` | string | Lifecycle hooks |
| `parameters` | table | Input filters + hooks |
| `result` | table | Output filters |
| `request_id` | string | Always |
| `session_id` | string | Session-scoped calls |
| `timestamp` | int64 | Always (Unix seconds) |
| `duration` | int64 | Output filters only (ms) |
| `filter_type` | string | Always (`"input"` or `"output"`) |

### 5.4 HookResult (Go side)

| Field | Type | Meaning |
|---|---|---|
| `Modified` | bool | Whether the filter changed any data |
| `Data` | map[string]interface{} | Filtered/returned data |
| `Error` | error | Execution error (if any) |
| `ExecutionTime` | time.Duration | Wall time for the Lua call |
| `Logs` | []string | Reserved for future log capture |

---

## 6. Type Conversion Utilities (`helpers.go`)

### Go → Lua

```go
func GoToLua(L *lua.LState, val interface{}) lua.LValue
func MapToLuaTable(L *lua.LState, m map[string]interface{}) *lua.LTable
```

Supports: `nil`, `bool`, `string`, `int`, `int64`, `float32`, `float64`, `map[string]interface{}`, `[]interface{}`

### Lua → Go

```go
func LuaToGo(lv lua.LValue) interface{}
func LuaTableToMap(lt *lua.LTable) map[string]interface{}
```

Lua arrays are converted to maps with string numeric keys (`"1"`, `"2"`, …).

---

## 7. Pando Helper Functions for Lua (`functions.go`)

These are injected into the LState via `RegisterPromptFunctions(L, opts)`. `pando_register_tool` is always registered.

### 7.1 Prompt Helper Functions

| Lua global | Signature | Description |
|---|---|---|
| `pando_get_config` | `pando_get_config(key) → value` | Returns a config value. Supported keys: `"working_dir"`, `"data_dir"`, `"debug"` |
| `pando_get_git_status` | `pando_get_git_status() → table` | Returns `{is_repo=bool, working_dir=string}` |
| `pando_load_file` | `pando_load_file(path) → string\|nil` | Reads a file; relative paths resolved from working dir |
| `pando_list_mcp_servers` | `pando_list_mcp_servers() → table` | Returns list of configured MCP server names |
| `pando_list_tools` | `pando_list_tools() → table` | Returns list of all available tool names |

`ListMCPServers` and `ListTools` are injected at runtime (not at init) since they depend on runtime agent state.

### 7.2 Tool Registration Functions

| Lua global | Description |
|---|---|
| `pando_register_tool(table)` | Registers a tool definition in a global registry (`__pando_registered_tools`) |
| `pando_run_tool(ctx)` | **User-defined** dispatcher function called when any `lua_*` tool is executed |

**`pando_register_tool` table fields:**

| Field | Required | Constraint |
|---|---|---|
| `name` | Yes | Must start with `lua_` |
| `description` | Yes | Non-empty string |
| `parameters` | No | Table of parameter definitions |
| `required` | No | Array of required parameter names |

---

## 8. FilterManager (`manager.go`)

Central engine that manages the Lua lifecycle and dispatches all hook/filter calls.

### 8.1 Creation and Lifecycle

```go
func NewFilterManager(scriptPath string, timeout time.Duration, strictMode bool) (*FilterManager, error)
func (fm *FilterManager) LoadScript() error
func (fm *FilterManager) ReloadScript() error
func (fm *FilterManager) Close()
func (fm *FilterManager) IsEnabled() bool
```

**Behavior on `NewFilterManager`:**
- If `scriptPath` is empty or the file doesn't exist, filters are disabled (no error).
- In **strict mode**: script load errors cause `NewFilterManager` to return an error and the Lua state is closed.
- In **non-strict mode**: script load errors are logged and the manager continues without filters.

**`ReloadScript`** tears down the existing `LState`, creates a fresh one, and re-executes the script — safe for hot-reload.

### 8.2 Execution Methods

```go
func (fm *FilterManager) ApplyInputFilter(ctx context.Context, hookCtx *HookContext) (*HookResult, error)
func (fm *FilterManager) ApplyOutputFilter(ctx context.Context, hookCtx *HookContext) (*HookResult, error)
func (fm *FilterManager) ExecuteHook(ctx context.Context, hookType HookType, data map[string]interface{}) (*HookResult, error)
func (fm *FilterManager) ExecuteLuaTool(ctx context.Context, toolName string, data map[string]interface{}) (*HookResult, error)
```

### 8.3 Function Resolution and Fallback Logic

| Call type | Primary function | Fallback |
|---|---|---|
| Input filter | `<server-name>-input` | `global-input` |
| Output filter | `<server-name>-output` | `global-output` |
| Lifecycle hook | `hook_<hookType>` | `hook_global` |
| Lua tool | `pando_run_tool` | — (no fallback) |

If neither the primary function nor the fallback exists, the original `ctx.Parameters` or `ctx.Result` is returned unchanged (`HookResult.Modified = false`).

**Server name normalization** (for MCP filters):
- `_` and `.` are replaced with `-`
- Example: MCP server `github_docs` → Lua function `github-docs-input`

### 8.4 Execution Internals

Each Lua call runs in a goroutine and is controlled by a `select` with three channels:

```
done              → normal completion
time.After(timeout) → timeout (logs error, returns original data in non-strict mode)
ctx.Done()          → context cancellation (returns ctx.Err())
```

- All Lua calls use `CallByParam` with `Protect: true` (panics caught as errors).
- The Lua state is protected by `sync.RWMutex` (`RLock` during execution, `Lock` during reload).

---

## 9. Integration Points in Pando

### 9.1 App Initialization (`internal/app/app.go`)

```go
luaMgr, err := luaengine.NewFilterManager(cfg.Lua.ScriptPath, luaTimeout, cfg.Lua.StrictMode)
app.LuaManager = luaMgr
agent.SetLuaManager(luaMgr)      // agent hooks + MCP filters + Lua tools
session.SetLuaManager(luaMgr)    // session lifecycle hooks
```

### 9.2 Session Hooks (`internal/session/session.go`)

| Hook | Trigger |
|---|---|
| `hook_session_start` | `NewSession()` — new session created |
| `hook_session_restore` | `RestoreSession()` — session loaded from DB |
| `hook_session_end` | `EndSession()` — session destroyed |

Typical `ctx` fields: `session_id`, `title`, `created_at`.

### 9.3 Agent Hooks (`internal/llm/agent/agent.go`)

| Hook | Trigger point |
|---|---|
| `hook_conversation_start` | Before the agent processing loop starts |
| `hook_user_prompt` | After user message received, before sending to LLM |
| `hook_agent_response_finish` | After LLM response complete (`EventComplete`) |

`hook_user_prompt` ctx includes `user_content`; set `modified_content` to override what the LLM sees.  
`hook_agent_response_finish` ctx includes `session_id`, `finish_reason`, `input_tokens`, `output_tokens`.

### 9.4 MCP Tool Filters (`internal/llm/agent/mcp-tools.go`)

```
LLM requests tool → ApplyInputFilter → MCP server → ApplyOutputFilter → LLM
```

Only applies to tools invoked through external MCP servers. Built-in Go tools are **not** intercepted.

### 9.5 Prompt Builder Hooks (`internal/llm/prompt/builder.go`, `prompt.go`)

| Hook | Trigger | Useful ctx fields |
|---|---|---|
| `hook_template_section` | Each template section rendered | `section_name`, `section_content`, `agent_name` |
| `hook_capability_check` | Each capability flag evaluated | `agent_name`, `capability`, `available` |
| `hook_provider_select` | Provider template selected | `agent_name`, `provider`, `model_family`, `provider_template` |
| `hook_prompt_compose` | Final prompt sections assembled | `sections` (array of `{name, content}` tables) |
| `hook_system_prompt` | System prompt string finalized | `system_prompt` (string) |

---

## 10. Lua Tool Integration (`lua_tools.go`, `tools.go`)

### Flow

1. At script load time: `pando_register_tool({...})` calls populate the internal `__pando_registered_tools` registry.
2. `CollectRegisteredTools(L)` validates and extracts `[]LuaToolDefinition`.
3. `FilterManager.LuaTools()` exposes them to the agent.
4. `tools.NewLuaTools(manager)` wraps each definition as a `BaseTool`.
5. The agent's tool list includes Lua tools automatically via `appendLuaTools()`.
6. When the LLM calls a `lua_*` tool, `luaTool.Run()` calls `manager.ExecuteLuaTool()` which invokes `pando_run_tool(ctx)`.

### `pando_run_tool` ctx fields

| Field | Type | Description |
|---|---|---|
| `tool_name` | string | Name of the called tool |
| `arguments` | table | Arguments from the LLM call |
| `session_id` | string | Current session ID |
| `message_id` | string | Current message ID |
| `call_id` | string | Tool call ID from the LLM |

### Return value from `pando_run_tool`

The function must return a Lua table with **one of**:

| Key | Behavior |
|---|---|
| `content` | Returned as text response |
| `output` | Returned as text response |
| `error` | Returned as error response to the LLM |

---

## 11. Configuration (`[Lua]` section in TOML)

```toml
[Lua]
Enabled    = true
ScriptPath = "examples/lua-hooks/hooks.lua"
Timeout    = "5s"
StrictMode = false
HotReload  = true
```

| Key | Type | Default | Description |
|---|---|---|---|
| `Enabled` | bool | false | Master switch |
| `ScriptPath` | string | `""` | Path to `.lua` file (absolute or relative to working dir) |
| `Timeout` | duration | `5s` | Maximum execution time per Lua call |
| `StrictMode` | bool | false | If true, any Lua error aborts the calling operation |
| `HotReload` | bool | false | If true, script is reloaded when the file changes |

---

## 12. Naming Rules Summary

### Lifecycle hooks

```lua
function hook_system_prompt(ctx) ... return ctx end
function hook_session_start(ctx) ... return ctx end
function hook_session_restore(ctx) ... return ctx end
function hook_session_end(ctx) ... return ctx end
function hook_conversation_start(ctx) ... return ctx end
function hook_user_prompt(ctx) ... return ctx end
function hook_agent_response_finish(ctx) ... return ctx end
function hook_template_section(ctx) ... return ctx end
function hook_capability_check(ctx) ... return ctx end
function hook_provider_select(ctx) ... return ctx end
function hook_prompt_compose(ctx) ... return ctx end
function hook_global(ctx) ... return ctx end  -- fallback for any unhandled hook
```

### MCP filters

Because `-` is not a valid Lua identifier, use `rawset`:

```lua
rawset(_G, "myserver-input", function(ctx) return ctx end)
rawset(_G, "myserver-output", function(ctx) return ctx end)
rawset(_G, "global-input", function(ctx) return ctx end)   -- fallback for all servers
rawset(_G, "global-output", function(ctx) return ctx end)
```

Server name normalization: `_` and `.` → `-` (e.g. `my_server` → `my-server-input`).

### Lua tools

```lua
pando_register_tool({
    name        = "lua_my_tool",    -- MUST start with "lua_"
    description = "What it does",
    parameters  = {
        arg1 = { type = "string", description = "..." }
    },
    required = { "arg1" }
})

function pando_run_tool(ctx)
    if ctx.tool_name == "lua_my_tool" then
        return { content = "result: " .. (ctx.arguments.arg1 or "") }
    end
    return { error = "unknown tool: " .. tostring(ctx.tool_name) }
end
```

---

## 13. Security Model

- Shell execution is disabled: `sh`, `os.exec`, `argparse` modules are NOT loaded.
- Every Lua call runs with a configurable timeout (default 5 s). Timeout causes the original data to be returned (non-strict) or an error to be returned (strict).
- Context cancellation is respected — calling code can cancel via `context.Context`.
- The LState is mutex-protected: reads (hook execution) use `RLock`, writes (reload, prompt function registration) use `Lock`.
- In non-strict mode, any Lua runtime error is logged and the original data is passed through unchanged.

---

## 14. Complete Example Script

```lua
-- pando-hooks.lua
-- Combines lifecycle hooks, prompt hooks, MCP filters, and a Lua tool.

local strings = require("strings")
local time    = require("time")
local json    = require("json")
local regexp  = require("regexp")

-- ── Lifecycle hooks ──────────────────────────────────────────────────────────

function hook_session_start(ctx)
    return ctx
end

function hook_session_end(ctx)
    ctx.session_closed_by_lua = true
    return ctx
end

function hook_user_prompt(ctx)
    if ctx.user_content and strings.contains(ctx.user_content, "@brief") then
        ctx.modified_content = ctx.user_content:gsub("@brief",
            "Please keep the answer concise and action-oriented.")
    end
    return ctx
end

function hook_agent_response_finish(ctx)
    return ctx
end

-- ── Prompt builder hooks ─────────────────────────────────────────────────────

function hook_system_prompt(ctx)
    local git = pando_get_git_status()
    if git and git.is_repo then
        ctx.system_prompt = ctx.system_prompt
            .. "\n\nCurrent repository: " .. (git.working_dir or "unknown")
    end
    return ctx
end

function hook_template_section(ctx)
    if ctx.section_name == "base/workflow" then
        local rules = pando_load_file(".pando/team-rules.md")
        if rules and rules ~= "" then
            ctx.section_content = ctx.section_content .. "\n\n## Team Rules\n" .. rules
        end
    end
    return ctx
end

function hook_capability_check(ctx)
    if ctx.agent_name == "auditor" and ctx.capability == "web_search" then
        ctx.available = false
    end
    return ctx
end

function hook_provider_select(ctx)
    if ctx.agent_name == "planner" then
        ctx.provider_template = "providers/anthropic"
    end
    return ctx
end

function hook_prompt_compose(ctx)
    local servers = pando_list_mcp_servers()
    if servers and #servers > 0 then
        local b = strings.new_builder()
        b:write("## Connected MCP Servers\n")
        for i = 1, #servers do
            b:write("- " .. tostring(servers[i]) .. "\n")
        end
        table.insert(ctx.sections, { name = "lua/mcp-summary", content = b:string() })
    end
    return ctx
end

-- ── MCP filters ──────────────────────────────────────────────────────────────

rawset(_G, "global-input", function(ctx)
    if ctx.parameters then
        ctx.parameters._lua_ts = time.unix()
    end
    return ctx
end)

rawset(_G, "global-output", function(ctx)
    return ctx
end)

rawset(_G, "github-docs-input", function(ctx)
    if ctx.parameters and ctx.parameters.query then
        ctx.parameters.query = ctx.parameters.query .. " language:go"
    end
    return ctx
end)

-- ── Lua tools ─────────────────────────────────────────────────────────────────

pando_register_tool({
    name        = "lua_echo",
    description = "Echoes text back to the caller.",
    parameters  = { text = { type = "string", description = "Text to echo" } },
    required    = { "text" }
})

pando_register_tool({
    name        = "lua_repo_summary",
    description = "Returns a small repository summary.",
    parameters  = {},
    required    = {}
})

function pando_run_tool(ctx)
    local name = ctx.tool_name
    local args = ctx.arguments or {}

    if name == "lua_echo" then
        return { content = "echo: " .. tostring(args.text or "") }
    end

    if name == "lua_repo_summary" then
        local git   = pando_get_git_status()
        local tools = pando_list_tools() or {}
        local repo  = (git and git.is_repo) and (git.working_dir or "unknown") or "not-a-repo"
        local data  = { repo = repo, tool_count = #tools, ts = time.format(time.unix(), "2006-01-02T15:04:05") }
        local s, err = json.encode(data)
        if err then return { error = err } end
        return { content = s }
    end

    return { error = "unknown lua tool: " .. tostring(name) }
end
```
